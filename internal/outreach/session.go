package outreach

import (
	"context"
	"fmt"
	"sync"
)

// Session owns the imported data and approval. The webview cannot supply recipients.
type Session struct {
	mu             sync.Mutex
	Report         Report
	raw            []byte
	sender         Sender
	engine         *Engine
	checked        string
	tested         string
	testRecipient  string
	uncertainTests map[string]bool
}

func NewSession(e *Engine, s Sender, report Report) *Session {
	return &Session{engine: e, sender: s, Report: report, uncertainTests: map[string]bool{}}
}

func (s *Session) ClearApproval() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checked, s.tested, s.testRecipient = "", "", ""
}
func (s *Session) idle() error {
	state, e := s.engine.State()
	if e != nil {
		return e
	}
	if state.Running {
		return fmt.Errorf("Stop sending first")
	}
	return nil
}
func (s *Session) Import(data []byte, name string, verified bool) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.idle(); e != nil {
		return Report{}, e
	}
	s.tested = ""
	s.raw = nil
	s.Report = Report{}
	r, e := ParseCSV(data, name, verified)
	if e == nil {
		r, e = s.engine.store.History(r)
	}
	if e == nil {
		s.raw = append([]byte(nil), data...)
		s.Report = r
	}
	return r, e
}
func (s *Session) Filter(verified bool) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.idle(); e != nil {
		return Report{}, e
	}
	if s.raw == nil {
		return Report{}, fmt.Errorf("Select the CSV again to change filtering")
	}
	s.tested = ""
	r, e := ParseCSV(s.raw, s.Report.Filename, verified)
	if e == nil {
		r, e = s.engine.store.History(r)
	}
	if e == nil {
		s.Report = r
	}
	return r, e
}
func (s *Session) Preview(r Request) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	report, e := s.engine.store.History(s.Report)
	if e != nil {
		return Plan{}, e
	}
	if len(report.Headers) == 0 {
		report = ExampleReport()
	}
	p, e := renderPlan(r, report)
	return scopePlan(p, s.engine.store.jobID), e
}
func (s *Session) Check(ctx context.Context, c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.idle(); e != nil {
		return e
	}
	s.checked = ""
	s.tested = ""
	if e := s.sender.Check(ctx, c); e != nil {
		return e
	}
	s.checked = digest(c)
	return nil
}
func proof(r Request, p Plan) string {
	return digest(struct {
		Request Request
		ID      string
	}{r, p.ID})
}
func (s *Session) Test(ctx context.Context, r Request, index int, to string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.idle(); e != nil {
		return "", e
	}
	if s.checked != digest(r.Config) {
		return "", fmt.Errorf("Check the connection for these settings first")
	}
	p, e := Prepare(r, s.Report)
	p = scopePlan(p, s.engine.store.jobID)
	if e != nil {
		return "", e
	}
	if index < 0 || index >= len(p.Messages) {
		return "", fmt.Errorf("Invalid preview contact")
	}
	m, e := TestMessage(p.Messages[index], to)
	if e != nil {
		return "", e
	}
	token := proof(r, p)
	recipient := fmt.Sprintf("%s/%d", m.To, index)
	attempt := token + recipient
	if s.uncertainTests[attempt] {
		return "", fmt.Errorf("This test email has an uncertain status; sending again to this test address is blocked")
	}
	if s.tested == token && s.testRecipient == recipient {
		return token, nil
	}
	s.tested = ""
	result := s.sender.Send(ctx, r.Config, m)
	if result.Status == "uncertain" {
		s.uncertainTests[attempt] = true
	}
	if result.Status != "sent" {
		return "", fmt.Errorf("Test email: %w", result.Err())
	}
	s.tested = token
	s.testRecipient = recipient
	return token, nil
}
func (s *Session) Start(r Request, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.idle(); e != nil {
		return e
	}
	p, e := Prepare(r, s.Report)
	p = scopePlan(p, s.engine.store.jobID)
	if e != nil {
		return e
	}
	if token == "" || s.tested != token || proof(r, p) != token || s.checked != digest(r.Config) {
		return fmt.Errorf("Settings changed or test email missing; check the connection and test again")
	}
	return s.engine.Start(p, r.Config)
}
