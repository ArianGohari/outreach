// google-check tests browser authorization only. It never sends email or opens the outreach database.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"outreach/internal/googlemail"

	"github.com/pkg/browser"
	"github.com/zalando/go-keyring"
)

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: google-check /path/to/desktop-client.json")
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		return fmt.Errorf("Could not open OAuth JSON")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return fmt.Errorf("Could not read OAuth JSON")
	}
	g := googlemail.New(googlemail.Vault{
		Get:    func(k string) (string, error) { return keyring.Get("Outreach.Google", k) },
		Set:    func(k, v string) error { return keyring.Set("Outreach.Google", k, v) },
		Delete: func(k string) error { return keyring.Delete("Outreach.Google", k) },
	})
	if err = g.ImportClient(data); err != nil {
		return err
	}
	fmt.Println("OAuth client imported. Complete sign-in in your browser; no email will be sent.")
	c, err := g.Login(context.Background(), browser.OpenURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := g.Forget(c.GoogleGrant); err != nil {
			fmt.Fprintln(os.Stderr, "Could not remove the temporary test credential from the system keyring.")
		}
	}()
	if err = g.Check(context.Background(), c); err != nil {
		return err
	}
	fmt.Println("Google sign-in and sending permission confirmed. No email sent. The OAuth client is configured for Outreach; connect accounts within their jobs.")
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
