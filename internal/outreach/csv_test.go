package outreach

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, e := os.ReadFile("testdata/apollo.csv")
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestApolloStructure(t *testing.T) {
	r, e := ParseCSV(fixture(t), "apollo.csv", false)
	if e != nil {
		t.Fatal(e)
	}
	if r.Total != 3 || len(r.Headers) != 8 || len(r.Contacts) != 3 || r.HasStatus {
		t.Fatalf("unexpected report: %+v", r)
	}
	if r.Contacts[0].Fields["Company Name"] != "Beispiel, GmbH" || r.Contacts[2].Fields["Title"] != "Leitung\nVertrieb" {
		t.Fatal("quoted CSV fields shifted or changed")
	}
	r, e = ParseCSV(fixture(t), "apollo.csv", true)
	if e != nil || len(r.Contacts) != 0 || len(r.Issues) != 3 {
		t.Fatal("missing status must never pass verified filter")
	}
}
func TestCSVRejectsAmbiguity(t *testing.T) {
	cases := map[string]string{
		"semicolon":       "First Name;Last Name;Company Name;Email\nA;B;C;a@example.com",
		"duplicate":       "First Name,Last Name,Company Name,Email,email\nA,B,C,a@example.com,b@example.com",
		"alias collision": "First Name,Last Name,Company Name,Company,Email\nA,B,C,D,a@example.com",
		"account export":  "Company Name,Email\nC,a@example.com",
		"short row":       "First Name,Last Name,Company Name,Email\nA,B,C",
		"long row":        "First Name,Last Name,Company Name,Email\nA,B,C,a@example.com,extra",
		"broken quote":    "First Name,Last Name,Company Name,Email\n\"A,B,C,a@example.com",
		"blank header":    "First Name,Last Name,Company Name,Email,\nA,B,C,a@example.com,x",
		"null":            "First Name,Last Name,Company Name,Email\nA,B,C,a@example.com\x00",
		"encoding":        "First Name,Last Name,Company Name,Email\nA,B,\xff,a@example.com",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, e := ParseCSV([]byte(data), "x.csv", false); e == nil {
				t.Fatal("unsafe CSV accepted")
			}
		})
	}
}
func TestCSVFiltersAndTransitiveDedup(t *testing.T) {
	csv := "First Name,Last Name,Company Name,Email,Person Linkedin Url,Email Status,Email Bounced\n" +
		"A,B,C,one@example.com,https://linkedin.com/in/person,verified,false\n" +
		"A,B,C,two@example.com,https://www.linkedin.com/in/person/,verified,false\n" +
		"A,B,C,TWO@example.com,https://linkedin.com/in/other,verified,false\n" +
		"A,B,C,three@example.com,https://linkedin.com/in/other,verified,false\n" +
		"D,E,F,four@example.com,,catch-all,false\n" +
		"G,H,I,five@example.com,,verified,true\n" +
		"G,H,I,not-an-email,,verified,false\n"
	r, e := ParseCSV([]byte(csv), "x", true)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Contacts) != 1 || len(r.Issues) != 6 {
		t.Fatalf("dedup/filter failure: %+v", r)
	}
}
func TestCSVBOMAndOptionalEmptyFields(t *testing.T) {
	r, e := ParseCSV(append([]byte{0xef, 0xbb, 0xbf}, fixture(t)...), "x", false)
	if e != nil {
		t.Fatal(e)
	}
	if r.Contacts[1].Fields["Title"] != "" {
		t.Fatal("empty title was rewritten")
	}
}
func TestCSVSizeLimit(t *testing.T) {
	if _, e := ParseCSV([]byte(strings.Repeat("x", MaxCSVBytes+1)), "x", false); e == nil {
		t.Fatal("oversize accepted")
	}
}

func TestIdentityStableWhenPersonEmailChanges(t *testing.T) {
	h := "First Name,Last Name,Company Name,Email,Person Linkedin Url\n"
	a, e := ParseCSV([]byte(h+"A,B,C,first@example.com,https://linkedin.com/in/person\n"), "a", false)
	if e != nil {
		t.Fatal(e)
	}
	b, e := ParseCSV([]byte(h+"A,B,C,second@example.com,https://www.linkedin.com/in/person/\n"), "b", false)
	if e != nil {
		t.Fatal(e)
	}
	if a.Contacts[0].Identity != b.Contacts[0].Identity {
		t.Fatal("person identifier depended on email")
	}
}
func FuzzCSVNeverPanics(f *testing.F) {
	b, _ := os.ReadFile("testdata/apollo.csv")
	f.Add(b)
	f.Add([]byte("\xef\xbb\xbf"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 100000 {
			return
		}
		r, e := ParseCSV(b, "fuzz", false)
		if e == nil {
			seen := map[string]bool{}
			for _, c := range r.Contacts {
				if seen[c.Email] {
					t.Fatal("duplicate recipient")
				}
				seen[c.Email] = true
				if _, e := Address(c.Email); e != nil {
					t.Fatal(e)
				}
			}
		}
	})
}
