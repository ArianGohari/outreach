package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zalando/go-keyring"
	"io"
	"os"
	"outreach/internal/diagnostics"
	"outreach/internal/googlemail"
	"outreach/internal/outreach"
	"path/filepath"
)

type App struct {
	ctx            context.Context
	store          *outreach.Store
	jobs           *outreach.Jobs
	reporter       *diagnostics.Reporter
	startupProblem *diagnostics.Problem
	services       *desktopServices
	google         *googlemail.Client
}

func NewApp(store *outreach.Store, reporters ...*diagnostics.Reporter) (*App, error) {
	g := googlemail.New(googlemail.Vault{
		Get: func(key string) (string, error) { return nativeServices.getPassword("Outreach.Google", key) },
		Set: func(key, value string) error { return nativeServices.setPassword("Outreach.Google", key, value) },
		Delete: func(key string) error {
			err := nativeServices.deletePassword("Outreach.Google", key)
			if err == keyring.ErrNotFound {
				return nil
			}
			return err
		},
	})
	a, err := newApp(store, googlemail.Sender{Google: g, SMTP: outreach.SMTPSender{}}, outreach.RealClock{}, reporters...)
	if err == nil {
		a.google = g
	}
	return a, err
}

func newApp(store *outreach.Store, sender outreach.Sender, clock outreach.Clock, reporters ...*diagnostics.Reporter) (*App, error) {
	a := &App{store: store}
	if len(reporters) > 0 {
		a.reporter = reporters[0]
	}
	var err error
	a.jobs, err = outreach.NewJobs(store, sender, clock, a.reporter, func() {
		if a.ctx != nil {
			a.desktop().emit(a.ctx, "outreach:changed")
		}
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}
func (a *App) ready() error {
	if a.startupProblem != nil {
		return a.startupProblem
	}
	if a.jobs == nil {
		return diagnostics.New("APP_UNAVAILABLE", "App is not ready", "Local sending data could not be loaded.", "Check the error log, fix the cause and reopen the app.", nil)
	}
	return nil
}
func (a *App) report(operation string, err error, private ...string) error {
	if err == nil {
		return nil
	}
	return a.reporter.Report(operation, err, private...)
}
func requestPrivate(r outreach.Request) []string {
	return []string{r.Config.Password, r.Config.Username, r.Config.Sender(), r.Config.ReplyTo, r.Subject, r.Body}
}
func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) shutdown(ctx context.Context) {
	if a.jobs != nil {
		a.jobs.Close()
	}
}
func (a *App) notifyProblem(p *diagnostics.Problem) {
	if a.ctx != nil {
		a.desktop().emit(a.ctx, "outreach:error", p)
	}
}
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	defer func() {
		if !prevent {
			a.saveWindow(ctx)
		}
	}()
	if a.jobs == nil {
		return false
	}
	views, err := a.jobs.Views()
	if err != nil {
		a.notifyProblem(a.reporter.Report("Check jobs before quitting", err))
		return true
	}
	count := 0
	for _, v := range views {
		if v.State.Running {
			count++
		}
	}
	if count == 0 {
		return false
	}
	answer, err := a.desktop().message(ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Pause jobs and quit?", Message: fmt.Sprintf("%d jobs are running. Transfers already in progress will finish. Resume jobs manually after reopening the app.", count), Buttons: []string{"Keep open", "Pause and quit"}, DefaultButton: "Keep open", CancelButton: "Keep open"})
	if err != nil {
		a.notifyProblem(a.reporter.Report("Open confirmation dialog", err))
		return true
	}
	if answer != "Pause and quit" {
		return true
	}
	a.jobs.Close()
	return false
}
func (a *App) GetWorkspace() (views []outreach.JobView, err error) {
	defer func() { err = a.report("Load jobs", err) }()
	if err = a.ready(); err != nil {
		return
	}
	return a.jobs.Views()
}
func (a *App) CreateJob(name string) (view outreach.JobView, err error) {
	defer func() { err = a.report("Create job", err) }()
	if err = a.ready(); err != nil {
		return
	}
	return a.jobs.Create(name)
}
func (a *App) SaveDraft(id, name string, r outreach.Request) (err error) {
	defer func() { err = a.report("Save job · "+id, err, requestPrivate(r)...) }()
	if err = a.ready(); err != nil {
		return
	}
	job, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	return a.jobs.SaveDraft(job, name, r)
}
func (a *App) GetState(id string) (state outreach.State, err error) {
	defer func() { err = a.report("Load delivery status", err) }()
	if err = a.ready(); err != nil {
		return
	}
	job, e := a.jobs.Get(id)
	if e != nil {
		return state, e
	}
	return job.Engine.State()
}
func (a *App) ChooseCSV(id string, verified bool) (report outreach.Report, err error) {
	defer func() { err = a.report("Import CSV · "+id, err) }()
	if err = a.ready(); err != nil {
		return
	}
	job, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	path, e := a.desktop().openFile(a.ctx, runtime.OpenDialogOptions{Title: "Select Apollo contact export", Filters: []runtime.FileFilter{{DisplayName: "CSV files", Pattern: "*.csv"}}})
	if e != nil {
		return report, e
	}
	if path == "" {
		return report, diagnostics.Cancelled("File selection cancelled")
	}
	f, e := os.Open(path)
	if e != nil {
		return report, fmt.Errorf("Could not open CSV: %w", e)
	}
	defer func() {
		if e := f.Close(); e != nil {
			a.report("Close CSV file", e)
		}
	}()
	b, e := io.ReadAll(io.LimitReader(f, outreach.MaxCSVBytes+1))
	if e != nil {
		return report, fmt.Errorf("Could not read CSV: %w", e)
	}
	report, err = job.Session.Import(b, filepath.Base(path), verified)
	if saveErr := a.jobs.SaveReport(job); saveErr != nil {
		return report, saveErr
	}
	return report, err
}
func (a *App) FilterContacts(id string, verified bool) (report outreach.Report, err error) {
	defer func() { err = a.report("Filter contacts · "+id, err) }()
	if err = a.ready(); err != nil {
		return
	}
	job, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	report, err = job.Session.Filter(verified)
	if saveErr := a.jobs.SaveReport(job); saveErr != nil {
		return report, saveErr
	}
	return report, err
}
func (a *App) Preview(id string, r outreach.Request) (plan outreach.Plan, err error) {
	defer func() { err = a.report("Check message preview · "+id, err, requestPrivate(r)...) }()
	if err = a.ready(); err != nil {
		return
	}
	job, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	return job.Session.Preview(r)
}
func credentialID(c outreach.Config) string {
	return fmt.Sprintf("%s:%d/%s", c.Host, c.Port, c.Username)
}
func (a *App) CheckConnection(id string, c outreach.Config) (err error) {
	defer func() {
		operation := "Check SMTP connection"
		if c.Provider == "google" {
			operation = "Check Google connection"
		}
		err = a.report(operation+" · "+id, err, c.Password, c.Username, c.Sender(), c.ReplyTo)
	}()
	if err = a.ready(); err != nil {
		return
	}
	job, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	if e := job.Session.Check(context.Background(), c); e != nil {
		return e
	}
	if c.Provider == "google" {
		return nil
	}
	if c.Remember {
		if e := a.desktop().setPassword("Outreach", credentialID(c), c.Password); e != nil {
			return diagnostics.New("KEYCHAIN_SAVE", "SMTP connected, but password not saved", e.Error(), "Unlock the system keyring or disable password storage, then check the connection again.", e)
		}
	} else {
		if e := a.desktop().deletePassword("Outreach", credentialID(c)); e != nil && e != keyring.ErrNotFound {
			return diagnostics.New("KEYCHAIN_DELETE", "Could not remove saved password", e.Error(), "Check access to the system keyring and try again.", e)
		}
	}
	return nil
}
func (a *App) LoadPassword(c outreach.Config) (password string, err error) {
	defer func() { err = a.report("Read keyring", err, c.Password, c.Username, c.Sender()) }()
	if err = a.ready(); err != nil {
		return
	}
	if !c.Remember || c.Provider == "google" {
		return "", nil
	}
	password, e := a.desktop().getPassword("Outreach", credentialID(c))
	if e != nil {
		return "", diagnostics.New("KEYCHAIN_READ", "Saved password unavailable", e.Error(), "Enter the password again or unlock the system keyring.", e)
	}
	return password, nil
}
func (a *App) SendTest(id string, r outreach.Request, index int, to string) (token string, err error) {
	defer func() { err = a.report("Send test email · "+id, err, append(requestPrivate(r), to)...) }()
	if err = a.ready(); err != nil {
		return
	}
	job, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	return job.Session.Test(context.Background(), r, index, to)
}
func (a *App) Start(id string, r outreach.Request, token string) (err error) {
	defer func() { err = a.report("Start sending · "+id, err, requestPrivate(r)...) }()
	if err = a.ready(); err != nil {
		return
	}
	job, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	p, e := job.Session.Preview(r)
	if e != nil {
		return e
	}
	historyNotice := ""
	if p.Report.PreviouslyContacted > 0 {
		historyNotice = fmt.Sprintf("\n%d contacts have already been contacted or have an uncertain delivery status. Identical messages remain blocked across all jobs and senders. Changed messages can be sent with a new sending plan.\n", p.Report.PreviouslyContacted)
	}
	answer, e := a.desktop().message(a.ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Start outreach?", Message: fmt.Sprintf("%d validated contacts. Sender: %s\n\nPreviously processed entries in this plan will be skipped. %d excluded CSV entries will not be contacted. Start or resume now?", len(p.Messages), r.Config.Sender(), len(p.Report.Issues)) + historyNotice, Buttons: []string{"Cancel", "Start sending"}, DefaultButton: "Cancel", CancelButton: "Cancel"})
	if e != nil {
		return e
	}
	if answer != "Start sending" {
		return diagnostics.Cancelled("Sending cancelled. This action did not start outreach.")
	}
	return a.jobs.Start(job, r, token)
}
func (a *App) Stop(id string) (err error) {
	defer func() { err = a.report("Stop sending", err) }()
	if err = a.ready(); err != nil {
		return
	}
	job, e := a.jobs.Get(id)
	if e != nil {
		return e
	}
	job.Engine.Stop()
	return nil
}

// Only a category crosses this boundary: frontend exceptions may contain form values.
// Return a value: Wails treats *Problem (which implements error) as a failed call.
func (a *App) ReportClientError(code string) diagnostics.Problem {
	switch code {
	case "UI_BATCH_RANGE":
		return *a.reporter.Report("Check batch pause", diagnostics.New(code, "Invalid batch pause", "Enter a batch pause between 0 and 24 hours.", "Use a number between 0 and 24 hours.", nil))
	case "UI_BATCH_PRECISION":
		return *a.reporter.Report("Check batch pause", diagnostics.New(code, "Invalid batch pause", "Enter a batch pause in whole minutes, such as 0.5 or 1.5 hours.", "The pause must equal a whole number of minutes.", nil))
	case "UI_NO_CONTACTS":
		return *a.reporter.Report("Check contacts", diagnostics.New(code, "No eligible contacts", "No eligible contacts. Check filters and exclusions.", "Check the contact list and verification filter.", nil))
	case "UI_INPUT":
		return *a.reporter.Report("Check input", diagnostics.New(code, "Invalid input", "An input could not be used for this action.", "Correct the input indicated in the app.", nil))
	default:
		return *a.reporter.Report("Interface", diagnostics.New("UI_RUNTIME", "Unexpected interface error", "An interface action could not be completed. Form contents are not logged.", "Check the saved delivery status. If the problem persists, use the error ID for diagnosis.", nil))
	}
}

func (a *App) DiscardEmptyJob(id string) (err error) {
	defer func() { err = a.report("Discard empty draft · "+id, err) }()
	if err = a.ready(); err != nil {
		return
	}
	return a.jobs.DiscardEmpty(id)
}

func (a *App) DeleteJob(id string) (err error) {
	defer func() { err = a.report("Delete job · "+id, err) }()
	if err = a.ready(); err != nil {
		return
	}
	return a.jobs.Delete(id)
}

// Wails wraps callback errors in Error(), so structured data must cross as a string.
func (a *App) formatError(err error) any {
	p := a.reporter.Report("Desktop-Aufruf", err)
	data, marshalErr := json.Marshal(p)
	if marshalErr != nil {
		return p.Error()
	}
	return string(data)
}
