package outreach

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSender struct {
	mu       sync.Mutex
	messages []Message
	result   SendResult
	entered  chan struct{}
	release  chan struct{}
}

func (f *fakeSender) Check(context.Context, Config) error { return nil }
func (f *fakeSender) Send(ctx context.Context, c Config, m Message) SendResult {
	f.mu.Lock()
	f.messages = append(f.messages, m)
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release != nil {
		<-f.release
	}
	if f.result.Status != "" {
		return f.result
	}
	return SendResult{Status: "sent", Detail: "An SMTP übergeben"}
}
func (f *fakeSender) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.messages) }

type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func (f *fakeClock) Now() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
func (f *fakeClock) Wait(ctx context.Context, d time.Duration) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if d > 0 {
		f.waits = append(f.waits, d)
		f.now = f.now.Add(d)
	}
	return nil
}
func newClock() *fakeClock { return &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)} }
func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func waitEngine(t *testing.T, e *Engine) {
	t.Helper()
	e.mu.Lock()
	done := e.done
	e.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not finish")
	}
}
func TestEnginePersistsBatchesAndNeverResends(t *testing.T) {
	s := testStore(t)
	f := &fakeSender{}
	clock := newClock()
	e := NewEngine(s, f, clock, nil)
	p := samplePlan(t)
	if err := e.Start(p, validRequest().Config); err != nil {
		t.Fatal(err)
	}
	waitEngine(t, e)
	if f.count() != 3 {
		t.Fatalf("sent %d", f.count())
	}
	if len(clock.waits) != 2 || clock.waits[0] != 30*time.Second || clock.waits[1] != 60*time.Minute {
		t.Fatalf("incorrect batch schedule %v", clock.waits)
	}
	if err := e.Start(p, validRequest().Config); err != nil {
		t.Fatal(err)
	}
	waitEngine(t, e)
	if f.count() != 3 {
		t.Fatal("repeat start resent mail")
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range state.Deliveries {
		if d.Status != "sent" {
			t.Fatal(d)
		}
	}
	changed := validRequest()
	changed.Schedule.Seconds = 31
	p, err = Prepare(changed, p.Report)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Start(p, validRequest().Config); err != nil {
		t.Fatal(err)
	}
	waitEngine(t, e)
	if f.count() != 3 {
		t.Fatal("reimport bypassed receipts")
	}
}
func TestConcurrentStartAndStop(t *testing.T) {
	s := testStore(t)
	f := &fakeSender{entered: make(chan struct{}, 1), release: make(chan struct{})}
	e := NewEngine(s, f, newClock(), nil)
	p := samplePlan(t)
	if err := e.Start(p, validRequest().Config); err != nil {
		t.Fatal(err)
	}
	<-f.entered
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.Start(p, validRequest().Config); err == nil {
				t.Error("concurrent start accepted")
			}
		}()
	}
	wg.Wait()
	e.Stop()
	close(f.release)
	waitEngine(t, e)
	if f.count() != 1 {
		t.Fatal("Stop allowed another recipient")
	}
	state, _ := s.State()
	if state.Deliveries[0].Status != "sent" || state.Deliveries[1].Status != "pending" {
		t.Fatal(state.Deliveries)
	}
	f.entered = nil
	f.release = nil
	if err := e.Start(p, validRequest().Config); err != nil {
		t.Fatal(err)
	}
	waitEngine(t, e)
	if f.count() != 3 {
		t.Fatal("resume did not send only remaining contacts")
	}
}
func TestCrashRecoveryAndSingleInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, e := OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	if second, e := OpenStore(path); e == nil {
		second.Close()
		t.Fatal("second process allowed to own database")
	}
	p := samplePlan(t)
	if e = s.Save(p); e != nil {
		t.Fatal(e)
	}
	ok, e := s.Claim(p, p.Messages[0], time.Now())
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	s.Close()
	s, e = OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	state, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if state.Deliveries[0].Status != "uncertain" {
		t.Fatal("in-flight message was not quarantined")
	}
	f := &fakeSender{}
	engine := NewEngine(s, f, newClock(), nil)
	if e = engine.Start(p, validRequest().Config); e != nil {
		t.Fatal(e)
	}
	waitEngine(t, engine)
	if f.count() != 2 {
		t.Fatal("crash recovery repeated in-flight message")
	}
}
func TestSMTPFailureStopsRun(t *testing.T) {
	for _, status := range []string{"failed", "uncertain"} {
		t.Run(status, func(t *testing.T) {
			s := testStore(t)
			f := &fakeSender{result: SendResult{Status: status, Detail: "test failure"}}
			e := NewEngine(s, f, newClock(), nil)
			p := samplePlan(t)
			if err := e.Start(p, validRequest().Config); err != nil {
				t.Fatal(err)
			}
			waitEngine(t, e)
			if f.count() != 1 {
				t.Fatal("continued after SMTP failure")
			}
			state, _ := e.State()
			if state.Error == "" || state.Deliveries[0].Status != status {
				t.Fatal(state)
			}
		})
	}
}
func TestNoSecretsPersisted(t *testing.T) {
	s := testStore(t)
	p := samplePlan(t)
	p.Request.Config.Password = "UNIQUE-SECRET-DO-NOT-STORE"
	if e := s.Save(p); e != nil {
		t.Fatal(e)
	}
	loaded, e := s.Active()
	if e != nil {
		t.Fatal(e)
	}
	if loaded.Request.Config.Password != "" {
		t.Fatal("secret persisted")
	}
	var blob []byte
	if e = s.db.QueryRow("SELECT payload FROM plans").Scan(&blob); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(blob), "UNIQUE-SECRET") {
		t.Fatal("secret in database")
	}
}
func TestStoreFailsClosedOnWriteError(t *testing.T) {
	s := testStore(t)
	p := samplePlan(t)
	if e := s.Save(p); e != nil {
		t.Fatal(e)
	}
	s.db.Exec("PRAGMA query_only=ON")
	ok, e := s.Claim(p, p.Messages[0], time.Now())
	if e == nil || ok {
		t.Fatal("claim succeeded without durable write")
	}
}
func TestSessionApprovalAndTestRecipient(t *testing.T) {
	s := testStore(t)
	f := &fakeSender{}
	engine := NewEngine(s, f, newClock(), nil)
	session := NewSession(engine, f, Report{})
	req := validRequest()
	if _, e := session.Import(fixture(t), "x", false); e != nil {
		t.Fatal(e)
	}
	if e := session.Start(req, "invented"); e == nil {
		t.Fatal("start without test allowed")
	}
	if _, e := session.Test(context.Background(), req, 0, "test@example.org"); e == nil {
		t.Fatal("test without connection check allowed")
	}
	if e := session.Check(context.Background(), req.Config); e != nil {
		t.Fatal(e)
	}
	token, e := session.Test(context.Background(), req, 0, "test@example.org")
	if e != nil {
		t.Fatal(e)
	}
	if f.messages[0].To != "test@example.org" || f.messages[0].Body != samplePlan(t).Messages[0].Body {
		t.Fatal("test recipient/content mismatch")
	}
	if _, e = session.Test(context.Background(), req, 0, "test@example.org"); e != nil || f.count() != 1 {
		t.Fatal("duplicate test click resent mail")
	}
	changed := req
	changed.Body += "changed"
	if e = session.Start(changed, token); e == nil {
		t.Fatal("changed content retained approval")
	}
	changed = req
	changed.Config.Password = "changed"
	if e = session.Start(changed, token); e == nil {
		t.Fatal("changed credentials retained approval")
	}
	if e = session.Start(req, token); e != nil {
		t.Fatal(e)
	}
	waitEngine(t, engine)
	if f.count() != 4 {
		t.Fatal("incorrect recipient count")
	}
	// A failed import must invalidate the old recipients and their approval.
	if _, e = session.Import([]byte("broken"), "bad", false); e == nil {
		t.Fatal("bad import accepted")
	}
	if e = session.Start(req, token); e == nil {
		t.Fatal("failed import retained send approval")
	}
}
func TestSessionFilterRequiresFreshApproval(t *testing.T) {
	s := testStore(t)
	f := &fakeSender{}
	engine := NewEngine(s, f, newClock(), nil)
	session := NewSession(engine, f, Report{})
	session.Import(fixture(t), "x", false)
	req := validRequest()
	session.Check(context.Background(), req.Config)
	token, e := session.Test(context.Background(), req, 0, "test@example.org")
	if e != nil {
		t.Fatal(e)
	}
	r, e := session.Filter(true)
	if e != nil || len(r.Contacts) != 0 {
		t.Fatal("missing verification passed")
	}
	if e = session.Start(req, token); e == nil {
		t.Fatal("filter change retained approval")
	}
}
func TestFixtureContainsOnlySyntheticAddresses(t *testing.T) {
	b, _ := os.ReadFile("testdata/apollo.csv")
	if strings.Contains(string(b), "@firma.de") {
		t.Fatal("unexpected personal fixture data")
	}
}

