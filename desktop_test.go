package main

import (
	"context"
	"errors"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zalando/go-keyring"
	"os"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type desktopSender struct {
	mu       sync.Mutex
	sent     []outreach.Message
	checkErr error
	entered  chan struct{}
	release  chan struct{}
}

func (s *desktopSender) Check(context.Context, outreach.Config) error { return s.checkErr }
func (s *desktopSender) Send(_ context.Context, _ outreach.Config, m outreach.Message) outreach.SendResult {
	if s.entered != nil {
		s.entered <- struct{}{}
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, m)
	return outreach.SendResult{Status: "sent"}
}

type desktopClock struct{}

func (desktopClock) Now() time.Time                                  { return time.Now() }
func (desktopClock) Wait(ctx context.Context, _ time.Duration) error { return ctx.Err() }
func desktopFixture(t *testing.T) (*App, *outreach.Store, *desktopSender) {
	t.Helper()
	store, err := outreach.OpenStore(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	log, err := diagnostics.OpenLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sender := &desktopSender{}
	a, err := newApp(store, sender, desktopClock{}, diagnostics.NewReporter(log))
	if err != nil {
		t.Fatal(err)
	}
	a.services = &desktopServices{openFile: func(context.Context, runtime.OpenDialogOptions) (string, error) {
		return "internal/outreach/testdata/apollo.csv", nil
	}, message: func(_ context.Context, o runtime.MessageDialogOptions) (string, error) {
		return o.Buttons[len(o.Buttons)-1], nil
	}, setPassword: func(string, string, string) error { return nil }, getPassword: func(string, string) (string, error) { return "test-password", nil }, deletePassword: func(string, string) error { return keyring.ErrNotFound }, emit: func(context.Context, string, ...interface{}) {}}
	a.startup(context.Background())
	t.Cleanup(func() { a.shutdown(context.Background()); store.Close() })
	return a, store, sender
}
func desktopRequest(v outreach.JobView) outreach.Request {
	r := v.Draft.Request
	r.Config = outreach.Config{Host: "smtp.example.org", Port: 587, Username: "sender@example.org", Password: "private-password"}
	r.Subject = "Hallo {{First Name}}"
	r.Body = "Hallo {{First Name}} bei {{Company|deinem Unternehmen}}"
	r.Schedule = outreach.Schedule{Seconds: 1, BatchSize: 2, BatchMinutes: 0}
	return r
}
func waitDesktop(t *testing.T, a *App, id string) outreach.State {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, e := a.GetState(id)
		if e != nil {
			t.Fatal(e)
		}
		if !s.Running {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("worker did not finish")
	return outreach.State{}
}
func TestDesktopCompleteWorkflowAndPersistence(t *testing.T) {
	a, store, sender := desktopFixture(t)
	v, err := a.CreateJob("Integration")
	if err != nil {
		t.Fatal(err)
	}
	r := desktopRequest(v)
	if err = a.SaveDraft(v.ID, "Saved", r); err != nil {
		t.Fatal(err)
	}
	report, err := a.ChooseCSV(v.ID, false)
	if err != nil || len(report.Contacts) != 3 {
		t.Fatal(report, err)
	}
	views, err := a.GetWorkspace()
	if err != nil || views[0].Draft.Request.Config.Password != "" || len(views[0].Draft.Report.Contacts) != 3 {
		t.Fatal(views, err)
	}
	p, err := a.Preview(v.ID, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Start(v.ID, r, "invalid"); err == nil {
		t.Fatal("start without test proof")
	}
	if err = a.CheckConnection(v.ID, r.Config); err != nil {
		t.Fatal(err)
	}
	token, err := a.SendTest(v.ID, r, 0, "test@example.org")
	if err != nil {
		t.Fatal(err)
	}
	a.services.message = func(_ context.Context, o runtime.MessageDialogOptions) (string, error) {
		if o.DefaultButton != "Abbrechen" || o.CancelButton != "Abbrechen" {
			t.Error(o)
		}
		return "Abbrechen", nil
	}
	if err = a.Start(v.ID, r, token); err == nil {
		t.Fatal("cancelled start accepted")
	}
	sender.mu.Lock()
	n := len(sender.sent)
	sender.mu.Unlock()
	if n != 1 {
		t.Fatal("cancel sent outreach")
	}
	a.services.message = func(context.Context, runtime.MessageDialogOptions) (string, error) { return "Versand starten", nil }
	if err = a.Start(v.ID, r, token); err != nil {
		t.Fatal(err)
	}
	s := waitDesktop(t, a, v.ID)
	if len(s.Deliveries) != 3 {
		t.Fatal(s)
	}
	if err = a.Start(v.ID, r, token); err != nil {
		t.Fatal(err)
	}
	waitDesktop(t, a, v.ID)
	sender.mu.Lock()
	messages := append([]outreach.Message(nil), sender.sent...)
	sender.mu.Unlock()
	if len(messages) != 4 {
		t.Fatal("duplicate send", len(messages))
	}
	for i, m := range p.Messages {
		if messages[i+1].To != m.To || messages[i+1].Body != m.Body || messages[i+1].Subject != m.Subject {
			t.Fatal("preview changed")
		}
	}
	restored, err := newApp(store, sender, desktopClock{}, a.reporter)
	if err != nil {
		t.Fatal(err)
	}
	restored.services = a.services
	if err = restored.Start(v.ID, r, token); err == nil {
		t.Fatal("old approval survived")
	}
}

func TestDesktopImportAndDialogFailures(t *testing.T) {
	a, _, _ := desktopFixture(t)
	v, _ := a.CreateJob("Import")
	for _, tc := range []struct {
		name, path string
		err        error
	}{
		{"cancel", "", nil}, {"dialog", "", errors.New("dialog unavailable")}, {"missing", filepath.Join(t.TempDir(), "missing.csv"), nil}, {"directory", t.TempDir(), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.services.openFile = func(context.Context, runtime.OpenDialogOptions) (string, error) { return tc.path, tc.err }
			_, err := a.ChooseCSV(v.ID, false)
			var p *diagnostics.Problem
			if !errors.As(err, &p) {
				t.Fatal(err)
			}
			if tc.name == "cancel" {
				if !p.Cancelled {
					t.Fatal(p)
				}
			} else if !p.Logged {
				t.Fatal(p)
			}
		})
	}
	bad := filepath.Join(t.TempDir(), "bad.csv")
	os.WriteFile(bad, []byte("Name,Address\nWrong,no\n"), 0600)
	a.services.openFile = func(context.Context, runtime.OpenDialogOptions) (string, error) { return bad, nil }
	if _, err := a.ChooseCSV(v.ID, false); err == nil {
		t.Fatal("bad CSV accepted")
	}
	if _, err := a.FilterContacts(v.ID, false); err == nil {
		t.Fatal("filter without CSV")
	}
	r := desktopRequest(v)
	if err := a.SaveDraft(v.ID, "Import", r); err != nil {
		t.Fatal(err)
	}
	a.services.openFile = func(context.Context, runtime.OpenDialogOptions) (string, error) {
		return "internal/outreach/testdata/apollo.csv", nil
	}
	if _, err := a.ChooseCSV(v.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckConnection(v.ID, r.Config); err != nil {
		t.Fatal(err)
	}
	token, err := a.SendTest(v.ID, r, 0, "test@example.org")
	if err != nil {
		t.Fatal(err)
	}
	dialogCalled := false
	a.services.message = func(context.Context, runtime.MessageDialogOptions) (string, error) {
		dialogCalled = true
		return "", errors.New("dialog failed")
	}
	if err := a.Start(v.ID, r, token); err == nil || !dialogCalled || !strings.Contains(err.Error(), "dialog failed") {
		t.Fatal("dialog error ignored")
	}
	if err := a.DeleteJob(v.ID); err != nil {
		t.Fatal(err)
	}
	blank, _ := a.CreateJob("Neuer Job")
	if err := a.DiscardEmptyJob(blank.ID); err != nil {
		t.Fatal(err)
	}
	views, err := a.GetWorkspace()
	if err != nil || len(views) != 0 {
		t.Fatal(views, err)
	}
}

func TestDesktopCredentialSuccessAndFailures(t *testing.T) {
	a, _, sender := desktopFixture(t)
	v, _ := a.CreateJob("Credentials")
	c := desktopRequest(v).Config
	c.Remember = true
	saved := map[string]string{}
	a.services.setPassword = func(service, account, password string) error { saved[service+account] = password; return nil }
	a.services.getPassword = func(service, account string) (string, error) { return saved[service+account], nil }
	a.services.deletePassword = func(service, account string) error { delete(saved, service+account); return nil }
	if err := a.CheckConnection(v.ID, c); err != nil {
		t.Fatal(err)
	}
	if password, err := a.LoadPassword(c); err != nil || password != c.Password {
		t.Fatal("keychain read", err)
	}
	c.Remember = false
	if err := a.CheckConnection(v.ID, c); err != nil || len(saved) != 0 {
		t.Fatal("keychain delete", err)
	}
	sender.checkErr = errors.New("connection failed")
	if err := a.CheckConnection(v.ID, c); err == nil {
		t.Fatal("connection error ignored")
	}
	sender.checkErr = nil
	for _, operation := range []string{"save", "read", "delete"} {
		t.Run(operation, func(t *testing.T) {
			fault := errors.New("keychain locked")
			a.services.setPassword = func(string, string, string) error { return fault }
			a.services.getPassword = func(string, string) (string, error) { return "", fault }
			a.services.deletePassword = func(string, string) error { return fault }
			c.Remember = operation != "delete"
			var err error
			if operation == "read" {
				_, err = a.LoadPassword(c)
			} else {
				err = a.CheckConnection(v.ID, c)
			}
			var p *diagnostics.Problem
			if !errors.As(err, &p) || !strings.HasPrefix(p.Code, "KEYCHAIN_") || !p.Logged {
				t.Fatal(err)
			}
		})
	}
}

func TestDesktopUnavailableAndMissingJobBoundaries(t *testing.T) {
	a, _, _ := desktopFixture(t)
	for _, broken := range []*App{{}, {startupProblem: diagnostics.NewReporter(nil).Report("startup", errors.New("disk unavailable"))}, a} {
		r := outreach.Request{}
		calls := []func() error{func() error { _, e := broken.GetState("missing"); return e }, func() error { return broken.SaveDraft("missing", "X", r) }, func() error { _, e := broken.ChooseCSV("missing", false); return e }, func() error { _, e := broken.FilterContacts("missing", false); return e }, func() error { _, e := broken.Preview("missing", r); return e }, func() error { return broken.CheckConnection("missing", r.Config) }, func() error { _, e := broken.SendTest("missing", r, 0, ""); return e }, func() error { return broken.Start("missing", r, "") }, func() error { return broken.Stop("missing") }, func() error { return broken.DeleteJob("missing") }, func() error { return broken.DiscardEmptyJob("missing") }}
		for i, call := range calls {
			if err := call(); err == nil {
				t.Fatalf("boundary %d accepted", i)
			}
		}
		if broken != a {
			if _, e := broken.GetWorkspace(); e == nil {
				t.Fatal("workspace accepted")
			}
			if _, e := broken.CreateJob("X"); e == nil {
				t.Fatal("create accepted")
			}
			if _, e := broken.LoadPassword(r.Config); e == nil {
				t.Fatal("keychain accepted")
			}
		}
	}
	for _, code := range []string{"UI_BATCH_RANGE", "UI_BATCH_PRECISION", "UI_NO_CONTACTS", "UI_INPUT", "unknown"} {
		if p := a.ReportClientError(code); !p.Logged {
			t.Fatal(p)
		}
	}
}

func TestDesktopBeforeCloseRunningAndErrors(t *testing.T) {
	a, store, sender := desktopFixture(t)
	v, _ := a.CreateJob("Running")
	r := desktopRequest(v)
	a.ChooseCSV(v.ID, false)
	a.CheckConnection(v.ID, r.Config)
	token, e := a.SendTest(v.ID, r, 0, "test@example.org")
	if e != nil {
		t.Fatal(e)
	}
	sender.entered = make(chan struct{}, 3)
	sender.release = make(chan struct{})
	if e = a.Start(v.ID, r, token); e != nil {
		t.Fatal(e)
	}
	<-sender.entered
	a.services.message = func(context.Context, runtime.MessageDialogOptions) (string, error) {
		return "Weiter geöffnet lassen", nil
	}
	if !a.beforeClose(context.Background()) {
		t.Fatal("closed without consent")
	}
	a.services.message = func(context.Context, runtime.MessageDialogOptions) (string, error) {
		return "", errors.New("dialog error")
	}
	if !a.beforeClose(context.Background()) {
		t.Fatal("closed after dialog error")
	}
	a.services.message = func(context.Context, runtime.MessageDialogOptions) (string, error) {
		// Let the confirmed close cancel the worker while its SMTP call is still in flight.
		go func() { time.Sleep(30 * time.Millisecond); close(sender.release) }()
		return "Pausieren und beenden", nil
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("did not close")
	}
	if s := waitDesktop(t, a, v.ID); s.Deliveries[1].Status != "pending" {
		t.Fatal("did not stop", s)
	}
	store.Close()
	if !a.beforeClose(context.Background()) {
		t.Fatal("closed despite unreadable state")
	}
	if (&App{}).beforeClose(context.Background()) {
		t.Fatal("empty app cannot close")
	}
}

func TestFrameworkLoggerNeverLeaksRPCArguments(t *testing.T) {
	log, e := diagnostics.OpenLog(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	l := frameworkLogger{reporter: diagnostics.NewReporter(log), notify: func(p *diagnostics.Problem) {
		count++
		if !p.Logged {
			t.Error(p)
		}
	}}
	secret := "password=SUPER_PRIVATE body=PRIVATE_BODY alice@example.org"
	l.Print(secret)
	l.Trace(secret)
	l.Debug(secret)
	l.Info(secret)
	for _, write := range []func(string){l.Warning, l.Error, l.Fatal} {
		write(secret)
	}
	data, e := os.ReadFile(log.Path())
	if e != nil {
		t.Fatal(e)
	}
	if count != 3 || strings.Contains(string(data), "SUPER_PRIVATE") || strings.Contains(string(data), "PRIVATE_BODY") || strings.Contains(string(data), "alice@example.org") || !strings.Contains(string(data), "Meldungskennung") {
		t.Fatal("unsafe runtime log")
	}
}
