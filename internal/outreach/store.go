package outreach

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	jobID string
	db    *sql.DB
	lock  *flock.Flock
}

func OpenStore(path string) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	lock := flock.New(path + ".lock")
	ok, e := lock.TryLock()
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, fmt.Errorf("The sending database is already open in another app instance")
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		lock.Unlock()
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, lock: lock}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS plans(id TEXT PRIMARY KEY, payload BLOB NOT NULL, next_at TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS deliveries(plan_id TEXT NOT NULL, key TEXT NOT NULL, recipient TEXT NOT NULL, position INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'pending', detail TEXT NOT NULL DEFAULT '', updated TEXT NOT NULL DEFAULT '', PRIMARY KEY(plan_id,key), UNIQUE(plan_id,recipient));
 CREATE TABLE IF NOT EXISTS receipts(token TEXT PRIMARY KEY, plan_id TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 UPDATE deliveries SET status='uncertain', detail='App closed during sending; retry blocked' WHERE status='sending';`)
	if e != nil {
		s.Close()
		return nil, e
	}
	if e = s.migrateGlobalReceipts(); e != nil {
		s.Close()
		return nil, e
	}
	_ = os.Chmod(path, 0600)
	return s, nil
}
func (s *Store) ForJob(id string) *Store { return &Store{db: s.db, jobID: id} }
func (s *Store) activeKey() string {
	if s.jobID == "" {
		return "active"
	}
	return "active:" + s.jobID
}
func (s *Store) Close() error {
	e := s.db.Close()
	l := s.lock.Unlock()
	if e != nil {
		return e
	}
	return l
}
func (s *Store) Save(p Plan) error {
	expectedScope := s.jobID
	if expectedScope == "legacy" {
		expectedScope = ""
	}
	if p.JobID != expectedScope {
		return fmt.Errorf("The sending plan belongs to another job")
	}

	if e := ValidatePlan(p); e != nil {
		return e
	}
	p.Request.Config = p.Request.Config.Public()
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT OR IGNORE INTO plans(id,payload) VALUES(?,?)", p.ID, b); e != nil {
		return e
	}
	for i, m := range p.Messages {
		if _, e = tx.Exec("INSERT OR IGNORE INTO deliveries(plan_id,key,recipient,position) VALUES(?,?,?,?)", p.ID, m.Key, m.To, i); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", s.activeKey(), p.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Active() (*Plan, error) {
	var b []byte
	e := s.db.QueryRow("SELECT payload FROM plans WHERE id=(SELECT value FROM meta WHERE key=?)", s.activeKey()).Scan(&b)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var p Plan
	if e = json.Unmarshal(b, &p); e != nil {
		return nil, e
	}
	if e = ValidatePlan(p); e != nil {
		return nil, e
	}
	return &p, nil
}
func (s *Store) State() (State, error) {
	state := State{Deliveries: []Delivery{}}
	p, e := s.Active()
	if e != nil {
		return state, e
	}
	state.Plan = p
	if p == nil {
		return state, nil
	}
	if e = s.db.QueryRow("SELECT next_at FROM plans WHERE id=?", p.ID).Scan(&state.NextAt); e != nil {
		return state, e
	}
	rows, e := s.db.Query("SELECT key,recipient,status,detail,updated FROM deliveries WHERE plan_id=? ORDER BY position", p.ID)
	if e != nil {
		return state, e
	}
	defer rows.Close()
	for rows.Next() {
		var d Delivery
		if e = rows.Scan(&d.Key, &d.To, &d.Status, &d.Detail, &d.Updated); e != nil {
			return state, e
		}
		state.Deliveries = append(state.Deliveries, d)
	}
	return state, rows.Err()
}
func (s *Store) Claim(p Plan, m Message, now time.Time) (bool, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	var status string
	if e = tx.QueryRow("SELECT status FROM deliveries WHERE plan_id=? AND key=?", p.ID, m.Key).Scan(&status); e != nil {
		return false, e
	}
	if status != "pending" {
		return false, nil
	}
	// Reserve both identity and address for this exact message before any network I/O.

	for _, identity := range []string{m.Key, m.To} {
		token := globalReceipt(identity, m.Subject, m.Body)
		result, e := tx.Exec("INSERT OR IGNORE INTO receipts(token,plan_id) VALUES(?,?)", token, p.ID)
		if e != nil {
			return false, e
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			if _, e = tx.Exec("UPDATE deliveries SET status='blocked',detail='Sending this message to this contact has already been attempted',updated=? WHERE plan_id=? AND key=?", now.UTC().Format(time.RFC3339Nano), p.ID, m.Key); e != nil {
				return false, e
			}
			return false, tx.Commit()
		}
	}
	result, e := tx.Exec("UPDATE deliveries SET status='sending',updated=? WHERE plan_id=? AND key=? AND status='pending'", now.UTC().Format(time.RFC3339Nano), p.ID, m.Key)
	if e != nil {
		return false, e
	}
	n, _ := result.RowsAffected()
	if e = tx.Commit(); e != nil {
		return false, e
	}
	return n == 1, nil
}
func (s *Store) Finish(p Plan, m Message, result SendResult, now time.Time) error {
	if result.Status != "sent" && result.Status != "failed" && result.Status != "uncertain" {
		return fmt.Errorf("Invalid delivery status")
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.Exec("UPDATE deliveries SET status=?,detail=?,updated=? WHERE plan_id=? AND key=? AND status='sending'", result.Status, result.Detail, now.UTC().Format(time.RFC3339Nano), p.ID, m.Key)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return fmt.Errorf("Could not reliably save delivery status")
	}
	var count int
	if e = tx.QueryRow("SELECT count(*) FROM deliveries WHERE plan_id=? AND status IN ('sent','failed','uncertain')", p.ID).Scan(&count); e != nil {
		return e
	}
	delay := time.Duration(p.Request.Schedule.Seconds) * time.Second
	if count%p.Request.Schedule.BatchSize == 0 {
		batch := time.Duration(p.Request.Schedule.BatchMinutes) * time.Minute
		if batch > delay {
			delay = batch
		}
	}
	if _, e = tx.Exec("UPDATE plans SET next_at=? WHERE id=?", now.Add(delay).UTC().Format(time.RFC3339Nano), p.ID); e != nil {
		return e
	}
	return tx.Commit()
}

func globalReceipt(identity, subject, body string) string {
	return digest([]string{"global-v1", identity, subject, body})
}

// Preserve reservations written by releases that scoped receipts to the sender.
func (s *Store) migrateGlobalReceipts() error {
	var done string
	err := s.db.QueryRow("SELECT value FROM meta WHERE key='global-receipts-v1'").Scan(&done)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	rows, err := s.db.Query("SELECT payload FROM plans")
	if err != nil {
		return err
	}
	var plans []Plan
	for rows.Next() {
		var b []byte
		var p Plan
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(b, &p); err != nil {
			rows.Close()
			return err
		}
		if err = ValidatePlan(p); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range plans {
		sender, err := Address(p.Request.Config.Sender())
		if err != nil {
			return err
		}
		for _, m := range p.Messages {
			for _, identity := range []string{m.Key, m.To} {
				old := digest([]string{sender, identity, m.Subject, m.Body})
				if _, err = tx.Exec("INSERT OR IGNORE INTO receipts(token,plan_id) SELECT ?,plan_id FROM receipts WHERE token=?", globalReceipt(identity, m.Subject, m.Body), old); err != nil {
					return err
				}
			}
		}
	}
	if _, err = tx.Exec("INSERT INTO meta(key,value) VALUES('global-receipts-v1','1')"); err != nil {
		return err
	}
	return tx.Commit()
}