func TestRealClockCancellationAndShutdown(t *testing.T) {
	clock := RealClock{}
	if time.Since(clock.Now()) > time.Second {
		t.Fatal("invalid clock")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if clock.Wait(ctx, time.Hour) == nil {
		t.Fatal("cancel ignored")
	}
	if clock.Wait(ctx, 0) == nil {
		t.Fatal("cancel ignored for elapsed pause")
	}
	if e := clock.Wait(context.Background(), time.Millisecond); e != nil {
		t.Fatal(e)
	}
	s := testStore(t)
	engine := NewEngine(s, &fakeSender{}, clock, nil)
	engine.Close()
	p := samplePlan(t)
	if e := engine.Start(p, validRequest().Config); e != nil {
		t.Fatal(e)
	}
	engine.Close()
	state, _ := engine.State()
	if state.Running {
		t.Fatal("close returned while running")
	}
}
func TestPreviewMatchesStoredPlanAndHistory(t *testing.T) {
	s := testStore(t)
	sender := &fakeSender{}
	engine := NewEngine(s, sender, newClock(), nil)
	session := NewSession(engine, sender, Report{})
	session.Import(fixture(t), "x", false)
	req := validRequest()
	p, e := session.Preview(req)
	if e != nil {
		t.Fatal(e)
	}
	if p.Report.PreviouslyContacted != 0 {
		t.Fatal("invented history")
	}
	if e = engine.Start(p, req.Config); e != nil {
		t.Fatal(e)
	}
	waitEngine(t, engine)
	preview, e := session.Preview(req)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := s.Active()
	if e != nil {
		t.Fatal(e)
	}
	if preview.ID != saved.ID || preview.Messages[0].Body != saved.Messages[0].Body || preview.Report.PreviouslyContacted != 3 {
		t.Fatal("preview/history mismatch")
	}
}
func TestUncertainTestMailDoesNotRepeat(t *testing.T) {
	s := testStore(t)
	sender := &fakeSender{result: SendResult{Status: "uncertain", Detail: "connection lost"}}
	engine := NewEngine(s, sender, newClock(), nil)
	session := NewSession(engine, sender, Report{})
	session.Import(fixture(t), "x", false)
	req := validRequest()
	session.Check(context.Background(), req.Config)
	for i := 0; i < 2; i++ {
		if _, e := session.Test(context.Background(), req, 0, "test@example.org"); e == nil {
			t.Fatal("uncertain test authorized send")
		}
	}
	if sender.count() != 1 {
		t.Fatal("uncertain test was retried")
	}
}

func TestCorruptSnapshotBlocksResume(t *testing.T) {
	s := testStore(t)
	p := samplePlan(t)
	if e := s.Save(p); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec("UPDATE plans SET payload=replace(CAST(payload AS TEXT),'lena@example.com','wrong@example.com')"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Active(); e == nil {
		t.Fatal("modified recipient snapshot accepted")
	}
	sender := &fakeSender{}
	engine := NewEngine(s, sender, newClock(), nil)
	if e := engine.Start(p, validRequest().Config); e == nil {
		t.Fatal("corrupted saved plan started")
	}
	if sender.count() != 0 {
		t.Fatal("mail sent from corrupt snapshot")
	}
}

func TestSenderCapitalizationCannotBypassReceipts(t *testing.T) {
	s := testStore(t)
	p := samplePlan(t)
	sender := &fakeSender{}
	engine := NewEngine(s, sender, newClock(), nil)
	if e := engine.Start(p, validRequest().Config); e != nil {
		t.Fatal(e)
	}
	waitEngine(t, engine)
	req := validRequest()
	req.Config.Username = "SENDER@EXAMPLE.COM"
	p, e := Prepare(req, p.Report)
	if e != nil {
		t.Fatal(e)
	}
	if e = engine.Start(p, req.Config); e != nil {
		t.Fatal(e)
	}
	waitEngine(t, engine)
	if sender.count() != 3 {
		t.Fatal("sender capitalization caused duplicate outreach")
	}
}

func TestExamplePreviewNeverAuthorizesDelivery(t *testing.T) {
	store := testStore(t)
	sender := &fakeSender{}
	engine := NewEngine(store, sender, newClock(), nil)
	session := NewSession(engine, sender, Report{})
	req := Request{Subject: "Hallo {{First Name}} {{Last Name}}", Body: "{{First Name}} {{Last Name}} bei {{Company|deinem Unternehmen}}"}
	p, err := session.Preview(req)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Example || p.Messages[0].Subject != "Hallo Jane Doe" || p.Messages[0].Body != "Jane Doe bei Example GmbH\n" {
		t.Fatal("wrong example preview", p)
	}
	if len(session.Report.Contacts) != 0 {
		t.Fatal("example became an imported recipient")
	}
	if err = store.Save(p); err == nil {
		t.Fatal("example stored in delivery queue")
	}
	req.Config = validRequest().Config
	req.Schedule = validRequest().Schedule
	if err = session.Check(context.Background(), req.Config); err != nil {
		t.Fatal(err)
	}
	if _, err = session.Test(context.Background(), req, 0, "test@example.org"); err == nil {
		t.Fatal("example test mail allowed")
	}
	if err = session.Start(req, "example-token"); err == nil {
		t.Fatal("example outreach allowed")
	}
	if sender.count() != 0 {
		t.Fatal("example was sent")
	}
	if _, err = session.Import(fixture(t), "synthetic.csv", false); err != nil {
		t.Fatal(err)
	}
	req.Config = Config{}
	p, err = session.Preview(req)
	if err != nil {
		t.Fatal(err)
	}
	if p.Example || p.Messages[0].Subject != "Hallo Lena Beispiel" {
		t.Fatal("import did not replace example")
	}
	if _, err = session.Filter(true); err != nil {
		t.Fatal(err)
	}
	if _, err = session.Preview(req); err == nil {
		t.Fatal("empty filtered import silently replaced with example")
	}
}
