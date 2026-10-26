//go:build !nativee2e

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStartupIsUsableOrFailsClosed(t *testing.T) {
	for _, mode := range []string{"normal", "config directory", "log path", "database", "second instance"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			var configErr error
			switch mode {
			case "config directory":
				configErr = errors.New("user config unavailable")
			case "log path":
				if err := os.WriteFile(filepath.Join(dir, "Outreach"), []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
			case "database":
				if err := os.MkdirAll(filepath.Join(dir, "Outreach", "outreach.db"), 0700); err != nil {
					t.Fatal(err)
				}
			case "second instance":
				first, store := openApp(dir, nil)
				if first.ready() != nil {
					t.Fatal(first.ready())
				}
				defer store.Close()
				defer first.shutdown(context.Background())
			}
			a, store := openApp(dir, configErr)
			if store != nil {
				defer store.Close()
			}
			defer a.shutdown(context.Background())
			if mode == "normal" {
				if a.ready() != nil {
					t.Fatal(a.ready())
				}
				if _, err := a.CreateJob("Startup"); err != nil {
					t.Fatal(err)
				}
				return
			}
			if a.startupProblem == nil || a.ready() == nil {
				t.Fatal("startup error did not disable sending")
			}
			if _, err := a.CreateJob("Blocked"); err == nil {
				t.Fatal("startup error allowed a job")
			}
			if mode == "log path" && a.startupProblem.LogIssue == "" {
				t.Fatal("unavailable log hidden")
			}
		})
	}
}
