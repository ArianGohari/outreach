package diagnostics

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestReporterPreservesDetailsAndRedactsSecrets(t *testing.T) {
	log, err := OpenLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reporter := NewReporter(log)
	password := "unique-secret-123"
	body := "CONFIDENTIAL BODY"
	subject := "PRIVATE SUBJECT"
	raw := fmt.Sprintf("535 5.7.8 rejected alice@example.com password=%s %s %s AUTH PLAIN %s", password, body, subject, base64.StdEncoding.EncodeToString([]byte("\x00alice@example.com\x00"+password)))
	f := New("SMTP_AUTH", "SMTP authentication failed", raw, "Enter the password again.", errors.New(raw))
	f.Step = "AUTH"
	f.SMTPCode = 535
	p := reporter.Report("Check SMTP", f, password, body, subject)
	if !p.Logged || p.ID == "" || p.SMTPCode != 535 || !strings.Contains(p.Details, "5.7.8") || p.Hint != "Enter the password again." {
		t.Fatal(p)
	}
	b, err := os.ReadFile(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{password, body, subject, "alice@example.com", base64.StdEncoding.EncodeToString([]byte(password))} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	var saved Problem
	if err = json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ID != p.ID || saved.Level != "error" || saved.Time == "" || saved.Step != "AUTH" {
		t.Fatal(saved)
	}
	if reporter.Report("Check SMTP", f, password, body, subject).ID != p.ID {
		t.Fatal("polling duplicate logged")
	}
	if reporter.Report("bridge", fmt.Errorf("wrapped: %w", p)) != p {
		t.Fatal("already reported error replaced")
	}
	after, _ := os.ReadFile(log.Path())
	if string(after) != string(b) {
		t.Fatal("duplicate log record")
	}
	reporter.Report("Cancel", Cancelled("cancel"))
	after, _ = os.ReadFile(log.Path())
	if string(after) != string(b) {
		t.Fatal("cancellation logged")
	}
}

type brokenSink struct{}

func (brokenSink) Append(Problem) error { return errors.New("disk full") }
func (brokenSink) Path() string         { return "/log" }
func TestLoggingFailureIsVisible(t *testing.T) {
	for _, reporter := range []*Reporter{NewReporter(brokenSink{}), NewReporter(nil), nil} {
		p := reporter.Report("test", errors.New("original failure"))
		if p.Logged || p.LogIssue == "" || p.Details != "original failure" || p.ID == "" {
			t.Fatal(p)
		}
	}
}

type databaseFault int

func (e databaseFault) Error() string { return "synthetic database failure" }
func (e databaseFault) Code() int     { return int(e) }

func TestStorageFailuresHaveActionableDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{databaseFault(13), "DATABASE_FULL"}, {databaseFault(8), "DATABASE_READ_ONLY"},
		{databaseFault(5), "DATABASE_BUSY"}, {databaseFault(6), "DATABASE_BUSY"},
		{databaseFault(11), "DATABASE_CORRUPT"}, {databaseFault(26), "DATABASE_CORRUPT"},
		{databaseFault(1), "DATABASE_ERROR"}, {os.ErrPermission, "FILE_PERMISSION"}, {os.ErrNotExist, "FILE_NOT_FOUND"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			p := NewReporter(nil).Report("Save", fmt.Errorf("wrapped: %w", tc.err))
			if p.Code != tc.code || p.Hint == "" || p.Details == "" {
				t.Fatal(p)
			}
		})
	}
	for operation, hint := range map[string]string{"Import CSV": "UTF-8", "Filter contacts": "UTF-8", "Check message preview": "placeholder", "Send test email": "test address", "Start sending": "saved status", "Save delivery status": "saved status", "Read keyring": "Unlock"} {
		if p := NewReporter(nil).Report(operation, errors.New("failed")); !strings.Contains(p.Hint, hint) {
			t.Fatal(p)
		}
	}
}
func TestConcurrentLogWritersAndRotation(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l := first
			if i%2 == 0 {
				l = second
			}
			if err := l.Append(Problem{ID: fmt.Sprint(i), Details: "test"}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	data, err := os.ReadFile(first.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 30 {
		t.Fatal(len(lines))
	}
	seen := map[string]bool{}
	for _, line := range lines {
		var p Problem
		if err = json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatal(err)
		}
		if seen[p.ID] {
			t.Fatal("duplicate")
		}
		seen[p.ID] = true
	}
	first.maxBytes = 200
	for i := 0; i < 10; i++ {
		if err = first.Append(Problem{ID: fmt.Sprint(i), Details: strings.Repeat("x", 100)}); err != nil {
			t.Fatal(err)
		}
	}
	matches, _ := filepath.Glob(first.Path() + "*")
	if len(matches) != 4 {
		t.Fatalf("rotation files %v", matches)
	}
	for _, path := range matches {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if !json.Valid([]byte(line)) {
				t.Fatalf("invalid record %s", path)
			}
		}
	}
}
