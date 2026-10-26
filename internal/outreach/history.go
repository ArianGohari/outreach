package outreach

// History is advisory for a deliberately new message. Receipts enforce exact-message deduplication.
func (s *Store) History(report Report) (Report, error) {
	rows, e := s.db.Query("SELECT DISTINCT key,recipient FROM deliveries WHERE status IN ('sent','sending','uncertain')")
	if e != nil {
		return report, e
	}
	defer rows.Close()
	keys := map[string]bool{}
	emails := map[string]bool{}
	for rows.Next() {
		var key, email string
		if e = rows.Scan(&key, &email); e != nil {
			return report, e
		}
		keys[key] = true
		emails[email] = true
	}
	if e = rows.Err(); e != nil {
		return report, e
	}
	report.PreviouslyContacted = 0
	for _, c := range report.Contacts {
		if keys[c.Identity] || emails[c.Email] {
			report.PreviouslyContacted++
		}
	}
	return report, nil
}
