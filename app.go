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
	"outreach/internal/outreach"
	"path/filepath"
)

type App struct {
	ctx            context.Context
	jobs           *outreach.Jobs
	reporter       *diagnostics.Reporter
	startupProblem *diagnostics.Problem
	services       *desktopServices
}

func NewApp(store *outreach.Store, reporters ...*diagnostics.Reporter) (*App, error) {
	return newApp(store, outreach.SMTPSender{}, outreach.RealClock{}, reporters...)
}

func newApp(store *outreach.Store, sender outreach.Sender, clock outreach.Clock, reporters ...*diagnostics.Reporter) (*App, error) {
	a := &App{}
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
		return diagnostics.New("APP_UNAVAILABLE", "App ist nicht bereit", "Die lokalen Versanddaten konnten nicht geladen werden.", "Fehlerprotokoll prüfen und die App nach Beheben der Ursache erneut öffnen.", nil)
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
func (a *App) beforeClose(ctx context.Context) bool {
	if a.jobs == nil {
		return false
	}
	views, err := a.jobs.Views()
	if err != nil {
		a.notifyProblem(a.reporter.Report("Jobs vor Beenden prüfen", err))
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
	answer, err := a.desktop().message(ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Jobs pausieren und beenden?", Message: fmt.Sprintf("%d Jobs laufen. Bereits laufende Übertragungen werden abgeschlossen. Nach dem Öffnen müssen die Jobs manuell fortgesetzt werden.", count), Buttons: []string{"Weiter geöffnet lassen", "Pausieren und beenden"}, DefaultButton: "Weiter geöffnet lassen", CancelButton: "Weiter geöffnet lassen"})
	if err != nil {
		a.notifyProblem(a.reporter.Report("Bestätigungsdialog öffnen", err))
		return true
	}
	if answer != "Pausieren und beenden" {
		return true
	}
	a.jobs.Close()
	return false
}
func (a *App) GetWorkspace() (views []outreach.JobView, err error) {
	defer func() { err = a.report("Jobs laden", err) }()
	if err = a.ready(); err != nil {
		return
	}
	return a.jobs.Views()
}
func (a *App) CreateJob(name string) (view outreach.JobView, err error) {
	defer func() { err = a.report("Job anlegen", err) }()
	if err = a.ready(); err != nil {
		return
	}
	return a.jobs.Create(name)
}
func (a *App) SaveDraft(id, name string, r outreach.Request) (err error) {
	defer func() { err = a.report("Job speichern · "+id, err, requestPrivate(r)...) }()
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
	defer func() { err = a.report("Versandstatus laden", err) }()
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
	defer func() { err = a.report("CSV importieren · "+id, err) }()
	if err = a.ready(); err != nil {
		return
	}
	job, release, err := a.jobs.Acquire(id)
	if err != nil {
		return
	}
	defer release()
	path, e := a.desktop().openFile(a.ctx, runtime.OpenDialogOptions{Title: "Apollo-Kontaktexport auswählen", Filters: []runtime.FileFilter{{DisplayName: "CSV-Dateien", Pattern: "*.csv"}}})
	if e != nil {
		return report, e
	}
	if path == "" {
		return report, diagnostics.Cancelled("Dateiauswahl abgebrochen")
	}
	f, e := os.Open(path)
	if e != nil {
		return report, fmt.Errorf("CSV konnte nicht geöffnet werden: %w", e)
	}
	defer func() {
		if e := f.Close(); e != nil {
			a.report("CSV-Datei schließen", e)
		}
	}()
	b, e := io.ReadAll(io.LimitReader(f, outreach.MaxCSVBytes+1))
	if e != nil {
		return report, fmt.Errorf("CSV konnte nicht gelesen werden: %w", e)
	}
	report, err = job.Session.Import(b, filepath.Base(path), verified)
	if saveErr := a.jobs.SaveReport(job); saveErr != nil {
		return report, saveErr
	}
	return report, err
}
func (a *App) FilterContacts(id string, verified bool) (report outreach.Report, err error) {
	defer func() { err = a.report("Kontakte filtern · "+id, err) }()
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
	defer func() { err = a.report("Nachrichtenvorschau prüfen · "+id, err, requestPrivate(r)...) }()
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
		err = a.report("SMTP-Verbindung prüfen · "+id, err, c.Password, c.Username, c.Sender(), c.ReplyTo)
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
	if c.Remember {
		if e := a.desktop().setPassword("Outreach", credentialID(c), c.Password); e != nil {
			return diagnostics.New("KEYCHAIN_SAVE", "SMTP verbunden, aber Passwort nicht gespeichert", e.Error(), "Betriebssystem-Schlüsselbund entsperren oder das Speichern des Passworts deaktivieren und die Verbindung erneut prüfen.", e)
		}
	} else {
		if e := a.desktop().deletePassword("Outreach", credentialID(c)); e != nil && e != keyring.ErrNotFound {
			return diagnostics.New("KEYCHAIN_DELETE", "Gespeichertes Passwort konnte nicht entfernt werden", e.Error(), "Zugriff auf den Betriebssystem-Schlüsselbund prüfen und erneut versuchen.", e)
		}
	}
	return nil
}
func (a *App) LoadPassword(c outreach.Config) (password string, err error) {
	defer func() { err = a.report("Schlüsselbund lesen", err, c.Password, c.Username, c.Sender()) }()
	if err = a.ready(); err != nil {
		return
	}
	if !c.Remember {
		return "", nil
	}
	password, e := a.desktop().getPassword("Outreach", credentialID(c))
	if e != nil {
		return "", diagnostics.New("KEYCHAIN_READ", "Gespeichertes Passwort nicht verfügbar", e.Error(), "Passwort erneut eingeben oder den Betriebssystem-Schlüsselbund entsperren.", e)
	}
	return password, nil
}
func (a *App) SendTest(id string, r outreach.Request, index int, to string) (token string, err error) {
	defer func() { err = a.report("Testmail senden · "+id, err, append(requestPrivate(r), to)...) }()
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
	defer func() { err = a.report("Versand starten · "+id, err, requestPrivate(r)...) }()
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
		historyNotice = fmt.Sprintf("\n%d Kontakte wurden bereits angeschrieben oder haben einen unklaren Versandstatus. Identische Nachrichten bleiben über alle Jobs und Absender hinweg gesperrt. Geänderte Nachrichten können bei einem neuen Versandplan erneut versendet werden.\n", p.Report.PreviouslyContacted)
	}
	answer, e := a.desktop().message(a.ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Outreach-Versand starten?", Message: fmt.Sprintf("%d geprüfte Kontakte. Absender: %s\n\nBereits bearbeitete Einträge desselben Versandplans werden übersprungen. %d ausgeschlossene CSV-Einträge werden nicht angeschrieben. Jetzt starten oder fortsetzen?", len(p.Messages), r.Config.Sender(), len(p.Report.Issues)) + historyNotice, Buttons: []string{"Abbrechen", "Versand starten"}, DefaultButton: "Abbrechen", CancelButton: "Abbrechen"})
	if e != nil {
		return e
	}
	if answer != "Versand starten" {
		return diagnostics.Cancelled("Versandstart abgebrochen")
	}
	return a.jobs.Start(job, r, token)
}
func (a *App) Stop(id string) (err error) {
	defer func() { err = a.report("Versand stoppen", err) }()
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
		return *a.reporter.Report("Batchpause prüfen", diagnostics.New(code, "Ungültige Batchpause", "Batchpause zwischen 0 und 24 Stunden eingeben.", "Eine Zahl zwischen 0 und 24 Stunden verwenden.", nil))
	case "UI_BATCH_PRECISION":
		return *a.reporter.Report("Batchpause prüfen", diagnostics.New(code, "Ungültige Batchpause", "Batchpause minutengenau eingeben, z. B. 0,5 oder 1,5 Stunden.", "Die Pause muss einer ganzen Anzahl Minuten entsprechen.", nil))
	case "UI_NO_CONTACTS":
		return *a.reporter.Report("Kontakte prüfen", diagnostics.New(code, "Keine freigegebenen Kontakte", "Keine freigegebenen Kontakte. Filter und Ausschlüsse prüfen.", "Kontaktliste und Verifizierungsfilter prüfen.", nil))
	case "UI_INPUT":
		return *a.reporter.Report("Eingabe prüfen", diagnostics.New(code, "Ungültige Eingabe in der Oberfläche", "Eine Eingabe konnte nicht für die Aktion übernommen werden.", "Die in der Oberfläche genannte Eingabe korrigieren.", nil))
	default:
		return *a.reporter.Report("Oberfläche", diagnostics.New("UI_RUNTIME", "Unerwarteter Fehler in der Oberfläche", "Eine Aktion der Oberfläche konnte nicht abgeschlossen werden. Formularinhalte werden nicht protokolliert.", "Gespeicherten Versandstatus prüfen. Bei wiederholtem Auftreten Fehler-ID zur Diagnose verwenden.", nil))
	}
}

func (a *App) DiscardEmptyJob(id string) (err error) {
	defer func() { err = a.report("Leeren Entwurf verwerfen · "+id, err) }()
	if err = a.ready(); err != nil {
		return
	}
	return a.jobs.DiscardEmpty(id)
}

func (a *App) DeleteJob(id string) (err error) {
	defer func() { err = a.report("Job löschen · "+id, err) }()
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
