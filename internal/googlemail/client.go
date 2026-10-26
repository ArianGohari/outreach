// Package googlemail authorizes desktop accounts and sends through Gmail with gmail.send only.
package googlemail

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const SendScope = "https://www.googleapis.com/auth/gmail.send"

// Vault operations are provided by the desktop's OS keyring. Tokens never cross the UI bridge.
type Vault struct {
	Get    func(string) (string, error)
	Set    func(string, string) error
	Delete func(string) error
}
type clientConfig struct {
	ID     string `json:"client_id"`
	Secret string `json:"client_secret"`
}
type credential struct {
	Client  clientConfig `json:"client"`
	Email   string       `json:"email"`
	Subject string       `json:"subject"`
	Refresh string       `json:"refresh"`
}
type identity struct {
	Email    string `json:"email"`
	Verified bool   `json:"email_verified"`
	Subject  string `json:"sub"`
}
type Client struct {
	vault             Vault
	http              *http.Client
	endpoint          oauth2.Endpoint
	userinfo, sendURL string
	// Token refreshes are serialized across this local app; SMTP/API sends remain parallel.
	mu     sync.Mutex
	tokens map[string]*oauth2.Token
}

func New(vault Vault) *Client {
	return &Client{vault: vault, http: &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams},
		userinfo: "https://openidconnect.googleapis.com/v1/userinfo", sendURL: "https://gmail.googleapis.com/gmail/v1/users/me/messages/send", tokens: map[string]*oauth2.Token{}}
}
func problem(code, details, hint string) error {
	return diagnostics.New(code, "Google-Verbindung fehlgeschlagen", details, hint, nil)
}
func vaultError() error {
	return problem("GOOGLE_KEYCHAIN", "Google-Zugangsdaten konnten nicht im Betriebssystem-Schlüsselbund verarbeitet werden.", "Schlüsselbund entsperren und erneut versuchen.")
}
func (g *Client) ImportClient(data []byte) error {
	var file struct {
		Installed *clientConfig `json:"installed"`
	}
	if len(data) > 65536 || json.Unmarshal(data, &file) != nil || file.Installed == nil || !strings.HasSuffix(file.Installed.ID, ".apps.googleusercontent.com") || file.Installed.Secret == "" || strings.ContainsAny(file.Installed.ID, "\r\n\t ") {
		return problem("GOOGLE_CLIENT", "Keine gültige OAuth-JSON für eine Desktop-App.", "In Google Cloud einen Client vom Typ Desktop-App erstellen und dessen JSON auswählen.")
	}
	// Ignore URLs in imported files: credentials are sent only to the fixed Google endpoints.
	b, _ := json.Marshal(file.Installed)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.vault.Set("client", string(b)); err != nil {
		return vaultError()
	}
	return nil
}
func (g *Client) configured() (clientConfig, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	raw, err := g.vault.Get("client")
	var c clientConfig
	if err != nil || json.Unmarshal([]byte(raw), &c) != nil || c.ID == "" || c.Secret == "" {
		return c, problem("GOOGLE_CLIENT_MISSING", "Google OAuth ist noch nicht eingerichtet oder der Schlüsselbund ist gesperrt.", "OAuth-JSON importieren oder Schlüsselbund entsperren.")
	}
	return c, nil
}
func (g *Client) Configured() bool { _, err := g.configured(); return err == nil }
func (g *Client) oauth(c clientConfig) *oauth2.Config {
	return &oauth2.Config{ClientID: c.ID, ClientSecret: c.Secret, Endpoint: g.endpoint, Scopes: []string{SendScope, "openid", "email"}}
}
func (g *Client) oauthContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, g.http)
}
func randomID() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func authError(code string) error {
	switch code {
	case "access_denied":
		return diagnostics.Cancelled("Google-Anmeldung abgebrochen oder nicht erlaubt")
	case "admin_policy_enforced", "org_internal":
		return problem("GOOGLE_POLICY", "Google hat die Anmeldung durch eine Organisationsrichtlinie blockiert.", "Für dieses Konto ist eine Freigabe durch den Workspace-Administrator erforderlich.")
	case "invalid_grant":
		return problem("GOOGLE_RECONNECT", "Die Google-Freigabe ist abgelaufen oder wurde widerrufen.", "Google-Konto erneut verbinden, Verbindung und Testmail erneut prüfen. Im Google-Testmodus laufen Freigaben nach sieben Tagen ab.")
	case "invalid_client", "unauthorized_client":
		return problem("GOOGLE_CLIENT", "Google hat den OAuth-Client abgelehnt.", "Desktop-Client-JSON und Google-Cloud-Konfiguration prüfen.")
	default:
		return problem("GOOGLE_AUTH", "Google konnte die Anmeldung oder Token-Erneuerung nicht abschließen.", "Internetverbindung und Google-Freigabe prüfen. Keine Nachricht wurde hierbei versendet.")
	}
}
func tokenError(err error) error {
	var response *oauth2.RetrieveError
	if errors.As(err, &response) {
		return authError(response.ErrorCode)
	}
	return authError("") // OAuth errors may contain codes/tokens and are never logged verbatim.
}

