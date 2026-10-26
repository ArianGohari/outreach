package googlemail

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

const testClient = `{"installed":{"client_id":"test.apps.googleusercontent.com","client_secret":"client-secret","auth_uri":"https://untrusted.example.org","token_uri":"https://untrusted.example.org"}}`

type fixture struct {
	g          *Client
	values     map[string]string
	sends      atomic.Int32
	refreshes  atomic.Int32
	challenge  string
	sendStatus int
	sendBody   string
	tokenError string
	scope      string
	email      string
	verified   bool
	drop       atomic.Bool
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{values: map[string]string{}, sendStatus: 200, sendBody: `{"id":"delivered"}`, scope: SendScope + " openid email", email: "sender@example.org", verified: true}
	g := New(Vault{
		Get: func(key string) (string, error) {
			v, ok := f.values[key]
			if !ok {
				return "", errors.New("not found")
			}
			return v, nil
		},
		Set:    func(key, value string) error { f.values[key] = value; return nil },
		Delete: func(key string) error { delete(f.values, key); return nil },
	})
	f.g = g
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			if r.Form.Get("client_secret") != "client-secret" || r.Form.Get("client_id") != "test.apps.googleusercontent.com" {
				t.Error("wrong OAuth client")
			}
			if r.Form.Get("grant_type") == "authorization_code" {
				h := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				if base64.RawURLEncoding.EncodeToString(h[:]) != f.challenge || r.Form.Get("code") != "synthetic-code" {
					t.Error("PKCE/code not verified")
				}
			} else {
				f.refreshes.Add(1)
				if r.Form.Get("refresh_token") != "refresh-secret" {
					t.Error("wrong refresh token")
				}
			}
			if f.tokenError != "" {
				w.WriteHeader(400)
				fmt.Fprintf(w, `{"error":%q,"error_description":"private-token-never-log"}`, f.tokenError)
				return
			}
			fmt.Fprintf(w, `{"access_token":"access-secret","refresh_token":"refresh-secret","token_type":"Bearer","expires_in":3600,"scope":%q}`, f.scope)
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer access-secret" {
				t.Error("missing access token")
			}
			fmt.Fprintf(w, `{"email":%q,"email_verified":%t,"sub":"google-person-1"}`, f.email, f.verified)
		case "/send":
			f.sends.Add(1)
			if r.Header.Get("Authorization") != "Bearer access-secret" {
				t.Error("wrong API authentication")
			}
			var payload struct {
				Raw string `json:"raw"`
			}
			if json.NewDecoder(r.Body).Decode(&payload) != nil {
				t.Error("bad API body")
			}
			data, err := base64.RawURLEncoding.DecodeString(payload.Raw)
			if err != nil || !strings.Contains(string(data), "Content-Type: text/plain") || strings.Contains(string(data), "{{") {
				t.Error("bad MIME")
			}
			if f.drop.Load() {
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
				return
			}
			w.Header().Set("Location", "/unexpected-retry")
			w.WriteHeader(f.sendStatus)
			fmt.Fprint(w, f.sendBody)
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	g.http = server.Client()
	g.http.Timeout = time.Second
	g.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	g.endpoint.TokenURL = server.URL + "/token"
	g.userinfo = server.URL + "/userinfo"
	g.sendURL = server.URL + "/send"
	if err := g.ImportClient([]byte(testClient)); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *fixture) browser(t *testing.T, failure string) func(string) error {
	return func(link string) error {
		u, err := url.Parse(link)
		if err != nil {
			return err
		}
		q := u.Query()
		f.challenge = q.Get("code_challenge")
		if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" || !containsScope(q.Get("scope"), SendScope) || containsScope(q.Get("scope"), "https://mail.google.com/") {
			t.Error("unsafe OAuth request")
		}
		callback, _ := url.Parse(q.Get("redirect_uri"))
		if callback.Hostname() != "127.0.0.1" {
			t.Error("non-loopback callback")
		}
		values := url.Values{"state": {q.Get("state")}, "code": {"synthetic-code"}}
		if failure != "" {
			values.Del("code")
			values.Set("error", failure)
		}
		// An unrelated local request must not consume the real callback.
		bad := *callback
		bad.RawQuery = "state=wrong&code=stolen"
		res, err := http.Get(bad.String())
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Error("bad state accepted")
		}
		callback.RawQuery = values.Encode()
		res, err = http.Get(callback.String())
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Error("valid callback refused")
		}
		return nil
	}
}
func (f *fixture) login(t *testing.T) outreach.Config {
	t.Helper()
	c, err := f.g.Login(context.Background(), f.browser(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestOAuthLoginPKCEIdentityAndSecretStorage(t *testing.T) {
	f := setup(t)
	c := f.login(t)
	if c.Username != f.email || c.Provider != "google" || c.GoogleGrant == "" || c.Password != "" {
		t.Fatal("bad account", c.Public())
	}
	for _, value := range f.values {
		if strings.Contains(value, "access-secret") || strings.Contains(value, "synthetic-code") {
			t.Fatal("short-lived secrets persisted")
		}
	}
	if err := f.g.Check(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c.Username = "different@example.org"
	if err := f.g.Check(context.Background(), c); err == nil {
		t.Fatal("account substitution accepted")
	}
	if f.sends.Load() != 0 {
		t.Fatal("login/check sent mail")
	}
}
func TestOAuthClientValidationAndFixedEndpoints(t *testing.T) {
	f := setup(t)
	for _, data := range []string{`{`, `{"web":{"client_id":"test","client_secret":"x"}}`, `{"installed":{"client_id":"x","client_secret":"x"}}`, strings.Repeat("x", 65537)} {
		if err := f.g.ImportClient([]byte(data)); err == nil {
			t.Fatal("bad client accepted")
		}
	}
	if f.g.endpoint.AuthURL != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Fatal("import replaced Google URL")
	}
	delete(f.values, "client")
	f.g.client = clientConfig{}
	if f.g.Configured() {
		t.Fatal("missing client shown as configured")
	}
	if _, err := f.g.Login(context.Background(), nil); err == nil {
		t.Fatal("login without client")
	}
}
func TestOAuthFailurePathsNeverAuthorizeOrLeak(t *testing.T) {
	for _, mode := range []string{"cancel", "policy", "scope", "unverified", "browser", "timeout", "exchange", "vault"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			open := f.browser(t, "")
			switch mode {
			case "cancel":
				open = f.browser(t, "access_denied")
			case "policy":
				open = f.browser(t, "admin_policy_enforced")
			case "scope":
				f.scope = "openid email"
			case "unverified":
				f.verified = false
			case "browser":
				open = func(string) error { return errors.New("private-token-never-log") }
			case "timeout":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				open = func(string) error { return nil }
			case "exchange":
				f.tokenError = "invalid_client"
			case "vault":
				f.g.vault.Set = func(string, string) error { return errors.New("private-token-never-log") }
			}
			c, err := f.g.Login(ctx, open)
			if err == nil || c.GoogleGrant != "" || strings.Contains(err.Error(), "private-token-never-log") {
				t.Fatal("unsafe failed login", err)
			}
			if len(f.values) != 1 || f.sends.Load() != 0 {
				t.Fatal("failed login retained a grant or sent mail")
			}
		})
	}
}
func TestRefreshSerializedAndRevokedGrantFailsClosed(t *testing.T) {
	f := setup(t)
	c := f.login(t)
	f.g.tokens = map[string]*oauth2.Token{} // Simulate restart: only the refresh token survives.
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f.g.Check(context.Background(), c); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.refreshes.Load() != 1 {
		t.Fatal("parallel token refresh", f.refreshes.Load())
	}
	f.g.tokens = map[string]*oauth2.Token{}
	f.tokenError = "invalid_grant"
	if err := f.g.Check(context.Background(), c); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("revoked grant accepted/leaked", err)
	}
	if err := f.g.Forget(c.GoogleGrant); err != nil {
		t.Fatal(err)
	}
	if err := f.g.Check(context.Background(), c); err == nil {
		t.Fatal("deleted token reused")
	}
}
func TestGoogleSendIsSingleAttemptAndClassifiesUncertainty(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, want string
		drop       bool
	}{
		{"accepted", 200, `{"id":"ok"}`, "sent", false}, {"malformed", 200, `{`, "uncertain", false}, {"missing-id", 200, `{}`, "uncertain", false},
		{"denied", 403, `{"error":{"errors":[{"reason":"domainPolicy"}]}}`, "failed", false},
		{"expired", 401, `{}`, "failed", false}, {"limit", 429, `{}`, "failed", false}, {"server", 503, `{}`, "uncertain", false},
		{"redirect", 307, `{}`, "uncertain", false}, {"lost-ack", 200, `{}`, "uncertain", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			c := f.login(t)
			f.sendStatus, f.sendBody = tc.status, tc.body
			f.drop.Store(tc.drop)
			m := outreach.Message{To: "recipient@example.org", Subject: "Hello", Body: "Personalized\n", MessageID: "<one@outreach.local>"}
			r := f.g.Send(context.Background(), c, m)
			if r.Status != tc.want || f.sends.Load() != 1 {
				t.Fatal("incorrect result/retry", r.Status, f.sends.Load())
			}
		})
	}
}

