package outreach

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

func Render(source string, fields map[string]string) (string, error) {
	var out strings.Builder
	for len(source) > 0 {
		i := strings.Index(source, "{{")
		if i < 0 {
			if strings.Contains(source, "}}") {
				return "", fmt.Errorf("Unvollständiger Platzhalter")
			}
			out.WriteString(source)
			break
		}
		if strings.Contains(source[:i], "}}") {
			return "", fmt.Errorf("Ungültiger Platzhalter")
		}
		out.WriteString(source[:i])
		source = source[i+2:]
		j := strings.Index(source, "}}")
		if j < 0 {
			return "", fmt.Errorf("Platzhalter nicht geschlossen")
		}
		expr := source[:j]
		if strings.ContainsAny(expr, "{}\r\n") {
			return "", fmt.Errorf("Ungültiger Platzhalter")
		}
		parts := strings.SplitN(expr, "|", 2)
		name := strings.TrimSpace(parts[0])
		if name == "Company" {
			name = "Company Name"
		}
		value, ok := fields[name]
		if !ok {
			return "", fmt.Errorf("Unbekannte Spalte: %s", name)
		}
		if strings.TrimSpace(value) == "" {
			if len(parts) == 2 {
				value = parts[1]
			} else {
				return "", fmt.Errorf("Variable fehlt: %s", name)
			}
		}
		out.WriteString(value)
		source = source[j+2:]
	}
	result := out.String()
	if strings.Contains(result, "{{") || strings.Contains(result, "}}") {
		return "", fmt.Errorf("Nicht aufgelöster Platzhalter im Ergebnis")
	}
	return result, nil
}
func Prepare(req Request, report Report) (Plan, error) {
	if report.Example {
		return Plan{}, fmt.Errorf("Beispieldaten sind nur für die Vorschau; zuerst eine Kontaktliste importieren")
	}
	if err := req.Config.Validate(); err != nil {
		return Plan{}, err
	}
	if err := req.Schedule.Validate(); err != nil {
		return Plan{}, err
	}
	return renderPlan(req, report)
}

// Rendering can be used before SMTP setup. Only Prepare creates sendable plans.
func renderPlan(req Request, report Report) (Plan, error) {
	if len(report.Contacts) == 0 {
		return Plan{}, fmt.Errorf("Keine freigegebenen Kontakte")
	}
	p := Plan{Example: report.Example, Request: req, Report: report, Messages: []Message{}}
	p.Request.Config = req.Config.Public()
	for _, c := range report.Contacts {
		subject, e := Render(req.Subject, c.Fields)
		if e != nil {
			return Plan{}, fmt.Errorf("Zeile %d: %w", c.Row, e)
		}
		subject = strings.TrimSpace(subject)
		body, e := Render(req.Body, c.Fields)
		if e != nil {
			return Plan{}, fmt.Errorf("Zeile %d: %w", c.Row, e)
		}
		body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
		if strings.TrimSpace(subject) == "" || invalidText(subject, false) || utf8.RuneCountInString(subject) > 250 {
			return Plan{}, fmt.Errorf("Zeile %d: Betreff leer, zu lang oder mit Steuerzeichen", c.Row)
		}
		if strings.TrimSpace(body) == "" || invalidText(body, true) || len(body) > 1000000 {
			return Plan{}, fmt.Errorf("Zeile %d: Nachricht leer, zu lang oder mit Steuerzeichen", c.Row)
		}
		// SMTP DATA ends on a line boundary. Store that same final newline in the preview.
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if _, e = Address(c.Email); e != nil {
			return Plan{}, fmt.Errorf("Zeile %d: %w", c.Row, e)
		}
		p.Messages = append(p.Messages, Message{Key: c.Identity, To: c.Email, Subject: subject, Body: body, Row: c.Row})
	}
	// File names, source line numbers and input ordering do not create a new run.
	identityMessages := append([]Message(nil), p.Messages...)
	for i := range identityMessages {
		identityMessages[i].Row = 0
	}
	sort.Slice(identityMessages, func(i, j int) bool { return identityMessages[i].Key < identityMessages[j].Key })
	p.ID = digest(struct {
		Config        Config
		Subject, Body string
		Schedule      Schedule
		Messages      []Message
	}{p.Request.Config, req.Subject, req.Body, req.Schedule, identityMessages})
	for i := range p.Messages {
		p.Messages[i].MessageID = "<" + digest(p.ID+"/"+p.Messages[i].Key) + "@outreach.local>"
	}
	return p, nil
}

func ValidatePlan(p Plan) error {
	if p.Example {
		return fmt.Errorf("Beispielvorschauen dürfen nicht gespeichert oder versendet werden")
	}
	r := p.Request
	if r.Config.Password == "" {
		r.Config.Password = "validation-only"
	}
	expected, err := Prepare(r, p.Report)
	if err != nil {
		return fmt.Errorf("Gespeicherter Versandplan ungültig: %w", err)
	}
	expected = scopePlan(expected, p.JobID)
	if expected.ID != p.ID || digest(expected.Messages) != digest(p.Messages) {
		return fmt.Errorf("Gespeicherte Nachrichten stimmen nicht mit dem geprüften Versandplan überein")
	}
	return nil
}

// A job owns its queue. Legacy plans retain their original IDs and delivery history.
func scopePlan(p Plan, jobID string) Plan {
	if jobID == "" || jobID == "legacy" {
		return p
	}
	p.JobID = jobID
	p.ID = digest(jobID + "/" + p.ID)
	for i := range p.Messages {
		p.Messages[i].MessageID = "<" + digest(p.ID+"/"+p.Messages[i].Key) + "@outreach.local>"
	}
	return p
}
