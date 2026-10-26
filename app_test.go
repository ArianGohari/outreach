package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopBindingsWithoutNetwork(t *testing.T) {
	store, e := outreach.OpenStore(filepath.Join(t.TempDir(), "app.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	app, e := NewApp(store)
	if e != nil {
		t.Fatal(e)
	}
	view, e := app.CreateJob("Testjob")
	if e != nil {
		t.Fatal(e)
	}
	job, _ := app.jobs.Get(view.ID)
	state, e := app.GetState(view.ID)
	if e != nil || state.Running || state.Plan != nil {
		t.Fatal("fresh app state invalid", e)
	}
	if e = app.CheckConnection(view.ID, outreach.Config{}); e == nil {
		t.Fatal("empty SMTP settings accepted")
	}
	b, e := os.ReadFile("internal/outreach/testdata/apollo.csv")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = job.Session.Import(b, "synthetic.csv", false); e != nil {
		t.Fatal(e)
	}
	r := outreach.Request{Config: outreach.Config{Host: "smtp.example.com", Port: 587, Username: "sender@example.org", Password: "test-only"}, Subject: "Hallo {{First Name}}", Body: "Plain text", Schedule: outreach.Schedule{Seconds: 30, BatchSize: 100, BatchMinutes: 60}}
	p, e := app.Preview(view.ID, r)
	if e != nil || len(p.Messages) != 3 {
		t.Fatal("native preview binding", e)
	}
	filtered, e := app.FilterContacts(view.ID, true)
	if e != nil || len(filtered.Contacts) != 0 {
		t.Fatal("native filter binding", e)
	}
	if password, e := app.LoadPassword(outreach.Config{}); e != nil || password != "" {
		t.Fatal("unexpected keychain access")
	}
	if e = job.Session.Start(r, "fake-token"); e == nil {
		t.Fatal("unapproved native start allowed")
	}
	app.Stop(view.ID)
	if app.beforeClose(context.Background()) {
		t.Fatal("idle app cannot close")
	}
	app.shutdown(context.Background())
}
func TestCredentialAccountSeparation(t *testing.T) {
	a := outreach.Config{Host: "smtp.example.org", Port: 587, Username: "one"}
	b := a
	b.Username = "two"
	c := a
	c.Port = 465
	if credentialID(a) == credentialID(b) || credentialID(a) == credentialID(c) {
		t.Fatal("keychain accounts collide")
	}
}

func TestAppDiagnosticBoundaryAndStartupFailure(t *testing.T) {
	log, err := diagnostics.OpenLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reporter := diagnostics.NewReporter(log)
	store, err := outreach.OpenStore(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app, err := NewApp(store, reporter)
	if err != nil {
		t.Fatal(err)
	}
	view, err := app.CreateJob("Diagnose")
	if err != nil {
		t.Fatal(err)
	}
	err = app.CheckConnection(view.ID, outreach.Config{Password: "do-not-log-this"})
	var problem *diagnostics.Problem
	if !errors.As(err, &problem) || !problem.Logged || !strings.HasPrefix(problem.Operation, "SMTP-Verbindung prüfen") {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log.Path())
	if err != nil || !strings.Contains(string(b), problem.ID) || strings.Contains(string(b), "do-not-log-this") {
		t.Fatal(err, string(b))
	}
	failed := &App{reporter: reporter, startupProblem: problem}
	if _, err = failed.GetState("missing"); err != problem {
		t.Fatal("startup cause lost", err)
	}
	if err = failed.Stop("missing"); err != problem {
		t.Fatal("startup stop unsafe", err)
	}
	local := app.ReportClientError("UI_BATCH_RANGE")
	if _, isError := any(local).(error); isError {
		t.Fatal("Wails must return the log receipt as data, not reject it as an error")
	}
	if !local.Logged || !strings.Contains(local.Details, "24 Stunden") {
		t.Fatal(local)
	}
}

func TestExamplePreviewAndDesktopErrorTransport(t *testing.T) {
	log, err := diagnostics.OpenLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := outreach.OpenStore(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app, err := NewApp(store, diagnostics.NewReporter(log))
	if err != nil {
		t.Fatal(err)
	}
	view, err := app.CreateJob("Neuer Job")
	if err != nil {
		t.Fatal(err)
	}
	r := view.Draft.Request
	if r.Schedule.BatchSize != 50 || r.Schedule.BatchMinutes != 1440 {
		t.Fatal("wrong defaults", r.Schedule)
	}
	r.Subject = "Hallo {{First Name}} {{Last Name}}"
	r.Body = "Hallo {{First Name}}!"
	p, err := app.Preview(view.ID, r)
	if err != nil || !p.Example || len(p.Messages) != 1 || p.Messages[0].Subject != "Hallo Jane Doe" {
		t.Fatal("example without CSV or SMTP", p, err)
	}
	r.Subject = ""
	_, err = app.Preview(view.ID, r)
	if err == nil {
		t.Fatal("blank subject accepted")
	}
	wire, ok := app.formatError(err).(string)
	if !ok {
		t.Fatal("Wails Error() requires a string")
	}
	var received diagnostics.Problem
	if err = json.Unmarshal([]byte(wire), &received); err != nil {
		t.Fatal(err)
	}
	if !received.Logged || received.ID == "" || !strings.Contains(received.Details, "Betreff leer") {
		t.Fatal("diagnostics lost", wire)
	}
	data, err := os.ReadFile(log.Path())
	if err != nil || !strings.Contains(string(data), received.ID) {
		t.Fatal("missing matching log", err)
	}
}
