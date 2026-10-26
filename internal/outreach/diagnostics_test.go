package outreach

import (
	"context"
	"errors"
	"net"
	"net/textproto"
	"os"
	"outreach/internal/diagnostics"
	"strings"
	"testing"
)

func TestSMTPDiagnosticCodes(t *testing.T) {
	c := validRequest().Config
	for _, tc := range []struct {
		step      string
		err       error
		code      string
		uncertain bool
	}{
		{"AUTH", &textproto.Error{Code: 535, Msg: "5.7.8 Authentication credentials invalid"}, "SMTP_AUTH", false},
		{"CONNECT", &net.DNSError{Err: "no such host", Name: "invalid.example"}, "SMTP_DNS", false},
		{"CONNECT", &net.DNSError{Err: "timeout", IsTimeout: true}, "SMTP_DNS", false},
		{"ACK", errors.New("connection reset"), "SMTP_ACK", true},
	} {
		t.Run(tc.code, func(t *testing.T) {
			f := SMTPFailure(tc.step, c, tc.err, tc.uncertain)
			if f.Code != tc.code || f.Step != tc.step || !errors.Is(f, tc.err) || f.Hint == "" {
				t.Fatal(f)
			}
			if tc.step == "AUTH" && (f.SMTPCode != 535 || !strings.Contains(f.Details, "5.7.8")) {
				t.Fatal(f)
			}
			if tc.uncertain && !strings.Contains(f.Hint, "kein automatischer erneuter Versand") {
				t.Fatal(f)
			}
		})
	}
}
func TestWorkerErrorLoggedAndPersisted(t *testing.T) {
	log, err := diagnostics.OpenLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := samplePlan(t)
	c := validRequest().Config
	sender := &fakeSender{result: failedSend("uncertain", SMTPFailure("ACK", c, errors.New("connection reset"), true))}
	e := NewEngine(testStore(t), sender, newClock(), nil, diagnostics.NewReporter(log))
	if err = e.Start(p, c); err != nil {
		t.Fatal(err)
	}
	waitEngine(t, e)
	state, err := e.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Problem == nil || !state.Problem.Logged || state.Problem.Step != "ACK" || state.Running {
		t.Fatal(state)
	}
	if !strings.Contains(state.Deliveries[0].Detail, state.Problem.ID) || state.Deliveries[0].Status != "uncertain" || sender.count() != 1 {
		t.Fatal(state)
	}
	b, err := os.ReadFile(log.Path())
	if err != nil || !strings.Contains(string(b), state.Problem.ID) {
		t.Fatal(err, string(b))
	}
}

type panicSender struct{ fakeSender }

func (*panicSender) Send(context.Context, Config, Message) SendResult {
	panic("sensitive-panic-message")
}
func TestWorkerPanicQuarantinesClaim(t *testing.T) {
	log, err := diagnostics.OpenLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := testStore(t)
	e := NewEngine(store, &panicSender{}, newClock(), nil, diagnostics.NewReporter(log))
	p := samplePlan(t)
	if err = e.Start(p, validRequest().Config); err != nil {
		t.Fatal(err)
	}
	waitEngine(t, e)
	state, err := e.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Problem == nil || state.Problem.Code != "WORKER_PANIC" || state.Deliveries[0].Status != "uncertain" || state.Running {
		t.Fatal(state)
	}
	b, _ := os.ReadFile(log.Path())
	if strings.Contains(string(b), "sensitive-panic-message") {
		t.Fatal("panic data leaked")
	}
	sender := &fakeSender{}
	resumed := NewEngine(store, sender, newClock(), nil)
	if err = resumed.Start(p, validRequest().Config); err != nil {
		t.Fatal(err)
	}
	waitEngine(t, resumed)
	if sender.count() != len(p.Messages)-1 {
		t.Fatal("uncertain claim retried")
	}
}
