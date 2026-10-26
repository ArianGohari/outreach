package main

import (
	"context"
	"outreach/internal/googlemail"
	"testing"
)

func TestGoogleDesktopCancelledLogin(t *testing.T) {
	a, _, _ := desktopFixture(t)
	values := map[string]string{}
	a.google = googlemail.New(googlemail.Vault{Get: func(k string) (string, error) { return values[k], nil }, Set: func(k, v string) error { values[k] = v; return nil }, Delete: func(k string) error { delete(values, k); return nil }})
	if err := a.google.ImportClient([]byte(`{"installed":{"client_id":"test.apps.googleusercontent.com","client_secret":"synthetic-only"}}`)); err != nil {
		t.Fatal(err)
	}
	job, err := a.CreateJob("Google")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.ctx = ctx
	a.services.openBrowser = func(string) error { return nil }
	if c, err := a.ConnectGoogle(job.ID); err == nil || c.GoogleGrant != "" {
		t.Fatal("cancelled login succeeded")
	}
	if _, err := a.ConnectGoogle("missing"); err == nil {
		t.Fatal("missing job accepted")
	}
}

func TestGoogleAccountCannotDisconnectWhileRunning(t *testing.T) {
	a, _, sender := desktopFixture(t)
	deleted := false
	a.google = googlemail.New(googlemail.Vault{Delete: func(string) error { deleted = true; return nil }})
	v, err := a.CreateJob("Google running")
	if err != nil {
		t.Fatal(err)
	}
	r := desktopRequest(v)
	r.Config.Provider = "google"
	r.Config.GoogleGrant = "synthetic-grant"
	r.Config.Password = ""
	r.Config.Remember = true
	if _, err = a.ChooseCSV(v.ID, false); err != nil {
		t.Fatal(err)
	}
	a.services.setPassword = func(string, string, string) error { t.Error("Google used SMTP keychain"); return nil }
	if err = a.CheckConnection(v.ID, r.Config); err != nil {
		t.Fatal(err)
	}
	if password, err := a.LoadPassword(r.Config); err != nil || password != "" {
		t.Fatal("Google loaded SMTP password")
	}
	token, err := a.SendTest(v.ID, r, 0, "test@example.org")
	if err != nil {
		t.Fatal(err)
	}
	sender.release = make(chan struct{})
	sender.entered = make(chan struct{}, 3)
	if err = a.Start(v.ID, r, token); err != nil {
		t.Fatal(err)
	}
	<-sender.entered
	if _, err = a.ConnectGoogle(v.ID); err == nil {
		t.Error("login during send")
	}
	if err = a.DisconnectGoogle(v.ID, r.Config); err == nil || deleted {
		t.Error("disconnected running account")
	}
	a.Stop(v.ID)
	close(sender.release)
	waitDesktop(t, a, v.ID)
	if err = a.DisconnectGoogle(v.ID, r.Config); err != nil || !deleted {
		t.Fatal("disconnect failed", err)
	}
	if err = a.Start(v.ID, r, token); err == nil {
		t.Fatal("disconnect retained send approval")
	}
}
