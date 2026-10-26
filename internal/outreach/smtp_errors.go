package outreach

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"outreach/internal/diagnostics"
	"strings"
	"syscall"
)

func SMTPFailure(step string, c Config, cause error, uncertain bool, private ...string) *diagnostics.Fault {
	titles := map[string]string{"CONNECT": "SMTP server unreachable", "TLS": "TLS connection failed", "GREETING": "SMTP greeting failed", "STARTTLS": "STARTTLS failed", "AUTH": "SMTP authentication failed", "NOOP": "SMTP connection check failed", "MAIL FROM": "SMTP sender rejected", "RCPT TO": "SMTP recipient rejected", "DATA": "SMTP server rejected message transfer", "BODY": "Connection lost during transfer", "ACK": "SMTP acceptance confirmation missing"}
	title := titles[step]
	if title == "" {
		title = "SMTP error"
	}
	hint := "Check the SMTP server, port and credentials with your provider."
	detail := fmt.Sprintf("Server %s, port %d. Step: %s.", c.Host, c.Port, step)
	code := "SMTP_" + strings.ReplaceAll(step, " ", "_")
	private = append(private, c.Password, c.Username, c.Sender(), c.ReplyTo, "\x00"+c.Username+"\x00"+c.Password)
	if cause != nil {
		detail += "\n" + diagnostics.Redact(cause.Error(), private...)
	}
	var reply *textproto.Error
	if errors.As(cause, &reply) {
		detail = fmt.Sprintf("Server %s, port %d. Step: %s.\nSMTP %d: %s", c.Host, c.Port, step, reply.Code, diagnostics.Redact(reply.Msg, private...))
		if reply.Code/100 == 4 {
			hint = "The server reported a temporary error. Check provider limits or outages; messages are not retried automatically."
		}
	}
	var dns *net.DNSError
	var netErr net.Error
	var hostname x509.HostnameError
	var unknownCA x509.UnknownAuthorityError
	var invalidCert x509.CertificateInvalidError
	switch {
	case errors.As(cause, &dns):
		code = "SMTP_DNS"
		hint = "Check the SMTP hostname for typos and verify your internet and DNS connection."
	case errors.As(cause, &netErr) && netErr.Timeout():
		code = "SMTP_TIMEOUT"
		hint = "Check your internet connection, firewall and SMTP port. The server did not respond within the time limit."
	case errors.Is(cause, syscall.ECONNREFUSED):
		code = "SMTP_REFUSED"
		hint = "Connection refused on this port. Check the SMTP port and server address with your provider."
	case errors.As(cause, &hostname):
		code = "SMTP_CERTIFICATE_HOST"
		hint = "The certificate name does not match the SMTP hostname. Use the official SMTP hostname; certificate verification remains enabled."
	case errors.As(cause, &unknownCA):
		code = "SMTP_CERTIFICATE_TRUST"
		hint = "The server certificate is not trusted. Check the certificate chain with your provider; certificate verification remains enabled."
	case errors.As(cause, &invalidCert):
		code = "SMTP_CERTIFICATE_INVALID"
		hint = "Check the system clock and server certificate validity. Certificate verification remains enabled."
	case step == "AUTH":
		hint = "Check the username, app password and SMTP access. For Google Workspace without an app password, select Google as the provider."
	case step == "MAIL FROM":
		hint = "The sender address must be authorized for this SMTP account. Check the sender address or alias with your provider."
	case step == "RCPT TO":
		hint = "Check the recipient address in the CSV and the server response. The server rejected this recipient."
	case errors.Is(cause, io.EOF) || errors.Is(cause, io.ErrUnexpectedEOF):
		hint = "The server closed the connection. Check the SMTP port, provider status and connection stability."
	}
	if uncertain {
		hint += " The message may already have been accepted. Its status remains uncertain; it will not be retried automatically."
	}
	f := diagnostics.New(code, title, diagnostics.Redact(detail, private...), hint, cause)
	f.Step = step
	if reply != nil {
		f.SMTPCode = reply.Code
	}
	return f
}
