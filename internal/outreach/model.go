package outreach

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/mail"
	"outreach/internal/diagnostics"
	"strings"
	"unicode"
)

type Config struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	FromName string `json:"fromName"`
	From     string `json:"from"`
	ReplyTo  string `json:"replyTo"`
	Remember bool   `json:"remember"`
}

func (c Config) Public() Config { c.Password = ""; return c }
func (c Config) Sender() string {
	if c.From != "" {
		return c.From
	}
	return c.Username
}
func (c Config) Validate() error {
	if c.Host == "" || strings.ContainsAny(c.Host, " /\\\r\n\t:") {
		return fmt.Errorf("SMTP-Server als Hostname ohne Port angeben")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("Ungültiger SMTP-Port")
	}
	if c.Username == "" || c.Password == "" {
		return fmt.Errorf("SMTP-Benutzer und Passwort fehlen")
	}
	if _, err := Address(c.Sender()); err != nil {
		return fmt.Errorf("Absender: %w", err)
	}
	if c.ReplyTo != "" {
		if _, err := Address(c.ReplyTo); err != nil {
			return fmt.Errorf("Antwort an: %w", err)
		}
	}
	if invalidText(c.FromName, false) || invalidText(c.Username, false) {
		return fmt.Errorf("Ungültige Steuerzeichen im Absender")
	}
	return nil
}
func Address(s string) (string, error) {
	if s == "" || s != strings.TrimSpace(s) || invalidText(s, false) || strings.ContainsAny(s, "<>,; ") {
		return "", fmt.Errorf("Genau eine gültige E-Mail-Adresse erforderlich")
	}
	for _, r := range s {
		if r > 127 {
			return "", fmt.Errorf("Internationale E-Mail-Adressen benötigen SMTPUTF8 und werden noch nicht unterstützt")
		}
	}
	a, err := mail.ParseAddress(s)
	at := strings.LastIndexByte(s, '@')
	if err != nil || a.Address != s || at < 1 || !strings.Contains(s[at+1:], ".") {
		return "", fmt.Errorf("Ungültige E-Mail-Adresse")
	}
	if len(s) > 254 || at > 64 {
		return "", fmt.Errorf("E-Mail-Adresse ist zu lang")
	}
	for _, label := range strings.Split(s[at+1:], ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("Ungültige E-Mail-Domain")
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return "", fmt.Errorf("Ungültige E-Mail-Domain")
			}
		}
	}
	return strings.ToLower(s), nil
}
func invalidText(s string, multiline bool) bool {
	for _, r := range s {
		if unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\r' || r == '\t')) {
			return true
		}
	}
	return false
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Contact struct {
	Row      int               `json:"row"`
	Email    string            `json:"email"`
	Identity string            `json:"identity"`
	Fields   map[string]string `json:"fields"`
}
type Issue struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}
type Report struct {
	Example             bool      `json:"example,omitempty"`
	Filename            string    `json:"filename"`
	Headers             []string  `json:"headers"`
	Contacts            []Contact `json:"contacts"`
	Issues              []Issue   `json:"issues"`
	Total               int       `json:"total"`
	HasStatus           bool      `json:"hasStatus"`
	OnlyVerified        bool      `json:"onlyVerified"`
	PreviouslyContacted int       `json:"previouslyContacted"`
}
type Schedule struct {
	Seconds      int `json:"seconds"`
	BatchSize    int `json:"batchSize"`
	BatchMinutes int `json:"batchMinutes"`
}

func (s Schedule) Validate() error {
	if s.Seconds < 1 || s.Seconds > 86400 || s.BatchSize < 1 || s.BatchSize > 100 || s.BatchMinutes < 0 || s.BatchMinutes > 1440 {
		return fmt.Errorf("Versandpausen oder Batchgröße liegen außerhalb der erlaubten Grenzen")
	}
	return nil
}

type Request struct {
	Config   Config   `json:"config"`
	Subject  string   `json:"subject"`
	Body     string   `json:"body"`
	Schedule Schedule `json:"schedule"`
}
type Message struct {
	Key       string `json:"key"`
	To        string `json:"to"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	MessageID string `json:"messageId"`
	Row       int    `json:"row"`
}
type Plan struct {
	JobID    string    `json:"jobId,omitempty"`
	Example  bool      `json:"example,omitempty"`
	ID       string    `json:"id"`
	Request  Request   `json:"request"`
	Report   Report    `json:"report"`
	Messages []Message `json:"messages"`
}
type Delivery struct {
	Key     string `json:"key"`
	To      string `json:"to"`
	Status  string `json:"status"`
	Detail  string `json:"detail"`
	Updated string `json:"updated"`
}
type State struct {
	Problem    *diagnostics.Problem `json:"problem,omitempty"`
	Plan       *Plan                `json:"plan"`
	Deliveries []Delivery           `json:"deliveries"`
	Running    bool                 `json:"running"`
	NextAt     string               `json:"nextAt"`
	Error      string               `json:"error"`
}
