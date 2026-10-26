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
	titles := map[string]string{"CONNECT": "SMTP-Server nicht erreichbar", "TLS": "TLS-Verbindung fehlgeschlagen", "GREETING": "SMTP-Begrüßung fehlgeschlagen", "STARTTLS": "STARTTLS fehlgeschlagen", "AUTH": "SMTP-Anmeldung fehlgeschlagen", "NOOP": "SMTP-Verbindungsprüfung fehlgeschlagen", "MAIL FROM": "SMTP-Absender wurde abgelehnt", "RCPT TO": "SMTP-Empfänger wurde abgelehnt", "DATA": "SMTP-Server hat die Nachrichtenübertragung abgelehnt", "BODY": "Verbindung während der Übertragung verloren", "ACK": "SMTP-Versandbestätigung fehlt"}
	title := titles[step]
	if title == "" {
		title = "SMTP-Fehler"
	}
	hint := "SMTP-Server, Port und Zugangsdaten beim Anbieter prüfen."
	detail := fmt.Sprintf("Server %s, Port %d. Schritt: %s.", c.Host, c.Port, step)
	code := "SMTP_" + strings.ReplaceAll(step, " ", "_")
	private = append(private, c.Password, c.Username, c.Sender(), c.ReplyTo, "\x00"+c.Username+"\x00"+c.Password)
	if cause != nil {
		detail += "\n" + diagnostics.Redact(cause.Error(), private...)
	}
	var reply *textproto.Error
	if errors.As(cause, &reply) {
		detail = fmt.Sprintf("Server %s, Port %d. Schritt: %s.\nSMTP %d: %s", c.Host, c.Port, step, reply.Code, diagnostics.Redact(reply.Msg, private...))
		if reply.Code/100 == 4 {
			hint = "Der Server meldet einen vorübergehenden Fehler. Anbieterlimits oder Störungen prüfen; es erfolgt kein automatischer erneuter Versand."
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
		hint = "SMTP-Hostnamen auf Tippfehler prüfen und Internet-/DNS-Verbindung kontrollieren."
	case errors.As(cause, &netErr) && netErr.Timeout():
		code = "SMTP_TIMEOUT"
		hint = "Internetverbindung, Firewall und SMTP-Port prüfen. Der Server hat nicht innerhalb des Zeitlimits geantwortet."
	case errors.Is(cause, syscall.ECONNREFUSED):
		code = "SMTP_REFUSED"
		hint = "Auf diesem Port wurde die Verbindung abgelehnt. SMTP-Port und Serveradresse beim Anbieter prüfen."
	case errors.As(cause, &hostname):
		code = "SMTP_CERTIFICATE_HOST"
		hint = "Der Zertifikatsname passt nicht zum SMTP-Hostnamen. Den offiziellen SMTP-Hostnamen verwenden; Zertifikatsprüfung bleibt aktiv."
	case errors.As(cause, &unknownCA):
		code = "SMTP_CERTIFICATE_TRUST"
		hint = "Das Serverzertifikat ist nicht vertrauenswürdig. Zertifikatskette beim Anbieter prüfen; Zertifikatsprüfung bleibt aktiv."
	case errors.As(cause, &invalidCert):
		code = "SMTP_CERTIFICATE_INVALID"
		hint = "Systemzeit und Gültigkeit des Serverzertifikats prüfen. Zertifikatsprüfung bleibt aktiv."
	case step == "AUTH":
		hint = "Benutzername, Passwort bzw. App-Passwort und aktivierten SMTP-Zugriff prüfen. Die App verwendet SMTP AUTH PLAIN über TLS; OAuth wird nicht unterstützt."
	case step == "MAIL FROM":
		hint = "Die Absenderadresse muss für dieses SMTP-Konto freigegeben sein. Absenderadresse oder Alias beim Anbieter prüfen."
	case step == "RCPT TO":
		hint = "Empfängeradresse in der CSV und die genaue Serverantwort prüfen. Der Server hat diesen Empfänger nicht angenommen."
	case errors.Is(cause, io.EOF) || errors.Is(cause, io.ErrUnexpectedEOF):
		hint = "Der Server hat die Verbindung geschlossen. SMTP-Port, Anbieterstatus und Verbindungsstabilität prüfen."
	}
	if uncertain {
		hint += " Die Mail könnte bereits angenommen worden sein. Ihr Status bleibt unklar; kein automatischer erneuter Versand."
	}
	f := diagnostics.New(code, title, diagnostics.Redact(detail, private...), hint, cause)
	f.Step = step
	if reply != nil {
		f.SMTPCode = reply.Code
	}
	return f
}
