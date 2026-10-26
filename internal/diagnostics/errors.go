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
		msg += "\nError ID: " + p.ID
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
		value = strings.ReplaceAll(value, v, "[redacted]")
	}
	value = authPattern.ReplaceAllString(value, "AUTH [redacted]")
	value = credentialPattern.ReplaceAllString(value, "$1=[redacted]")
	value = emailPattern.ReplaceAllString(value, "[email address]")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	if len([]rune(value)) > 6000 {
		value = string([]rune(value)[:6000]) + " … [truncated]"
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
	p := &Problem{Level: "error", Code: "OPERATION_FAILED", Operation: operation, Message: operation + " failed", Details: err.Error(), Hint: hintFor(operation), Time: time.Now().UTC().Format(time.RFC3339Nano)}
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
		p.Message = "Could not access local database"
		p.Hint = "Do not send again until the database is available. Do not delete sending history."
		switch sqlErr.Code() & 255 {
		case 13:
			p.Code = "DATABASE_FULL"
			p.Hint = "Free disk space and reopen the app. Keep sending history."
		case 8:
			p.Code = "DATABASE_READ_ONLY"
			p.Hint = "Check write permissions for the Outreach data directory. The database must be writable."
		case 5, 6:
			p.Code = "DATABASE_BUSY"
			p.Hint = "Close other Outreach instances or programs using the same database."
		case 11, 26:
			p.Code = "DATABASE_CORRUPT"
			p.Hint = "Do not resume sending. Back up the database and log for diagnosis; do not reset sending history."
		}
	}
	switch {
	case errors.Is(err, os.ErrPermission):
		p.Code = "FILE_PERMISSION"
		p.Hint = "Check access permissions for the affected file or folder."
	case errors.Is(err, os.ErrNotExist):
		p.Code = "FILE_NOT_FOUND"
		p.Hint = "The file was moved or deleted. Select an existing file."
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
		p.LogIssue = "Local error log is unavailable."
		return p
	}
	p.LogPath = r.sink.Path()
	p.Logged = true
	if e := r.sink.Append(*p); e != nil {
		p.Logged = false
		p.LogIssue = "Could not write error log: " + Redact(e.Error(), private...)
		fmt.Fprintln(os.Stderr, p.Code, p.ID, p.LogIssue)
	}
	return p
}

func hintFor(operation string) string {
	operation = strings.ToLower(operation)
	switch {
	case strings.Contains(operation, "csv") || strings.Contains(operation, "contacts"):
		return "Check the CSV header, indicated row and filters. Save as UTF-8 with comma separators."
	case strings.Contains(operation, "preview") || strings.Contains(operation, "message"):
		return "Check the indicated placeholder and CSV row. Use a fallback for empty fields, such as {{First Name|there}}."
	case strings.Contains(operation, "test email"):
		return "Check the connection, test address and template. Do not retry if delivery status is uncertain."
	case strings.Contains(operation, "sending") || strings.Contains(operation, "delivery") || strings.Contains(operation, "recipient"):
		return "The message was not retried automatically. Fix the cause and check the saved status first."
	case strings.Contains(operation, "keyring"):
		return "Unlock the system keyring or enter the password again."
	default:
		return "Check your settings. If the problem persists, use the error ID and local log for diagnosis."
	}
}
