package outreach

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestGoogleMIMEProvidesEquivalentSafeAlternatives(t *testing.T) {
	for _, tc := range []struct{ name, body, html string }{
		{"paragraphs", "Hello Jane,\n\nRegards\n", "Hello Jane,<br><br>Regards<br>"},
		{"windows", "Hello\r\n\r\nJane\r\n", "Hello<br><br>Jane<br>"},
		{"literal_markup", "<img src=x onerror=alert(1)> & <script>x</script> \"quoted\" 'text'\n", "&lt;img src=x onerror=alert(1)&gt; &amp; &lt;script&gt;x&lt;/script&gt; &#34;quoted&#34; &#39;text&#39;<br>"},
		{"unicode_spaces", "  Grüße 東京 😀\n\tA  B\n", "  Grüße 東京 😀<br>\tA  B<br>"},
		{"long_line", strings.Repeat("ä < > & ", 100), strings.Repeat("ä &lt; &gt; &amp; ", 100)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := validRequest().Config
			c.Provider, c.GoogleGrant = "google", "synthetic-grant"
			c.FromName, c.ReplyTo = "Sender Müller", "reply@example.org"
			m := samplePlan(t).Messages[0]
			m.Subject, m.Body = "Grüße 東京", tc.body
			raw, err := MIME(c, m)
			if err != nil {
				t.Fatal(err)
			}
			msg, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
			if err != nil || subject != m.Subject || msg.Header.Get("Message-ID") != m.MessageID || msg.Header.Get("Reply-To") != "<reply@example.org>" || msg.Header.Get("Bcc") != "" || msg.Header.Get("Cc") != "" {
				t.Fatal("message headers changed")
			}
			to, err := msg.Header.AddressList("To")
			if err != nil || len(to) != 1 || to[0].Address != m.To {
				t.Fatal("recipient changed")
			}
			mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
			if err != nil || mediaType != "multipart/alternative" || params["boundary"] == "" {
				t.Fatal("missing MIME alternatives")
			}
			reader := multipart.NewReader(msg.Body, params["boundary"])
			for i, wantType := range []string{"text/plain", "text/html"} {
				part, err := reader.NextPart() // Also decodes quoted-printable.
				if err != nil {
					t.Fatal(err)
				}
				partType, attrs, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
				if err != nil || partType != wantType || attrs["charset"] != "UTF-8" {
					t.Fatal("incorrect MIME part", part.Header)
				}
				body, err := io.ReadAll(part)
				if err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					if strings.ReplaceAll(string(body), "\r\n", "\n") != strings.ReplaceAll(tc.body, "\r\n", "\n") {
						t.Fatal("plain text changed")
					}
				} else {
					want := "<!DOCTYPE html><html><body><div style=\"white-space:pre-wrap\">" + tc.html + "</div></body></html>"
					if string(body) != want {
						t.Fatalf("HTML changed content: %q", body)
					}
				}
			}
			if _, err := reader.NextPart(); err != io.EOF {
				t.Fatal("unexpected extra part or incomplete MIME", err)
			}
			for _, line := range strings.Split(string(raw), "\r\n") {
				if len(line) > 998 {
					t.Fatal("MIME line exceeds transport limit")
				}
			}
		})
	}
}

func TestGoogleMIMERejectsInvalidContent(t *testing.T) {
	c := validRequest().Config
	c.Provider, c.GoogleGrant = "google", "synthetic-grant"
	for _, mutate := range []func(*Message){
		func(m *Message) { m.To += "\r\nBcc: victim@example.org" },
		func(m *Message) { m.Subject += "\r\nBcc: victim@example.org" },
		func(m *Message) { m.MessageID += "\r\nBcc: victim@example.org" },
		func(m *Message) { m.Body = "bad\x00content" },
		func(m *Message) { m.Body = " \n" },
	} {
		m := samplePlan(t).Messages[0]
		mutate(&m)
		if _, err := MIME(c, m); err == nil {
			t.Fatal("invalid content accepted")
		}
	}
}
