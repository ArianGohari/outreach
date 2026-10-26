package outreach

import (
	"strings"
	"testing"
)

func validRequest() Request {
	return Request{Config: Config{Host: "smtp.example.com", Port: 587, Username: "sender@example.com", Password: "secret", FromName: "Arian"}, Subject: "Hallo {{First Name}}", Body: "Hallo {{First Name}},\n\n{{Company|deine Firma}}", Schedule: Schedule{Seconds: 30, BatchSize: 2, BatchMinutes: 60}}
}
func samplePlan(t *testing.T) Plan {
	t.Helper()
	r, e := ParseCSV(fixture(t), "x", false)
	if e != nil {
		t.Fatal(e)
	}
	p, e := Prepare(validRequest(), r)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestRenderRules(t *testing.T) {
	fields := map[string]string{"First Name": "Lena", "Title": "", "Company Name": "Example & Co"}
	cases := []struct {
		input, want string
		bad         bool
	}{
		{"Hallo {{First Name}}", "Hallo Lena", false}, {"{{Title|Team}}", "Team", false}, {"{{Company}}", "Example & Co", false},
		{"{{Missing|fallback}}", "", true}, {"{{Title}}", "", true}, {"{{First Name", "", true}, {"x }}", "", true}, {"{{{First Name}}}", "", true},
		{"Hallo\n\nOhne Signatur", "Hallo\n\nOhne Signatur", false},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got, e := Render(c.input, fields)
			if (e != nil) != c.bad || (!c.bad && got != c.want) {
				t.Fatalf("got %q / %v", got, e)
			}
		})
	}
}
func TestPrepareAllMessagesBeforeSending(t *testing.T) {
	r, _ := ParseCSV(fixture(t), "x", false)
	req := validRequest()
	req.Body = "{{Title}}"
	if _, e := Prepare(req, r); e == nil {
		t.Fatal("missing value in later contact must block whole plan")
	}
	req.Body = "{{Title|Team}}"
	p, e := Prepare(req, r)
	if e != nil {
		t.Fatal(e)
	}
	if p.Request.Config.Password != "" {
		t.Fatal("password leaked into persisted plan")
	}
	if p.Messages[1].Body != "Team\n" {
		t.Fatal("fallback mismatch")
	}
	req.Subject = "{{Title|Team}}"
	if _, e = Prepare(req, r); e == nil {
		t.Fatal("multiline subject must fail")
	}
}
func TestTemplateDoesNotEvaluateImportedPlaceholders(t *testing.T) {
	if _, e := Render("{{First Name}}", map[string]string{"First Name": "{{Email}}"}); e == nil {
		t.Fatal("imported placeholder must block")
	}
}
func TestAddressAndHeaderValidation(t *testing.T) {
	for _, s := range []string{"a@example.com,b@example.com", "a@example.com\r\nBcc: b@example.com", "Name <a@example.com>", "ä@example.com", " a@example.com", "a@localhost", "", "a@example.com.", "a@-example.com", "a@exam_ple.com", "a@example..com", strings.Repeat("a", 65) + "@example.com"} {
		if _, e := Address(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if got, e := Address("A@EXAMPLE.COM"); e != nil || got != "a@example.com" {
		t.Fatal(got, e)
	}
	for _, mutate := range []func(*Request){func(r *Request) { r.Config.FromName = "A\r\nBcc: x" }, func(r *Request) { r.Config.ReplyTo = "a@example.com,b@example.com" }, func(r *Request) { r.Subject = "A\nB" }, func(r *Request) { r.Body = "\x00" }, func(r *Request) { r.Subject = strings.Repeat("a", 251) }, func(r *Request) { r.Schedule.Seconds = 0 }, func(r *Request) { r.Schedule.BatchSize = 101 }, func(r *Request) { r.Schedule.BatchMinutes = -1 }} {
		req := validRequest()
		mutate(&req)
		r, _ := ParseCSV(fixture(t), "x", false)
		if _, e := Prepare(req, r); e == nil {
			t.Fatal("invalid request accepted")
		}
	}
}
func TestPlanAndMessageIDsAreStable(t *testing.T) {
	a := samplePlan(t)
	b := samplePlan(t)
	if a.ID != b.ID || a.Messages[0].MessageID != b.Messages[0].MessageID {
		t.Fatal("non-deterministic send identity")
	}
}

func TestReorderedCSVPreservesRunIdentity(t *testing.T) {
	report, _ := ParseCSV(fixture(t), "x", false)
	a, e := Prepare(validRequest(), report)
	if e != nil {
		t.Fatal(e)
	}
	report.Contacts[0], report.Contacts[2] = report.Contacts[2], report.Contacts[0]
	report.Contacts[0].Row = 999
	report.Filename = "renamed.csv"
	b, e := Prepare(validRequest(), report)
	if e != nil {
		t.Fatal(e)
	}
	if a.ID != b.ID {
		t.Fatal("reordering created a new campaign")
	}
}
