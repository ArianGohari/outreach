package outreach

// Example data never enter Session.Report, the send queue or the recipient history.
func ExampleReport() Report {
	return Report{
		Example: true,
		Headers: []string{"First Name", "Last Name", "Title", "Company Name", "Email", "Contact Owner", "Person Linkedin Url", "City"},
		Contacts: []Contact{{Row: 1, Email: "jane.doe@example.com", Identity: "preview-only-jane-doe", Fields: map[string]string{
			"First Name": "Jane", "Last Name": "Doe", "Title": "Head of Sales", "Company Name": "Example GmbH", "Email": "jane.doe@example.com", "Contact Owner": "Alex Example", "Person Linkedin Url": "https://www.linkedin.com/in/jane-doe-example", "City": "Berlin",
		}}},
		Issues: []Issue{}, Total: 1,
	}
}
