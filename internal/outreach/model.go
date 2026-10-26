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
	Provider    string `json:"provider,omitempty"`
	GoogleGrant string `json:"googleGrant,omitempty"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	FromName    string `json:"fromName"`
	From        string `json:"from"`
	ReplyTo     string `json:"replyTo"`
	Remember    bool   `json:"remember"`
}

func (c Config) Public() Config { c.Password = ""; return c }
func (c Config) Sender() string {
	if c.From != "" {
		return c.From
	}
	return c.Username
}
func (c Config) Validate() error {
	if c.Provider != "" && c.Provider != "google" {
		return fmt.Errorf("Unknown sending provider")
	}
	if c.Provider == "google" {
		if c.GoogleGrant == "" {
			return fmt.Errorf("Connect a Google account first")
		}
		if _, err := Address(c.Username); err != nil {
			return fmt.Errorf("Google account: %w", err)
		}
		if !strings.EqualFold(c.Sender(), c.Username) {
			return fmt.Errorf("The Google sender must match the connected account; aliases are not supported")
		}
	} else {
		if c.Host == "" || strings.ContainsAny(c.Host, " /\\\r\n\t:") {
			return fmt.Errorf("Enter the SMTP server as a hostname without a port")
		}
		if c.Port < 1 || c.Port > 65535 {
			return fmt.Errorf("Invalid SMTP port")
		}
		if c.Username == "" || c.Password == "" {
			return fmt.Errorf("SMTP username and password are required")
		}
	}
	if _, err := Address(c.Sender()); err != nil {
		return fmt.Errorf("Sender: %w", err)
	}
	if c.ReplyTo != "" {
		if _, err := Address(c.ReplyTo); err != nil {
			return fmt.Errorf("Reply-to: %w", err)
		}
	}
	if invalidText(c.FromName, false) || invalidText(c.Username, false) {
		return fmt.Errorf("Invalid control characters in sender")
	}
	return nil
}
func Address(s string) (string, error) {
	if s == "" || s != strings.TrimSpace(s) || invalidText(s, false) || strings.ContainsAny(s, "<>,; ") {
		return "", fmt.Errorf("Exactly one valid email address is required")
	}
	for _, r := range s {
		if r > 127 {
			return "", fmt.Errorf("International email addresses require SMTPUTF8 and are not yet supported")
		}
	}
	a, err := mail.ParseAddress(s)
	at := strings.LastIndexByte(s, '@')
	if err != nil || a.Address != s || at < 1 || !strings.Contains(s[at+1:], ".") {
		return "", fmt.Errorf("Invalid email address")
	}
	if len(s) > 254 || at > 64 {
		return "", fmt.Errorf("Email address is too long")
	}
	for _, label := range strings.Split(s[at+1:], ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("Invalid email domain")
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return "", fmt.Errorf("Invalid email domain")
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
		return fmt.Errorf("Sending pauses or batch size are outside the allowed range")
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
