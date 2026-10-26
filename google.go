package main

import (
	"context"
	"fmt"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) googleReady() error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.google == nil {
		return fmt.Errorf("Google sign-in is unavailable")
	}
	return nil
}
func (a *App) ConnectGoogle(id string) (config outreach.Config, err error) {
	defer func() { err = a.report("Connect Google account", err) }()
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
		return config, fmt.Errorf("Stop sending first")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.google.Login(ctx, a.desktop().openBrowser)
}
func (a *App) DisconnectGoogle(id string, c outreach.Config) (err error) {
	defer func() { err = a.report("Disconnect Google account", err, c.Username) }()
	if err = a.googleReady(); err != nil {
		return
	}
	_, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	if c.Provider != "google" || c.GoogleGrant == "" {
		return fmt.Errorf("No Google account connected")
	}
	views, err := a.jobs.Views()
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.State.Running && v.State.Plan != nil && v.State.Plan.Request.Config.GoogleGrant == c.GoogleGrant {
			return fmt.Errorf("Stop jobs using this Google connection first")
		}
	}
	answer, err := a.desktop().message(a.ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Remove Google connection?", Message: "Local access will be removed for all jobs sharing this connection. Google consent remains and can be revoked in your Google Account.", Buttons: []string{"Cancel", "Remove connection"}, DefaultButton: "Cancel", CancelButton: "Cancel"})
	if err != nil {
		return err
	}
	if answer != "Remove connection" {
		return diagnostics.Cancelled("Connection kept")
	}
	return a.jobs.RevokeGoogleGrant(c.GoogleGrant, func() error { return a.google.Forget(c.GoogleGrant) })
}
