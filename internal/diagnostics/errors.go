package diagnostics

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Fault struct {
	Code, Message, Details, Hint, Step string
	SMTPCode                           int
	cause                              error
}

func New(code, message, details, hint string, cause error) *Fault {
	return &Fault{Code: code, Message: message, Details: details, Hint: hint, cause: cause}
}
func (f *Fault) Error() string { return f.Message + ": " + f.Details }
func (f *Fault) Unwrap() error { return f.cause }

type Problem struct {
	ID        string `json:"id"`
	Level     string `json:"level"`
	Code      string `json:"code"`
	Operation string `json:"operation"`
	Message   string `json:"message"`
	Details   string `json:"details"`
	Hint      string `json:"hint"`
	Step      string `json:"step,omitempty"`
	SMTPCode  int    `json:"smtpCode,omitempty"`
	Time      string `json:"time"`
	LogPath   string `json:"logPath,omitempty"`
	Logged    bool   `json:"logged"`
	LogIssue  string `json:"logIssue,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

func (p *Problem) Error() string {
	msg := p.Message
	if p.Details != "" {
		msg += "\n" + p.Details
	}
	if p.Hint != "" {
		msg += "\n" + p.Hint
	}
	if p.ID != "" {
		msg += "\nFehler-ID: " + p.ID
	}
	return msg
}
func Cancelled(message string) error {
	return &Problem{Code: "CANCELLED", Message: message, Cancelled: true}
}

var emailPattern = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9.-]+`)
var credentialPattern = regexp.MustCompile(`(?i)(password|passwort|authorization|bearer|token|secret|api[_-]?key)(\s*[:=]\s*)[^\s,;]+`)
var authPattern = regexp.MustCompile(`(?i)AUTH\s+(PLAIN|LOGIN|XOAUTH2)\s+\S+`)

func Redact(value string, private ...string) string {
	replacements := make([]string, 0, len(private)*3)
	for _, v := range private {
		if v != "" {
			replacements = append(replacements, v, base64.StdEncoding.EncodeToString([]byte(v)), base64.RawStdEncoding.EncodeToString([]byte(v)))
		}
	}
	sort.Slice(replacements, func(i, j int) bool { return len(replacements[i]) > len(replacements[j]) })
	for _, v := range replacements {
		value = strings.ReplaceAll(value, v, "[geschützt]")
	}
	value = authPattern.ReplaceAllString(value, "AUTH [geschützt]")
	value = credentialPattern.ReplaceAllString(value, "$1=[geschützt]")
	value = emailPattern.ReplaceAllString(value, "[E-Mail-Adresse]")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	if len([]rune(value)) > 6000 {
		value = string([]rune(value)[:6000]) + " … [gekürzt]"
	}
	return value
}

type Recorder interface {
	Append(Problem) error
	Path() string
}
type Reporter struct {
	sink   Recorder
	mu     sync.Mutex
	recent map[string]*Problem
}

func NewReporter(sink Recorder) *Reporter {
	return &Reporter{sink: sink, recent: map[string]*Problem{}}
}

