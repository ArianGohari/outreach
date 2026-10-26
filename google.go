package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) googleReady() error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.google == nil {
		return fmt.Errorf("Google-Anmeldung nicht verfügbar")
	}
	return nil
}
func (a *App) GoogleConfigured() (bool, error) {
	if err := a.googleReady(); err != nil {
		return false, a.report("Google-Einrichtung prüfen", err)
	}
	return a.google.Configured(), nil
}
func (a *App) ImportGoogleClient() (err error) {
	defer func() { err = a.report("Google OAuth einrichten", err) }()
	if err = a.googleReady(); err != nil {
		return err
	}
	path, err := a.desktop().openFile(a.ctx, runtime.OpenDialogOptions{Title: "Google Desktop-OAuth-JSON auswählen", Filters: []runtime.FileFilter{{DisplayName: "OAuth-JSON", Pattern: "*.json"}}})
	if err != nil {
		return err
	}
	if path == "" {
		return diagnostics.Cancelled("Dateiauswahl abgebrochen")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return err
	}
	return a.google.ImportClient(data)
}
func (a *App) ConnectGoogle(id string) (config outreach.Config, err error) {
	defer func() { err = a.report("Google-Konto verbinden", err) }()
	if err = a.googleReady(); err != nil {
		return
	}
	job, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	state, err := job.Engine.State()
	if err != nil {
		return config, err
	}
	if state.Running {
		return config, fmt.Errorf("Versand zuerst stoppen")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.google.Login(ctx, a.desktop().openBrowser)
}
func (a *App) DisconnectGoogle(id string, c outreach.Config) (err error) {
	defer func() { err = a.report("Google-Konto trennen", err, c.Username) }()
	if err = a.googleReady(); err != nil {
		return
	}
	_, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	if c.Provider != "google" || c.GoogleGrant == "" {
		return fmt.Errorf("Kein Google-Konto verbunden")
	}
	views, err := a.jobs.Views()
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.State.Running && v.State.Plan != nil && v.State.Plan.Request.Config.GoogleGrant == c.GoogleGrant {
			return fmt.Errorf("Jobs dieses Google-Kontos zuerst stoppen")
		}
	}
	answer, err := a.desktop().message(a.ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Google-Verbindung entfernen?", Message: "Die lokale Freigabe wird für alle Jobs mit dieser Verbindung entfernt. Die Freigabe bei Google bleibt bestehen und kann im Google-Konto widerrufen werden.", Buttons: []string{"Abbrechen", "Verbindung entfernen"}, DefaultButton: "Abbrechen", CancelButton: "Abbrechen"})
	if err != nil {
		return err
	}
	if answer != "Verbindung entfernen" {
		return diagnostics.Cancelled("Verbindung bleibt erhalten")
	}
	return a.jobs.RevokeGoogleGrant(c.GoogleGrant, func() error { return a.google.Forget(c.GoogleGrant) })
}