// Login opens the system browser and accepts one state-bound loopback callback with PKCE.
func (g *Client) Login(ctx context.Context, open func(string) error) (outreach.Config, error) {
	c, err := g.configured()
	if err != nil {
		return outreach.Config{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return outreach.Config{}, problem("GOOGLE_CALLBACK", "Lokaler Anmeldeport konnte nicht geöffnet werden.", "Lokale Firewall prüfen und erneut versuchen.")
	}
	defer listener.Close()
	o := g.oauth(c)
	o.RedirectURL = "http://" + listener.Addr().String() + "/oauth/callback"
	state, verifier := randomID(), oauth2.GenerateVerifier()
	type callback struct{ code, failure string }
	result := make(chan callback, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if r.Method != "GET" || r.URL.Path != "/oauth/callback" || r.Host != listener.Addr().String() {
			http.Error(w, "Not found", 404)
			return
		}
		q := r.URL.Query()
		if len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid state", 400)
			return
		}
		if (q.Get("code") == "") == (q.Get("error") == "") || len(q["code"]) > 1 || len(q["error"]) > 1 {
			http.Error(w, "Invalid response", 400)
			return
		}
		select {
		case result <- callback{q.Get("code"), q.Get("error")}:
			fmt.Fprint(w, "Anmeldung empfangen. Zurueck zu Outreach wechseln.")
		default:
			http.Error(w, "Already received", 409)
		}
	})
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	link := o.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "consent select_account"))
	if err = open(link); err != nil {
		return outreach.Config{}, problem("GOOGLE_BROWSER", "Der Browser konnte nicht geöffnet werden.", "Standardbrowser prüfen und erneut anmelden.")
	}
	var answer callback
	select {
	case answer = <-result:
	case <-ctx.Done():
		return outreach.Config{}, problem("GOOGLE_TIMEOUT", "Die Google-Anmeldung wurde nicht rechtzeitig abgeschlossen.", "Erneut anmelden und den Vorgang im Browser abschließen.")
	}
	if answer.failure != "" {
		return outreach.Config{}, authError(answer.failure)
	}
	token, err := o.Exchange(g.oauthContext(ctx), answer.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return outreach.Config{}, tokenError(err)
	}
	granted, _ := token.Extra("scope").(string)
	if !containsScope(granted, SendScope) {
		return outreach.Config{}, problem("GOOGLE_SCOPE", "Die Gmail-Sendeberechtigung wurde nicht erteilt.", "Erneut verbinden und das Senden von E-Mails erlauben.")
	}
	if token.RefreshToken == "" {
		return outreach.Config{}, problem("GOOGLE_OFFLINE", "Google hat keine dauerhafte Freigabe erteilt.", "Outreach in den Google-Kontoverbindungen entfernen und erneut verbinden.")
	}
	i, err := g.identify(ctx, token.AccessToken)
	if err != nil {
		return outreach.Config{}, err
	}
	grant := randomID()
	stored := credential{Client: c, Email: i.Email, Subject: i.Subject, Refresh: token.RefreshToken}
	b, _ := json.Marshal(stored)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err = g.vault.Set("account/"+grant, string(b)); err != nil {
		return outreach.Config{}, vaultError()
	}
	g.tokens[grant] = token
	return outreach.Config{Provider: "google", GoogleGrant: grant, Username: i.Email}, nil
}
func containsScope(scopes, want string) bool {
	for _, s := range strings.Fields(scopes) {
		if s == want {
			return true
		}
	}
	return false
}
func (g *Client) identify(ctx context.Context, access string) (identity, error) {
	var i identity
	req, _ := http.NewRequestWithContext(ctx, "GET", g.userinfo, nil)
	req.Header.Set("Authorization", "Bearer "+access)
	res, err := g.http.Do(req)
	if err != nil {
		return i, authError("")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return i, apiError("Konto prüfen", res.StatusCode, res.Body)
	}
	if json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&i) != nil || !i.Verified || i.Subject == "" {
		return i, problem("GOOGLE_IDENTITY", "Google-Konto konnte nicht eindeutig bestätigt werden.", "Mit einem bestätigten Google-Konto erneut anmelden.")
	}
	i.Email, err = outreach.Address(i.Email)
	if err != nil {
		return i, authError("")
	}
	return i, nil
}
func (g *Client) access(ctx context.Context, c outreach.Config) (string, credential, error) {
	if err := c.Validate(); err != nil {
		return "", credential{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	raw, err := g.vault.Get("account/" + c.GoogleGrant)
	var saved credential
	if err != nil || json.Unmarshal([]byte(raw), &saved) != nil || saved.Refresh == "" {
		return "", saved, problem("GOOGLE_RECONNECT", "Google-Freigabe fehlt oder der Schlüsselbund ist gesperrt.", "Schlüsselbund entsperren oder Google-Konto erneut verbinden.")
	}
	if !strings.EqualFold(c.Username, saved.Email) || saved.Subject == "" {
		return "", saved, problem("GOOGLE_ACCOUNT", "Absender und Google-Freigabe stimmen nicht überein.", "Das gewünschte Google-Konto erneut verbinden.")
	}
	token := g.tokens[c.GoogleGrant]
	if token == nil {
		token = &oauth2.Token{RefreshToken: saved.Refresh}
	}
	if !token.Valid() {
		token, err = g.oauth(saved.Client).TokenSource(g.oauthContext(ctx), token).Token()
		if err != nil {
			return "", saved, tokenError(err)
		}
		if token.RefreshToken != "" && token.RefreshToken != saved.Refresh {
			saved.Refresh = token.RefreshToken
			b, _ := json.Marshal(saved)
			if err = g.vault.Set("account/"+c.GoogleGrant, string(b)); err != nil {
				return "", saved, vaultError()
			}
		}
		g.tokens[c.GoogleGrant] = token
	}
	return token.AccessToken, saved, nil
}
func (g *Client) Check(ctx context.Context, c outreach.Config) error {
	access, saved, err := g.access(ctx, c)
	if err != nil {
		return err
	}
	i, err := g.identify(ctx, access)
	if err != nil {
		return err
	}
	if i.Subject != saved.Subject || i.Email != saved.Email {
		return problem("GOOGLE_ACCOUNT", "Google-Konto hat sich geändert.", "Konto erneut verbinden und den Absender prüfen.")
	}
	return nil
}
func (g *Client) Forget(grant string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.vault.Delete("account/" + grant); err != nil {
		return vaultError()
	}
	delete(g.tokens, grant)
	return nil
}

// Only known reason codes are rendered. Provider response bodies can contain private data.
func apiError(step string, status int, body io.Reader) error {
	var response struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(body, 65536)).Decode(&response)
	hint := "Google-Freigabe und Internetverbindung prüfen. Es erfolgt kein automatischer erneuter Versand."
	if status == 401 {
		hint = "Google-Konto erneut verbinden und Verbindung sowie Testmail erneut prüfen."
	}
	if status == 429 {
		hint = "Google-Versandlimit erreicht. Später manuell fortsetzen; kein automatischer erneuter Versand."
	}
	if status >= 500 {
		hint = "Google meldet eine Störung. Die Nachricht könnte bereits angenommen worden sein; Status bleibt unklar und gesperrt."
	}
	for _, e := range response.Error.Errors {
		switch e.Reason {
		case "accessNotConfigured":
			hint = "Gmail API im Google-Cloud-Projekt aktivieren."
		case "insufficientPermissions":
			hint = "Google-Konto erneut verbinden und die Gmail-Sendeberechtigung erlauben."
		case "domainPolicy":
			hint = "Die Workspace-Richtlinie blockiert den Zugriff. Administrator-Freigabe erforderlich."
		case "rateLimitExceeded", "userRateLimitExceeded", "dailyLimitExceeded":
			hint = "Google-Versandlimit erreicht. Später manuell fortsetzen; kein automatischer erneuter Versand."
		}
	}
	return problem("GOOGLE_API", fmt.Sprintf("%s: Google API antwortet mit HTTP %d.", step, status), hint)
}