type fastClock struct{}

func (fastClock) Now() time.Time                                  { return time.Now() }
func (fastClock) Wait(ctx context.Context, _ time.Duration) error { return ctx.Err() }
func TestGoogleWorkflowSharesPersistentDuplicateProtection(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprint("lost_ack_", lost), func(t *testing.T) {
			f := setup(t)
			c := f.login(t)
			store, err := outreach.OpenStore(filepath.Join(t.TempDir(), "google.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			jobs, err := outreach.NewJobs(store, Sender{Google: f.g, SMTP: outreach.SMTPSender{}}, fastClock{}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer jobs.Close()
			for attempt := 0; attempt < 2; attempt++ {
				f.drop.Store(false)
				v, err := jobs.Create("Google test")
				if err != nil {
					t.Fatal(err)
				}
				job, _ := jobs.Get(v.ID)
				_, err = job.Session.Import([]byte("First Name,Last Name,Company Name,Email\nJane,Doe,Example,jane@example.org\n"), "synthetic.csv", false)
				if err != nil {
					t.Fatal(err)
				}
				r := outreach.Request{Config: c, Subject: "Hello {{First Name}}", Body: "Hi {{Company}}", Schedule: outreach.Schedule{Seconds: 1, BatchSize: 50}}
				if err = job.Session.Check(context.Background(), c); err != nil {
					t.Fatal(err)
				}
				token, err := job.Session.Test(context.Background(), r, 0, "test@example.org")
				if err != nil {
					t.Fatal(err)
				}
				f.drop.Store(lost && attempt == 0)
				if err = jobs.Start(job, r, token); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(3 * time.Second)
				for {
					s, err := job.Engine.State()
					if err != nil {
						t.Fatal(err)
					}
					if !s.Running {
						want := "sent"
						if lost {
							want = "uncertain"
						}
						if attempt == 1 {
							want = "blocked"
						}
						if len(s.Deliveries) != 1 || s.Deliveries[0].Status != want {
							t.Fatal(s)
						}
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("worker stuck")
					}
					time.Sleep(time.Millisecond)
				}
				if err = jobs.Delete(v.ID); err != nil {
					t.Fatal(err)
				}
			}
			if f.sends.Load() != 3 {
				t.Fatal("duplicate outreach across deleted jobs", f.sends.Load())
			}
		})
	}
}

