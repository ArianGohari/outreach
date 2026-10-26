//go:build !nativee2e

package main

import (
	"fmt"
	"github.com/wailsapp/wails/v2"
	"os"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"
	"path/filepath"
)

func main() {
	dir, configErr := os.UserConfigDir()
	if configErr != nil {
		dir = os.TempDir()
	}
	app, store := openApp(dir, configErr)
	if store != nil {
		defer func() {
			if err := store.Close(); err != nil {
				app.reporter.Report("Datenbank schließen", err)
			}
		}()
	}
	if err := wails.Run(desktopOptions(app)); err != nil {
		problem := app.reporter.Report("Desktop-Fenster öffnen", err)
		fmt.Fprintln(os.Stderr, problem.Error())
	}
}

func openApp(dir string, configErr error) (*App, *outreach.Store) {
	log, logErr := diagnostics.OpenLog(filepath.Join(dir, "Outreach", "logs"))
	var recorder diagnostics.Recorder
	if log != nil {
		recorder = log
	}
	reporter := diagnostics.NewReporter(recorder)
	var app *App
	var store *outreach.Store
	var startupErr error
	if configErr != nil {
		startupErr = fmt.Errorf("Benutzer-Datenordner nicht verfügbar: %w", configErr)
	} else if logErr != nil {
		startupErr = fmt.Errorf("Fehlerprotokoll konnte nicht geöffnet werden: %w", logErr)
	} else {
		store, startupErr = outreach.OpenStore(filepath.Join(dir, "Outreach", "outreach.db"))
		if startupErr == nil {
			app, startupErr = NewApp(store, reporter)
		}
	}
	if startupErr != nil {
		app = &App{reporter: reporter, startupProblem: reporter.Report("App starten", startupErr)}
	}
	return app, store
}
