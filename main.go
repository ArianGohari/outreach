package main

import (
	"embed"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"os"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"
	"path/filepath"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dir, configErr := os.UserConfigDir()
	if configErr != nil {
		dir = os.TempDir()
	}
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
	if store != nil {
		defer func() {
			if err := store.Close(); err != nil {
				reporter.Report("Datenbank schließen", err)
			}
		}()
	}
	if startupErr != nil {
		app = &App{reporter: reporter, startupProblem: reporter.Report("App starten", startupErr)}
	}
	err := wails.Run(&options.App{Title: "Outreach", Width: 1000, Height: 900, MinWidth: 640, MinHeight: 600, AssetServer: &assetserver.Options{Assets: assets}, BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 255}, OnStartup: app.startup, OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose, Bind: []interface{}{app}, ErrorFormatter: app.formatError, Logger: &frameworkLogger{reporter: reporter, notify: app.notifyProblem}})
	if err != nil {
		problem := reporter.Report("Desktop-Fenster öffnen", err)
		fmt.Fprintln(os.Stderr, problem.Error())
	}
}
