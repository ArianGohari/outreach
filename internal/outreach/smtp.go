package outreach

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"outreach/internal/diagnostics"
	"strings"
	"time"
)

type SendResult struct {
	Status, Detail string
	Failure        error
}

func (r SendResult) Err() error {
	if r.Failure != nil {
		return r.Failure
	}
	return fmt.Errorf("%s", r.Detail)
}
func failedSend(status string, err error) SendResult {
	return SendResult{Status: status, Detail: err.Error(), Failure: err}
}

type Sender interface {
	Check(context.Context, Config) error
	Send(context.Context, Config, Message) SendResult
}
type SMTPSender struct {
	tlsConfig   *tls.Config
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func (sender SMTPSender) connect(ctx context.Context, c Config) (*smtp.Client, net.Conn, error) {
	if e := c.Validate(); e != nil {
		return nil, nil, e
	}
	addr := net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
	d := net.Dialer{Timeout: 20 * time.Second}
	dial := d.DialContext
	if sender.dialContext != nil {
		dial = sender.dialContext
	}
	conn, e := dial(ctx, "tcp", addr)
	if e != nil {
		return nil, nil, SMTPFailure("CONNECT", c, e, false)
	}
	deadline := time.Now().Add(45 * time.Second)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	if e = conn.SetDeadline(deadline); e != nil {
		conn.Close()
		return nil, nil, SMTPFailure("CONNECT", c, e, false)
	}
	fail := func(err error) (*smtp.Client, net.Conn, error) { conn.Close(); return nil, nil, err }
	tlsConf := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if sender.tlsConfig != nil {
		tlsConf = sender.tlsConfig.Clone()
		tlsConf.ServerName = c.Host
	}
	if c.Port == 465 {
		secure := tls.Client(conn, tlsConf)
		if e = secure.HandshakeContext(ctx); e != nil {
			return fail(SMTPFailure("TLS", c, e, false))
		}
		conn = secure
	}
	client, e := smtp.NewClient(conn, c.Host)
	if e != nil {
		return fail(SMTPFailure("GREETING", c, e, false))
	}
	if e = client.Hello("localhost"); e != nil {
		return fail(SMTPFailure("GREETING", c, e, false))
	}
	if c.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fail(diagnostics.New("SMTP_STARTTLS_UNAVAILABLE", "SMTP-Server bietet kein STARTTLS an", fmt.Sprintf("Server %s, Port %d bietet keine sichere STARTTLS-Verbindung an.", c.Host, c.Port), "SMTP-Port prüfen: 465 verwendet direkt TLS; andere Ports müssen STARTTLS anbieten. Es wurden keine Zugangsdaten übertragen.", nil))
		}
		if e = client.StartTLS(tlsConf); e != nil {
			return fail(SMTPFailure("STARTTLS", c, e, false))
		}
	}
	if e = client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); e != nil {
		return fail(SMTPFailure("AUTH", c, e, false))
	}
	return client, conn, nil
}
func (sender SMTPSender) Check(ctx context.Context, c Config) error {
	client, conn, e := sender.connect(ctx, c)
	if e != nil {
		return e
	}
	defer conn.Close()
	if e = client.Noop(); e != nil {
		return SMTPFailure("NOOP", c, e, false)
	}
	return nil
}

func MIME(c Config, m Message) ([]byte, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	if _, e := Address(m.To); e != nil {
		return nil, e
	}
	if strings.TrimSpace(m.Subject) == "" || invalidText(m.Subject, false) || strings.TrimSpace(m.Body) == "" || invalidText(m.Body, true) {
		return nil, fmt.Errorf("Ungültiger Nachrichteninhalt")
	}
	if !strings.HasPrefix(m.MessageID, "<") || !strings.HasSuffix(m.MessageID, ">") || invalidText(m.MessageID, false) {
		return nil, fmt.Errorf("Ungültige Message-ID")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\n", (&mail.Address{Name: c.FromName, Address: c.Sender()}).String(), (&mail.Address{Address: m.To}).String(), mime.QEncoding.Encode("UTF-8", m.Subject))
	if c.ReplyTo != "" {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", (&mail.Address{Address: c.ReplyTo}).String())
	}
	fmt.Fprintf(&b, "Date: %s\r\nMessage-ID: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", time.Now().Format(time.RFC1123Z), m.MessageID)
	q := quotedprintable.NewWriter(&b)
	if _, e := q.Write([]byte(m.Body)); e != nil {
		return nil, e
	}
	if e := q.Close(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
func (sender SMTPSender) Send(ctx context.Context, c Config, m Message) SendResult {
	data, e := MIME(c, m)
	if e != nil {
		return failedSend("failed", e)
	}
	client, conn, e := sender.connect(ctx, c)
	if e != nil {
		return failedSend("failed", e)
	}
	defer conn.Close()
	// Stop does not cancel an in-flight transaction; it prevents the next claim.
	if e = client.Mail(c.Sender()); e != nil {
		return failedSend("failed", SMTPFailure("MAIL FROM", c, e, false, m.To, m.Subject, m.Body))
	}
	if e = client.Rcpt(m.To); e != nil {
		return failedSend("failed", SMTPFailure("RCPT TO", c, e, false, m.To, m.Subject, m.Body))
	}
	w, e := client.Data()
	if e != nil {
		return failedSend("failed", SMTPFailure("DATA", c, e, false, m.To, m.Subject, m.Body))
	}
	if _, e = w.Write(data); e != nil {
		return failedSend("uncertain", SMTPFailure("BODY", c, e, true, m.To, m.Subject, m.Body))
	}
	if e = w.Close(); e != nil {
		return failedSend("uncertain", SMTPFailure("ACK", c, e, true, m.To, m.Subject, m.Body))
	}
	// A lost QUIT reply cannot undo the final positive DATA acknowledgement.
	_ = client.Quit()
	return SendResult{Status: "sent", Detail: "An SMTP-Server übergeben"}
}
func TestMessage(m Message, to string) (Message, error) {
	a, e := Address(to)
	if e != nil {
		return Message{}, e
	}
	var b [16]byte
	if _, e = rand.Read(b[:]); e != nil {
		return Message{}, e
	}
	m.To = a
	m.MessageID = "<test-" + hex.EncodeToString(b[:]) + "@outreach.local>"
	return m, nil
}
