//go:build integration

package main

import (
	"context"
	"fmt"
	"github.com/zalando/go-keyring"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"os"
	"outreach/internal/outreach"
	"outreach/internal/testsupport"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesktopBindingsWithRealSMTPAndSQLite(t *testing.T) {
	sink, e := testsupport.NewSMTP()
	if e != nil {
		t.Fatal(e)
	}
	defer sink.Close()
	a, store, _ := desktopFixture(t)
	a.jobs.Close()
	services := a.services
	a, e = newApp(store, outreach.IntegrationSender(sink.Roots, sink.Listener.Addr().String()), desktopClock{}, a.reporter)
	if e != nil {
		t.Fatal(e)
	}
	a.services = services
	t.Cleanup(func() { a.shutdown(context.Background()) })
	v, e := a.CreateJob("Real integration")
	if e != nil {
		t.Fatal(e)
	}
	r := desktopRequest(v)
	r.Config.Host = "127.0.0.1"
	r.Config.Port = sink.Listener.Addr().(*net.TCPAddr).Port
	if e = a.SaveDraft(v.ID, v.Name, r); e != nil {
		t.Fatal(e)
	}
	if _, e = a.ChooseCSV(v.ID, false); e != nil {
		t.Fatal(e)
	}
	if e = a.CheckConnection(v.ID, r.Config); e != nil {
		t.Fatal(e)
	}
	p, e := a.Preview(v.ID, r)
	if e != nil {
		t.Fatal(e)
	}
	token, e := a.SendTest(v.ID, r, 0, "test@example.org")
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Start(v.ID, r, token); e != nil {
		t.Fatal(e)
	}
	waitDesktop(t, a, v.ID)
	if e = a.Start(v.ID, r, token); e != nil {
		t.Fatal(e)
	}
	waitDesktop(t, a, v.ID)
	messages, recipients := sink.Snapshot()
	if len(messages) != 4 {
		t.Fatal("duplicate/missing message", len(messages))
	}
	for i, want := range p.Messages {
		m, e := mail.ReadMessage(strings.NewReader(messages[i+1]))
		if e != nil {
			t.Fatal(e)
		}
		subject, e := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
		if e != nil || subject != want.Subject {
			t.Fatal("subject changed", e)
		}
		body, e := io.ReadAll(quotedprintable.NewReader(m.Body))
		if e != nil || string(body) != want.Body || recipients[i+1] != "<"+want.To+">" {
			t.Fatal("recipient/body changed", e)
		}
	}
	if e = a.DeleteJob(v.ID); e != nil {
		t.Fatal(e)
	}
	if views, e := a.GetWorkspace(); e != nil || len(views) != 0 {
		t.Fatal(views, e)
	}
}

func TestOperatingSystemCredentialRoundTrip(t *testing.T) {
	if os.Getenv("OUTREACH_OS_TESTS") != "1" {
		t.Skip("set OUTREACH_OS_TESTS=1 to test the real OS credential store")
	}
	service := fmt.Sprintf("Outreach.Integration.%d", time.Now().UnixNano())
	account := "synthetic@example.org"
	password := "synthetic-test-password-ÄÖÜ"
	t.Cleanup(func() { keyring.Delete(service, account) })
	if e := keyring.Set(service, account, password); e != nil {
		t.Fatal(e)
	}
	if got, e := keyring.Get(service, account); e != nil || got != password {
		t.Fatal("credential read differs", e)
	}
	if e := keyring.Delete(service, account); e != nil {
		t.Fatal(e)
	}
	if _, e := keyring.Get(service, account); e != keyring.ErrNotFound {
		t.Fatal("credential survived delete", e)
	}
}

func TestDesktopImportPersistsAcrossReopen(t *testing.T) {
	a, _, _ := desktopFixture(t)
	// Reopen through a separate database handle after releasing the process lock.
	path := filepath.Join(t.TempDir(), "restart.db")
	store, e := outreach.OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	b, e := newApp(store, &desktopSender{}, desktopClock{})
	if e != nil {
		t.Fatal(e)
	}
	b.services = a.services
	fresh, e := b.CreateJob("Restart")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.ChooseCSV(fresh.ID, false); e != nil {
		t.Fatal(e)
	}
	b.shutdown(context.Background())
	store.Close()
	store, e = outreach.OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	b, e = newApp(store, &desktopSender{}, desktopClock{})
	if e != nil {
		t.Fatal(e)
	}
	defer b.shutdown(context.Background())
	b.services = a.services
	report, e := b.FilterContacts(fresh.ID, false)
	if e != nil || len(report.Contacts) != 3 {
		t.Fatal("raw CSV not restored", report, e)
	}
}
