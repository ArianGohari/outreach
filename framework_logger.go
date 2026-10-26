package main

import (
	"crypto/sha256"
	"fmt"
	"outreach/internal/diagnostics"
	"runtime"
)

type frameworkLogger struct {
	reporter *diagnostics.Reporter
	notify   func(*diagnostics.Problem)
}

func (l *frameworkLogger) Print(string)           {}
func (l *frameworkLogger) Trace(string)           {}
func (l *frameworkLogger) Debug(string)           {}
func (l *frameworkLogger) Info(string)            {}
func (l *frameworkLogger) Warning(message string) { l.record(message) }
func (l *frameworkLogger) Error(message string)   { l.record(message) }
func (l *frameworkLogger) Fatal(message string)   { l.record(message) }
func (l *frameworkLogger) record(message string) {
	// Wails messages can contain complete RPC arguments. Retain only a fingerprint and call site.
	hash := sha256.Sum256([]byte(message))
	pc, _, line, _ := runtime.Caller(2)
	name := "Desktop runtime"
	if fn := runtime.FuncForPC(pc); fn != nil {
		name = fn.Name()
	}
	p := l.reporter.Report("Desktop runtime", diagnostics.New("DESKTOP_RUNTIME", "Internal desktop runtime error", fmt.Sprintf("Location: %s:%d. Message fingerprint: %x. Call arguments are not logged.", name, line, hash[:8]), "Check the delivery status. Use the error ID for diagnosis.", nil))
	if l.notify != nil {
		l.notify(p)
	}
}
