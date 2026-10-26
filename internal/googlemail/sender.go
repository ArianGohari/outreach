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
		return fail("uncertain", problem("GOOGLE_SEND_UNCERTAIN", "Google did not confirm acceptance.", "The message may already have been accepted. It will not be retried automatically."))
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		status := "uncertain"
		if res.StatusCode >= 400 && res.StatusCode < 500 {
			status = "failed"
		}
		return fail(status, apiError("Send email", res.StatusCode, res.Body))
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&result) != nil || result.ID == "" {
		return fail("uncertain", problem("GOOGLE_SEND_UNCERTAIN", "Google did not return a valid acceptance confirmation.", "The message may already have been accepted. Sending it again remains blocked."))
	}
	return outreach.SendResult{Status: "sent", Detail: "Accepted by Google"}
}