func (r *Reporter) Report(operation string, err error, private ...string) *Problem {
	if err == nil {
		return nil
	}
	var existing *Problem
	if errors.As(err, &existing) {
		return existing
	}
	p := &Problem{Level: "error", Code: "OPERATION_FAILED", Operation: operation, Message: operation + " fehlgeschlagen", Details: err.Error(), Hint: hintFor(operation), Time: time.Now().UTC().Format(time.RFC3339Nano)}
	var fault *Fault
	if errors.As(err, &fault) {
		p.Code = fault.Code
		p.Message = fault.Message
		p.Details = fault.Details
		p.Hint = fault.Hint
		p.Step = fault.Step
		p.SMTPCode = fault.SMTPCode
	}
	var sqlErr interface{ Code() int }
	if errors.As(err, &sqlErr) {
		p.Code = "DATABASE_ERROR"
		p.Message = "Lokale Datenbank konnte nicht verarbeitet werden"
		p.Hint = "Nicht erneut senden, bis die Datenbank wieder verfügbar ist. Die Versandhistorie nicht löschen."
		switch sqlErr.Code() & 255 {
		case 13:
			p.Code = "DATABASE_FULL"
			p.Hint = "Speicherplatz auf dem Laufwerk freigeben und die App erneut öffnen. Versandhistorie behalten."
		case 8:
			p.Code = "DATABASE_READ_ONLY"
			p.Hint = "Schreibrechte für den Outreach-Datenordner prüfen. Die Datenbank darf nicht schreibgeschützt sein."
		case 5, 6:
			p.Code = "DATABASE_BUSY"
			p.Hint = "Weitere Outreach-Instanzen oder Programme schließen, die dieselbe Datenbank verwenden."
		case 11, 26:
			p.Code = "DATABASE_CORRUPT"
			p.Hint = "Versand nicht fortsetzen. Datenbank und Protokoll zur Diagnose sichern; die Versandhistorie nicht zurücksetzen."
		}
	}
	switch {
	case errors.Is(err, os.ErrPermission):
		p.Code = "FILE_PERMISSION"
		p.Hint = "Zugriffsrechte für die betroffene Datei bzw. den Ordner prüfen."
	case errors.Is(err, os.ErrNotExist):
		p.Code = "FILE_NOT_FOUND"
		p.Hint = "Die Datei wurde verschoben oder gelöscht. Eine vorhandene Datei erneut auswählen."
	}
	p.Operation = Redact(p.Operation, private...)
	p.Message = Redact(p.Message, private...)
	p.Details = Redact(p.Details, private...)
	p.Hint = Redact(p.Hint, private...)
	if r != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		key := p.Operation + "/" + p.Code + "/" + p.Details
		if previous := r.recent[key]; previous != nil {
			at, _ := time.Parse(time.RFC3339Nano, previous.Time)
			if time.Since(at) < 5*time.Second {
				return previous
			}
		}
		defer func() {
			if len(r.recent) > 128 {
				r.recent = map[string]*Problem{}
			}
			r.recent[key] = p
		}()
	}
	var id [8]byte
	if _, e := rand.Read(id[:]); e == nil {
		p.ID = hex.EncodeToString(id[:])
	} else {
		p.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if r == nil || r.sink == nil {
		p.LogIssue = "Lokales Fehlerprotokoll ist nicht verfügbar."
		return p
	}
	p.LogPath = r.sink.Path()
	p.Logged = true
	if e := r.sink.Append(*p); e != nil {
		p.Logged = false
		p.LogIssue = "Fehlerprotokoll konnte nicht geschrieben werden: " + Redact(e.Error(), private...)
		fmt.Fprintln(os.Stderr, p.Code, p.ID, p.LogIssue)
	}
	return p
}

func hintFor(operation string) string {
	switch {
	case strings.Contains(operation, "CSV") || strings.Contains(operation, "Kontakte"):
		return "CSV-Kopfzeile, genannte Zeile und Filter prüfen. Datei als UTF-8 mit Komma-Trennzeichen speichern."
	case strings.Contains(operation, "Vorschau") || strings.Contains(operation, "Nachricht"):
		return "Genannten Platzhalter und CSV-Zeile prüfen. Für leere Felder einen Ersatzwert verwenden, etwa {{First Name|zusammen}}."
	case strings.Contains(operation, "Testmail"):
		return "SMTP-Verbindung, Testadresse und Template prüfen. Bei unklarem Versandstatus keinen erneuten Versuch auslösen."
	case strings.Contains(operation, "Versand"):
		return "Die Nachricht wurde nicht automatisch erneut gesendet. Fehlerursache zuerst beheben und den gespeicherten Status prüfen."
	case strings.Contains(operation, "Schlüsselbund"):
		return "Betriebssystem-Schlüsselbund entsperren oder das Passwort erneut eingeben."
	default:
		return "Angaben prüfen. Bei wiederholtem Auftreten die Fehler-ID und das lokale Protokoll zur Diagnose verwenden."
	}
}
