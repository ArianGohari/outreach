package outreach

import (
	"context"
	"errors"
	"fmt"
	"outreach/internal/diagnostics"
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
func (RealClock) Wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type Engine struct {
	mu          sync.Mutex
	store       *Store
	sender      Sender
	clock       Clock
	cancel      context.CancelFunc
	done        chan struct{}
	lastError   string
	lastProblem *diagnostics.Problem
	reporter    *diagnostics.Reporter
	notify      func()
}

func NewEngine(store *Store, sender Sender, clock Clock, notify func(), reporters ...*diagnostics.Reporter) *Engine {
	e := &Engine{store: store, sender: sender, clock: clock, notify: notify}
	if len(reporters) > 0 {
		e.reporter = reporters[0]
	}
	return e
}
func (e *Engine) State() (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.store.State()
	s.Running = e.cancel != nil
	s.Error = e.lastError
	s.Problem = e.lastProblem
	return s, err
}
func (e *Engine) Start(p Plan, c Config) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		return fmt.Errorf("Sending is already running")
	}
	if err := e.store.Save(p); err != nil {
		return err
	}
	saved, err := e.store.Active()
	if err != nil {
		return err
	}
	if saved == nil {
		return fmt.Errorf("Sending plan is missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan struct{})
	e.lastError = ""
	e.lastProblem = nil
	go e.run(ctx, *saved, c, e.done)
	return nil
}
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
	}
}
func (e *Engine) Close() {
	e.Stop()
	e.mu.Lock()
	done := e.done
	e.mu.Unlock()
	if done != nil {
		<-done
	}
}
func (e *Engine) emit() {
	if e.notify != nil {
		e.notify()
	}
}
func (e *Engine) run(ctx context.Context, p Plan, c Config, done chan struct{}) {
	var inFlight *Message
	fail := func(operation string, err error) *diagnostics.Problem {
		private := []string{c.Password, c.Username, c.Sender(), c.ReplyTo, p.Request.Subject, p.Request.Body}
		if inFlight != nil {
			private = append(private, inFlight.To, inFlight.Subject, inFlight.Body)
		}
		if e.store.jobID != "" {
			operation += " · Job " + e.store.jobID
		}
		problem := e.reporter.Report(operation, err, private...)
		e.mu.Lock()
		e.lastProblem = problem
		e.lastError = problem.Error()
		e.mu.Unlock()
		return problem
	}
	defer func() {
		if recover() != nil {
			problem := fail("Sending", diagnostics.New("WORKER_PANIC", "Unexpected sending error", "Sending has stopped. A transfer in progress may already have been accepted.", "Do not retry the affected message. Use the error ID for diagnosis.", nil))
			if inFlight != nil {
				if err := e.store.Finish(p, *inFlight, SendResult{Status: "uncertain", Detail: problem.Error()}, e.clock.Now()); err != nil {
					fail("Save delivery status", err)
				}
			}
		}
		e.mu.Lock()
		e.cancel = nil
		close(done)
		e.mu.Unlock()
		e.emit()
	}()
	for _, m := range p.Messages {
		if ctx.Err() != nil {
			return
		}
		s, err := e.store.State()
		if err != nil {
			fail("Load delivery status", err)
			return
		}
		pending := false
		for _, d := range s.Deliveries {
			if d.Key == m.Key && d.Status == "pending" {
				pending = true
				break
			}
		}
		if !pending {
			continue
		}
		if s.NextAt != "" {
			at, err := time.Parse(time.RFC3339Nano, s.NextAt)
			if err != nil {
				fail("Read sending pause", err)
				return
			}
			if err = e.clock.Wait(ctx, at.Sub(e.clock.Now())); err != nil {
				if !errors.Is(err, context.Canceled) {
					fail("Sending pause", err)
				}
				return
			}
		}
		// Serialize Stop with claiming the next recipient.
		claimed, err := func() (bool, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return e.store.Claim(p, m, e.clock.Now())
		}()
		if errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			fail("Reserve recipient before sending", err)
			return
		}
		if !claimed {
			e.emit()
			continue
		}
		inFlight = &m
		e.emit()
		result := e.sender.Send(context.Background(), c, m)
		if result.Status != "sent" {
			problem := fail(fmt.Sprintf("Sending · CSV row %d", m.Row), result.Err())
			result.Detail = problem.Error()
		}
		if err = e.store.Finish(p, m, result, e.clock.Now()); err != nil {
			fail("Save delivery status", err)
			return
		}
		inFlight = nil
		e.emit()
		// A connection/authentication problem should not burn through an entire list.
		if result.Status != "sent" {
			return
		}
	}
}
