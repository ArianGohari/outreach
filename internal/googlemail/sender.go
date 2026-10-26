package googlemail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"outreach/internal/outreach"
)

type Sender struct {
	Google *Client
	SMTP   outreach.Sender
}

func (s Sender) Check(ctx context.Context, c outreach.Config) error {
	if c.Provider == "google" {
		return s.Google.Check(ctx, c)
	}
	return s.SMTP.Check(ctx, c)
}
func (s Sender) Send(ctx context.Context, c outreach.Config, m outreach.Message) outreach.SendResult {
	if c.Provider == "google" {
		return s.Google.Send(ctx, c, m)
	}
	return s.SMTP.Send(ctx, c, m)
}
func (g *Client) Send(ctx context.Context, c outreach.Config, m outreach.Message) outreach.SendResult {
	fail := func(status string, err error) outreach.SendResult {
		return outreach.SendResult{Status: status, Detail: err.Error(), Failure: err}
	}
	data, err := outreach.MIME(c, m)
	if err != nil {
		return fail("failed", err)
	}
	access, _, err := g.access(ctx, c)
	if err != nil {
		return fail("failed", err)
	}
	body, _ := json.Marshal(struct {
		Raw string `json:"raw"`
	}{base64.RawURLEncoding.EncodeToString(data)})
	// No GetBody, redirects, token-auth transport or retry wrapper: a send is attempted once.
	req, err := http.NewRequestWithContext(ctx, "POST", g.sendURL, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return fail("failed", authError(""))
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	res, err := g.http.Do(req)
	if err != nil {
		return fail("uncertain", problem("GOOGLE_SEND_UNCERTAIN", "Die Versandbestätigung von Google fehlt.", "Die Mail könnte bereits angenommen worden sein. Kein automatischer erneuter Versand."))
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		status := "uncertain"
		if res.StatusCode >= 400 && res.StatusCode < 500 {
			status = "failed"
		}
		return fail(status, apiError("E-Mail senden", res.StatusCode, res.Body))
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&result) != nil || result.ID == "" {
		return fail("uncertain", problem("GOOGLE_SEND_UNCERTAIN", "Google hat keine eindeutige Versandbestätigung geliefert.", "Die Mail könnte bereits angenommen worden sein. Erneuter Versand bleibt gesperrt."))
	}
	return outreach.SendResult{Status: "sent", Detail: "An Google übergeben"}
}
