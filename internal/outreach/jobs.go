package outreach

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"outreach/internal/diagnostics"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

type Draft struct {
	Request Request `json:"request"`
	Report  Report  `json:"report"`
}
type JobView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Draft Draft  `json:"draft"`
	State State  `json:"state"`
	Empty bool   `json:"empty"`
}
type Job struct {
	ID        string
	Session   *Session
	Engine    *Engine
	op        sync.Mutex
	discarded bool
}
type Jobs struct {
	mu       sync.Mutex
	startMu  sync.Mutex
	store    *Store
	sender   Sender
	clock    Clock
	reporter *diagnostics.Reporter
	notify   func()
	entries  map[string]*Job
}

func NewJobs(store *Store, sender Sender, clock Clock, reporter *diagnostics.Reporter, notify func()) (*Jobs, error) {
	j := &Jobs{store: store, sender: sender, clock: clock, reporter: reporter, notify: notify, entries: map[string]*Job{}}
	if _, err := store.db.Exec(`CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY,name TEXT NOT NULL,draft BLOB NOT NULL,raw BLOB,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return nil, err
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM jobs").Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		old, err := store.Active()
		if err != nil {
			return nil, err
		}
		if old != nil {
			d := Draft{Request: old.Request, Report: old.Report}
			d.Request.Config = d.Request.Config.Public()
			blob, err := json.Marshal(d)
			if err != nil {
				return nil, err
			}
			tx, err := store.db.Begin()
			if err != nil {
				return nil, err
			}
			defer tx.Rollback()
			if _, err = tx.Exec("INSERT INTO jobs(id,name,draft) VALUES('legacy','Bisheriger Versand',?)", blob); err != nil {
				return nil, err
			}
			if _, err = tx.Exec("INSERT INTO meta(key,value) VALUES('active:legacy',?)", old.ID); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
		}
	}
	rows, err := store.db.Query("SELECT id,draft,raw FROM jobs ORDER BY created_at,rowid")
	if err != nil {
		return nil, err
	}
	type saved struct {
		id    string
		draft Draft
		raw   []byte
	}
	var records []saved
	for rows.Next() {
		var r saved
		var blob []byte
		if err = rows.Scan(&r.id, &blob, &r.raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(blob, &r.draft); err != nil {
			rows.Close()
			return nil, err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		j.entries[r.id] = j.runtime(r.id, r.draft, r.raw)
	}
	// ponytail: reuse the strict empty check; never prune user content or history.
	for _, r := range records {
		empty, err := j.empty(r.id)
		if err != nil {
			return nil, err
		}
		if empty {
			if err = j.DiscardEmpty(r.id); err != nil {
				return nil, err
			}
		}
	}
	return j, nil
}
func (j *Jobs) runtime(id string, d Draft, raw []byte) *Job {
	e := NewEngine(j.store.ForJob(id), j.sender, j.clock, j.notify, j.reporter)
	s := NewSession(e, j.sender, d.Report)
	s.raw = raw
	return &Job{ID: id, Engine: e, Session: s}
}
func (j *Jobs) Create(name string) (JobView, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if strings.TrimSpace(name) == "" {
		name = "Neuer Job"
	}
	if utf8.RuneCountInString(name) > 100 || invalidText(name, false) {
		return JobView{}, fmt.Errorf("Jobname: höchstens 100 Zeichen ohne Steuerzeichen")
	}
	if name == "Neuer Job" {
		ids := make([]string, 0, len(j.entries))
		for id := range j.entries {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			job := j.entries[id]
			if !job.op.TryLock() {
				continue
			}
			empty, err := j.empty(id)
			job.op.Unlock()
			if err != nil {
				return JobView{}, err
			}
			if empty {
				return JobView{ID: id, Name: name, Draft: blankDraft(), State: State{Deliveries: []Delivery{}}, Empty: true}, nil
			}
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return JobView{}, err
	}
	id := hex.EncodeToString(random[:])
	d := blankDraft()
	blob, err := json.Marshal(d)
	if err != nil {
		return JobView{}, err
	}
	if _, err = j.store.db.Exec("INSERT INTO jobs(id,name,draft) VALUES(?,?,?)", id, name, blob); err != nil {
		return JobView{}, err
	}
	job := j.runtime(id, d, nil)
	j.entries[id] = job
	return JobView{ID: id, Name: name, Draft: d, State: State{Deliveries: []Delivery{}}, Empty: true}, nil
}
func (j *Jobs) Get(id string) (*Job, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job := j.entries[id]
	if job == nil {
		return nil, fmt.Errorf("Job nicht gefunden")
	}
	return job, nil
}
func (j *Jobs) Acquire(id string) (*Job, func(), error) {
	job, err := j.Get(id)
	if err != nil {
		return nil, nil, err
	}
	if !job.op.TryLock() {
		return nil, nil, fmt.Errorf("Für diesen Job läuft noch eine Aktion. Bitte kurz warten")
	}
	if job.discarded {
		job.op.Unlock()
		return nil, nil, fmt.Errorf("Job wurde verworfen")
	}
	return job, job.op.Unlock, nil
}
func (j *Jobs) Views() ([]JobView, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	rows, err := j.store.db.Query("SELECT id,name,draft FROM jobs ORDER BY created_at,rowid")
	if err != nil {
		return nil, err
	}
	views := []JobView{}
	for rows.Next() {
		var v JobView
		var blob []byte
		if err = rows.Scan(&v.ID, &v.Name, &blob); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(blob, &v.Draft); err != nil {
			rows.Close()
			return nil, err
		}
		views = append(views, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range views {
		job := j.entries[views[i].ID]
		if job == nil {
			return nil, fmt.Errorf("Job nicht gefunden")
		}
		views[i].Empty, err = j.empty(views[i].ID)
		if err != nil {
			return nil, err
		}
		views[i].State, err = job.Engine.State()
		if err != nil {
			return nil, err
		}
	}
	return views, nil
}

// Call with the job operation lock held. SMTP credentials never enter the draft JSON.
func (j *Jobs) SaveDraft(job *Job, name string, req Request) error {
	state, err := job.Engine.State()
	if err != nil {
		return err
	}
	if state.Running {
		return fmt.Errorf("Laufenden Job vor Änderungen pausieren")
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 || invalidText(name, false) {
		return fmt.Errorf("Jobname: 1 bis 100 Zeichen ohne Steuerzeichen")
	}
	if len(req.Body) > 1000000 || len(req.Subject) > 10000 {
		return fmt.Errorf("Nachricht ist zu lang")
	}
	req.Config = req.Config.Public()
	job.Session.mu.Lock()
	d := Draft{Request: req, Report: job.Session.Report}
	raw := append([]byte(nil), job.Session.raw...)
	job.Session.mu.Unlock()
	blob, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = j.store.db.Exec("UPDATE jobs SET name=?,draft=?,raw=? WHERE id=?", name, blob, raw, job.ID)
	return err
}
func (j *Jobs) SaveReport(job *Job) error {
	var name string
	var blob []byte
	if err := j.store.db.QueryRow("SELECT name,draft FROM jobs WHERE id=?", job.ID).Scan(&name, &blob); err != nil {
		return err
	}
	var d Draft
	if err := json.Unmarshal(blob, &d); err != nil {
		return err
	}
	return j.SaveDraft(job, name, d.Request)
}
func (j *Jobs) Start(job *Job, req Request, token string) error {
	j.startMu.Lock()
	defer j.startMu.Unlock()
	sender, err := Address(req.Config.Sender())
	if err != nil {
		return err
	}
	views, err := j.Views()
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.ID == job.ID || !v.State.Running || v.State.Plan == nil {
			continue
		}
		other, _ := Address(v.State.Plan.Request.Config.Sender())
		if sender == other {
			return fmt.Errorf("Für diese Absenderadresse läuft bereits der Job „%s“. Diesen zuerst pausieren", v.Name)
		}
	}
	return job.Session.Start(req, token)
}
func (j *Jobs) Close() {
	j.mu.Lock()
	jobs := make([]*Job, 0, len(j.entries))
	for _, job := range j.entries {
		jobs = append(jobs, job)
	}
	j.mu.Unlock()
	for _, job := range jobs {
		job.Engine.Stop()
	}
	for _, job := range jobs {
		job.Engine.Close()
	}
}

func blankDraft() Draft {
	return Draft{Request: Request{Config: Config{Port: 587}, Schedule: Schedule{Seconds: 30, BatchSize: 50, BatchMinutes: 1440}}, Report: Report{OnlyVerified: true, Headers: []string{}, Contacts: []Contact{}, Issues: []Issue{}}}
}

// Only untouched defaults qualify. Even an altered name, port or schedule is user data.
func (j *Jobs) empty(id string) (bool, error) {
	var name string
	var blob []byte
	var rawBytes, history int
	err := j.store.db.QueryRow(`SELECT name,draft,coalesce(length(raw),0),
 (SELECT count(*) FROM meta WHERE key=?) + (SELECT count(*) FROM plans WHERE json_extract(CAST(payload AS TEXT),'$.jobId')=?)
 FROM jobs WHERE id=?`, "active:"+id, id, id).Scan(&name, &blob, &rawBytes, &history)
	if err != nil {
		return false, err
	}
	var d Draft
	if err = json.Unmarshal(blob, &d); err != nil {
		return false, err
	}
	r := d.Report
	return name == "Neuer Job" && d.Request == blankDraft().Request && rawBytes == 0 && history == 0 && r.Filename == "" && !r.Example && !r.HasStatus && r.OnlyVerified && r.Total == 0 && r.PreviouslyContacted == 0 && len(r.Headers) == 0 && len(r.Contacts) == 0 && len(r.Issues) == 0, nil
}
func (j *Jobs) DiscardEmpty(id string) error {
	job, release, err := j.Acquire(id)
	if err != nil {
		return err
	}
	defer release()
	j.mu.Lock()
	defer j.mu.Unlock()
	empty, err := j.empty(id)
	if err != nil {
		return err
	}
	if !empty {
		return fmt.Errorf("Nur vollständig leere Entwürfe ohne Versandhistorie können verworfen werden")
	}
	state, err := job.Engine.State()
	if err != nil {
		return err
	}
	if state.Running {
		return fmt.Errorf("Laufender Job kann nicht verworfen werden")
	}
	if _, err = j.store.db.Exec("DELETE FROM jobs WHERE id=?", id); err != nil {
		return err
	}
	job.discarded = true
	delete(j.entries, id)
	return nil
}

func (j *Jobs) Delete(id string) error {
	job, release, err := j.Acquire(id)
	if err != nil {
		return err
	}
	defer release()
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := job.Engine.State()
	if err != nil {
		return err
	}
	if state.Running {
		return fmt.Errorf("Job zuerst pausieren und laufende Übertragung abwarten")
	}
	tx, err := j.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// ponytail: delete only the workspace entry; retain all plans and duplicate reservations.
	if _, err = tx.Exec("DELETE FROM jobs WHERE id=?", id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM meta WHERE key=?", "active:"+id); err != nil {
		return err
	}
	if id == "legacy" {
		// Otherwise the next startup would import the deleted legacy job again.
		if _, err = tx.Exec("DELETE FROM meta WHERE key='active'"); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	job.discarded = true
	delete(j.entries, id)
	return nil
}
