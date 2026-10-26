package outreach

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxCSVBytes = 20 * 1024 * 1024

var aliases = map[string]string{"first name": "First Name", "last name": "Last Name", "email": "Email", "email status": "Email Status", "company": "Company Name", "company name": "Company Name", "title": "Title", "city": "City", "industry": "Industry", "contact owner": "Contact Owner", "person linkedin url": "Person Linkedin Url", "apollo contact id": "Apollo Contact Id", "email bounced": "Email Bounced"}

func ParseCSV(data []byte, filename string, verified bool) (Report, error) {
	out := Report{Filename: filename, OnlyVerified: verified, Contacts: []Contact{}, Issues: []Issue{}}
	if len(data) > MaxCSVBytes {
		return out, fmt.Errorf("CSV überschreitet 20 MB")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return out, fmt.Errorf("CSV muss gültiges UTF-8 ohne Nullzeichen enthalten")
	}
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = 0
	headers, err := reader.Read()
	if err != nil {
		return out, fmt.Errorf("CSV-Kopfzeile: %w", err)
	}
	seen := map[string]bool{}
	for _, h := range headers {
		h = strings.TrimSpace(h)
		if a, ok := aliases[strings.ToLower(h)]; ok {
			h = a
		}
		key := strings.ToLower(h)
		if h == "" || seen[key] || strings.ContainsAny(h, "{}|\r\n") || invalidText(h, false) {
			return out, fmt.Errorf("Leere, doppelte oder mehrdeutige CSV-Spalte: %q", h)
		}
		seen[key] = true
		out.Headers = append(out.Headers, h)
	}
	for _, h := range []string{"first name", "last name", "email", "company name"} {
		if !seen[h] {
			return out, fmt.Errorf("Apollo-Kontaktexport nicht eindeutig erkannt: Spalte %q fehlt (Komma als Trennzeichen erforderlich)", h)
		}
	}
	out.HasStatus = seen["email status"]
	var candidates []Contact
	var tokens [][]string
	for {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return out, fmt.Errorf("CSV-Struktur fehlerhaft: %w", e)
		}
		out.Total++
		if out.Total > 50000 {
			return out, fmt.Errorf("Maximal 50.000 Kontakte pro Import")
		}
		line, _ := reader.FieldPos(0)
		fields := map[string]string{}
		for i, h := range out.Headers {
			fields[h] = strings.TrimSpace(row[i])
		}
		email, e := Address(fields["Email"])
		if e != nil {
			out.Issues = append(out.Issues, Issue{line, e.Error()})
			continue
		}
		ids := []string{"email:" + email}
		if id := fields["Apollo Contact Id"]; id != "" {
			ids = append(ids, "apollo:"+id)
		}
		if link := fields["Person Linkedin Url"]; link != "" {
			u, e := url.Parse(link)
			if e != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || (strings.ToLower(u.Host) != "www.linkedin.com" && strings.ToLower(u.Host) != "linkedin.com") || !strings.HasPrefix(u.Path, "/in/") || len(strings.Trim(u.Path, "/")) <= 3 {
				out.Issues = append(out.Issues, Issue{line, "LinkedIn-Personenkennung ungültig"})
				continue
			}
			ids = append(ids, "linkedin:"+strings.ToLower(strings.TrimSuffix(u.Path, "/")))
		}
		candidates = append(candidates, Contact{Row: line, Email: email, Fields: fields})
		tokens = append(tokens, ids)
	}
	// Union all identities before filtering; this also catches transitive duplicates.
	parent := make([]int, len(candidates))
	for i := range parent {
		parent[i] = i
	}
	var root func(int) int
	root = func(i int) int {
		if parent[i] != i {
			parent[i] = root(parent[i])
		}
		return parent[i]
	}
	owners := map[string]int{}
	for i, ids := range tokens {
		for _, id := range ids {
			if j, ok := owners[id]; ok {
				parent[root(i)] = root(j)
			} else {
				owners[id] = i
			}
		}
	}
	used := map[int]bool{}
	groupIDs := map[int][]string{}
	for i, ids := range tokens {
		groupIDs[root(i)] = append(groupIDs[root(i)], ids...)
	}
	for group, ids := range groupIDs {
		sort.Slice(ids, func(i, j int) bool {
			priority := func(v string) int {
				if strings.HasPrefix(v, "apollo:") {
					return 0
				}
				if strings.HasPrefix(v, "linkedin:") {
					return 1
				}
				return 2
			}
			if priority(ids[i]) != priority(ids[j]) {
				return priority(ids[i]) < priority(ids[j])
			}
			return ids[i] < ids[j]
		})
		groupIDs[group] = ids
	}
	for i, c := range candidates {
		if strings.EqualFold(c.Fields["Email Bounced"], "true") {
			out.Issues = append(out.Issues, Issue{c.Row, "Als unzustellbar markiert"})
			continue
		}
		if verified && !strings.EqualFold(c.Fields["Email Status"], "verified") {
			reason := "E-Mail nicht als verifiziert ausgewiesen"
			if !out.HasStatus {
				reason = "Verifizierungsstatus nicht enthalten"
			}
			out.Issues = append(out.Issues, Issue{c.Row, reason})
			continue
		}
		group := root(i)
		if used[group] {
			out.Issues = append(out.Issues, Issue{c.Row, "Doppelter Kontakt oder doppelte E-Mail-Adresse"})
			continue
		}
		used[group] = true
		c.Identity = digest(groupIDs[group][0])
		out.Contacts = append(out.Contacts, c)
	}
	return out, nil
}