func TestHTTPFailuresDoNotExposeProviderBodies(t *testing.T) {
	err := apiError("send", 403, strings.NewReader(`{"error":{"message":"access-secret recipient@example.org","errors":[{"reason":"unknown-private-data"}]}}`))
	if strings.Contains(err.Error(), "access-secret") || strings.Contains(err.Error(), "recipient@example.org") || strings.Contains(err.Error(), "unknown-private-data") {
		t.Fatal("provider data leaked")
	}
}

func TestBundledClientSignsInWithoutLocalImport(t *testing.T) {
	f := setup(t)
	// Simulate a fresh installation: only the build-provided client exists.
	delete(f.values, "client")
	c := f.login(t)
	if len(f.values) != 1 || c.GoogleGrant == "" || f.sends.Load() != 0 {
		t.Fatal("sign-in required setup, stored unrelated data or sent mail")
	}
	if _, ok := f.values["client"]; ok {
		t.Fatal("bundled client was unnecessarily written into the keyring")
	}
}

func TestClientUpdateKeepsExistingAccountCredentials(t *testing.T) {
	f := setup(t)
	c := f.login(t)
	// A future release may use a new app client. Existing account refresh tokens
	// must still be exchanged with the client they were originally issued to.
	f.g.client = clientConfig{ID: "new.apps.googleusercontent.com", Secret: "new-client"}
	f.g.tokens = map[string]*oauth2.Token{}
	if err := f.g.Check(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if f.refreshes.Load() != 1 || f.sends.Load() != 0 {
		t.Fatal("account was not restored safely")
	}
}

func TestBundledClientPrecedesLegacySetup(t *testing.T) {
	f := setup(t)
	f.values["client"] = `{"client_id":"old.apps.googleusercontent.com","client_secret":"old"}`
	f.login(t) // The local token endpoint rejects any client other than the bundled fixture.
}

func TestDeveloperBuildUsesLegacySetup(t *testing.T) {
	f := setup(t)
	f.g.client = clientConfig{}
	f.login(t)
}

func TestConstructorUsesBuildClientWithoutOpeningKeyring(t *testing.T) {
	g := New(Vault{Get: func(string) (string, error) { t.Fatal("constructor read keyring"); return "", nil }})
	if g.client != bundledClient {
		t.Fatal("constructor discarded bundled client")
	}
}

func TestMissingClientFailsBeforeOpeningBrowser(t *testing.T) {
	g := New(Vault{Get: func(string) (string, error) { return "", errors.New("unavailable") }})
	g.client = clientConfig{}
	_, err := g.Login(context.Background(), func(string) error { t.Fatal("opened browser without a client"); return nil })
	var p *diagnostics.Fault
	if !errors.As(err, &p) || p.Code != "GOOGLE_CLIENT_MISSING" {
		t.Fatal("missing client not explained", err)
	}
}
