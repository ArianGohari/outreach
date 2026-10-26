package outreach

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Runs only as a subprocess. The parent kills it while SMTP is waiting for its DATA acknowledgement.
func TestCrashProcessHelper(t *testing.T) {
	dir := os.Getenv("OUTREACH_CRASH_FIXTURE")
	if dir == "" {
		return
	}
	raw, e := os.ReadFile(filepath.Join(dir, "config.json"))
	if e != nil {
		t.Fatal(e)
	}
	var c Config
	if e = json.Unmarshal(raw, &c); e != nil {
		t.Fatal(e)
	}
	der, e := os.ReadFile(filepath.Join(dir, "cert.der"))
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	store, e := OpenStore(filepath.Join(dir, "crash.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	p := samplePlan(t)
	engine := NewEngine(store, SMTPSender{tlsConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, RealClock{}, nil)
	if e = engine.Start(p, c); e != nil {
		t.Fatal(e)
	}
	// A bounded wait, not a graceful shutdown: only the parent's Kill may interrupt the in-flight send.
	time.Sleep(20 * time.Second)
	t.Fatal("parent failed to terminate child")
}

func TestKilledProcessAfterSMTPDataNeverResends(t *testing.T) {
	server := startSMTP(t, "hold-ack")
	dir := t.TempDir()
	raw, _ := json.Marshal(server.config())
	if e := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "cert.der"), server.cert.Certificate[0], 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashProcessHelper$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "OUTREACH_CRASH_FIXTURE="+dir)
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	received := false
	for time.Now().Before(deadline) {
		server.mu.Lock()
		received = len(server.messages) == 1
		server.mu.Unlock()
		if received {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !received {
		t.Fatal("child never reached SMTP DATA")
	}
	if e := cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	if e := cmd.Wait(); e == nil {
		t.Fatal("child exited normally")
	}
	close(server.ackRelease)
	store, e := OpenStore(filepath.Join(dir, "crash.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	state, e := store.State()
	if e != nil || state.Deliveries[0].Status != "uncertain" {
		t.Fatal("in-flight reservation not quarantined", state, e)
	}
	engine := NewEngine(store, server.sender(), newClock(), nil)
	if e = engine.Start(*state.Plan, server.config()); e != nil {
		t.Fatal(e)
	}
	waitEngine(t, engine)
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.messages) != 3 {
		t.Fatal("wrong number after crash", len(server.messages))
	}
	seen := map[string]bool{}
	for _, to := range server.recipients {
		if seen[to] {
			t.Fatal("duplicate after hard crash", to)
		}
		seen[to] = true
	}
}
