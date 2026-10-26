package outreach

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"outreach/internal/diagnostics"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func createPreparedJob(t *testing.T, jobs *Jobs, name, address string) (*Job, Request, string) {
	t.Helper()
	view, err := jobs.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	job, _ := jobs.Get(view.ID)
	req := validRequest()
	req.Config.Username = address
	if _, err = job.Session.Import(fixture(t), "synthetic.csv", false); err != nil {
		t.Fatal(err)
	}
	if err = jobs.SaveDraft(job, name, req); err != nil {
		t.Fatal(err)
	}
	if err = job.Session.Check(context.Background(), req.Config); err != nil {
		t.Fatal(err)
	}
	token, err := job.Session.Test(context.Background(), req, 0, "test@example.org")
	if err != nil {
		t.Fatal(err)
	}
	return job, req, token
}
func TestJobsParallelIsolationAndAccountExclusion(t *testing.T) {
	sender := &fakeSender{}
	jobs, err := NewJobs(testStore(t), sender, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, a, ta := createPreparedJob(t, jobs, "Alpha", "a@example.com")
	second, b, tb := createPreparedJob(t, jobs, "Beta", "b@example.com")
	same, c, tc := createPreparedJob(t, jobs, "Other A", "A@EXAMPLE.COM")
	// A distinct message is intentional; identical outreach is globally reserved.
	b.Body += " Beta"
	second.Session.Check(context.Background(), b.Config)
	tb, err = second.Session.Test(context.Background(), b, 0, "test@example.org")
	if err != nil {
		t.Fatal(err)
	}
	sender.entered = make(chan struct{}, 10)
	sender.release = make(chan struct{})
	if err = jobs.Start(first, a, ta); err != nil {
		t.Fatal(err)
	}
	if err = jobs.Start(second, b, tb); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-sender.entered:
		case <-time.After(time.Second):
			t.Fatal("jobs did not overlap")
		}
	}
	if err = jobs.Start(same, c, tc); err == nil {
		t.Fatal("same sender ran concurrently")
	}
	views, err := jobs.Views()
	if err != nil {
		t.Fatal(err)
	}
	if !views[0].State.Running || !views[1].State.Running || views[2].State.Running {
		t.Fatal("wrong job states")
	}
	if views[0].State.Plan.ID == views[1].State.Plan.ID || views[0].State.Plan.JobID != first.ID || views[1].State.Plan.JobID != second.ID {
		t.Fatal("queues not isolated")
	}
	first.Engine.Stop()
	close(sender.release)
	waitEngine(t, first.Engine)
	waitEngine(t, second.Engine)
	sa, _ := first.Engine.State()
	sb, _ := second.Engine.State()
	if sa.Deliveries[0].Status != "sent" || sa.Deliveries[1].Status != "pending" {
		t.Fatal("stop did not target first job", sa)
	}
	for _, d := range sb.Deliveries {
		if d.Status != "sent" {
			t.Fatal("other job was stopped", sb)
		}
	}
	jobs.Close()
}

func TestAccountRemainsLockedDuringBatchPauseUntilStopped(t *testing.T) {
	sender := &fakeSender{}
	jobs, err := NewJobs(testStore(t), sender, RealClock{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	first, a, _ := createPreparedJob(t, jobs, "Waiting", "a@example.org")
	second, b, tb := createPreparedJob(t, jobs, "Next", "A@EXAMPLE.ORG")
	a.Schedule = Schedule{Seconds: 1, BatchSize: 1, BatchMinutes: 1440}
	ta, err := first.Session.Test(context.Background(), a, 0, "test@example.org")
	if err != nil {
		t.Fatal(err)
	}
	before := sender.count()
	if err = jobs.Start(first, a, ta); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := first.Engine.State()
		if err != nil {
			t.Fatal(err)
		}
		if state.NextAt != "" && state.Deliveries[0].Status == "sent" {
			if !state.Running {
				t.Fatal("pause released the worker")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("batch pause not reached")
		}
		time.Sleep(time.Millisecond)
	}
	if err = jobs.Start(second, b, tb); err == nil {
		t.Fatal("second job started during batch pause")
	}
	if err = jobs.Delete(first.ID); err == nil {
		t.Fatal("waiting worker was deleted")
	}
	first.Engine.Close()
	if sender.count() != before+1 {
		t.Fatal("stop sent the next batch")
	}
	if err = jobs.Start(second, b, tb); err != nil {
		t.Fatal("stopped job kept the account locked", err)
	}
}
func TestConcurrentStartsSameAddressHaveOneWinner(t *testing.T) {
	sender := &fakeSender{}
	jobs, err := NewJobs(testStore(t), sender, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, a, ta := createPreparedJob(t, jobs, "A", "same@example.com")
	second, b, tb := createPreparedJob(t, jobs, "B", "SAME@example.com")
	sender.release = make(chan struct{})
	sender.entered = make(chan struct{}, 2)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, call := range []func() error{func() error { return jobs.Start(first, a, ta) }, func() error { return jobs.Start(second, b, tb) }} {
		wg.Add(1)
		go func(f func() error) { defer wg.Done(); results <- f() }(call)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("%d starts succeeded", success)
	}
	first.Engine.Stop()
	second.Engine.Stop()
	close(sender.release)
	jobs.Close()
}
func TestConcurrentJobsGloballyReserveIdenticalMessage(t *testing.T) {
	sender := &fakeSender{}
	jobs, err := NewJobs(testStore(t), sender, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, a, ta := createPreparedJob(t, jobs, "A", "a@example.com")
	second, b, tb := createPreparedJob(t, jobs, "B", "b@example.com")
	before := sender.count()
	var wg sync.WaitGroup
	for _, call := range []func() error{func() error { return jobs.Start(first, a, ta) }, func() error { return jobs.Start(second, b, tb) }} {
		wg.Add(1)
		go func(f func() error) {
			defer wg.Done()
			if err := f(); err != nil {
				t.Error(err)
			}
		}(call)
	}
	wg.Wait()
	waitEngine(t, first.Engine)
	waitEngine(t, second.Engine)
	if sender.count()-before != 3 {
		t.Fatalf("duplicate outreach across jobs: %d", sender.count()-before)
	}
	astate, _ := first.Engine.State()
	bstate, _ := second.Engine.State()
	sent, blocked := 0, 0
	for _, d := range append(astate.Deliveries, bstate.Deliveries...) {
		if d.Status == "sent" {
			sent++
		}
		if d.Status == "blocked" {
			blocked++
		}
	}
	if sent != 3 || blocked != 3 {
		t.Fatal(sent, blocked)
	}
}
func TestDraftRestartSecretsAndLegacyMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := samplePlan(t)
	if err = store.Save(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim(legacy, legacy.Messages[0], time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(legacy, legacy.Messages[0], SendResult{Status: "sent", Detail: "test"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	jobs, err := NewJobs(store, &fakeSender{}, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	views, err := jobs.Views()
	if err != nil || len(views) != 1 || views[0].ID != "legacy" || views[0].State.Plan.ID != legacy.ID || views[0].State.Deliveries[0].Status != "sent" {
		t.Fatal("legacy data not retained", err, views)
	}
	view, err := jobs.Create("Draft")
	if err != nil {
		t.Fatal(err)
	}
	job, _ := jobs.Get(view.ID)
	req := validRequest()
	req.Config.Password = "NEVER-PERSIST-JOB-PASSWORD"
	req.Body = "Draft text"
	if _, err = job.Session.Import(fixture(t), "list.csv", false); err != nil {
		t.Fatal(err)
	}
	if err = jobs.SaveDraft(job, "Saved job", req); err != nil {
		t.Fatal(err)
	}
	jobs.Close()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), req.Config.Password) {
		t.Fatal("password in database")
	}
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	jobs, err = NewJobs(store, &fakeSender{}, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	views, err = jobs.Views()
	if err != nil || len(views) != 2 {
		t.Fatal(err)
	}
	if views[1].Draft.Request.Config.Password != "" || views[1].Name != "Saved job" || views[1].Draft.Request.Body != "Draft text" || len(views[1].Draft.Report.Contacts) != 3 || views[1].State.Running {
		t.Fatal("draft not restored", views[1])
	}
	restored, _ := jobs.Get(view.ID)
	if _, err = restored.Session.Filter(true); err != nil {
		t.Fatal("raw CSV not restored", err)
	}
	if err = jobs.Start(restored, req, "forged"); err == nil {
		t.Fatal("restart retained approval")
	}
}
func TestLegacySenderReceiptsMigrateGlobally(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	p := samplePlan(t)
	if err = s.Save(p); err != nil {
		t.Fatal(err)
	}
	m := p.Messages[0]
	sender, _ := Address(p.Request.Config.Sender())
	if _, err = s.db.Exec("DELETE FROM meta WHERE key='global-receipts-v1'"); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{m.Key, m.To} {
		if _, err = s.db.Exec("INSERT INTO receipts(token,plan_id) VALUES(?,?)", digest([]string{sender, identity, m.Subject, m.Body}), p.ID); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	req := validRequest()
	req.Config.Username = "new-sender@example.com"
	other, err := Prepare(req, p.Report)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(other); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Claim(other, other.Messages[0], time.Now())
	if err != nil || claimed {
		t.Fatal("legacy reservation bypassed", err)
	}
	var count int
	s.db.QueryRow("SELECT count(*) FROM meta WHERE key='global-receipts-v1'").Scan(&count)
	if count != 1 {
		t.Fatal("migration marker")
	}
}
func TestPlanAndProofCannotCrossJobBoundaries(t *testing.T) {
	jobs, err := NewJobs(testStore(t), &fakeSender{}, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, req, token := createPreparedJob(t, jobs, "A", "same@example.com")
	b, _, _ := createPreparedJob(t, jobs, "B", "same@example.com")
	pa, err := a.Session.Preview(req)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := b.Session.Preview(req)
	if err != nil {
		t.Fatal(err)
	}
	if pa.ID == pb.ID || pa.Messages[0].MessageID == pb.Messages[0].MessageID {
		t.Fatal("job plan identity collision")
	}
	if err = jobs.Start(b, req, token); err == nil {
		t.Fatal("another job's test proof authorized send")
	}
	pa.JobID = b.ID
	if err = ValidatePlan(pa); err == nil {
		t.Fatal("job scope tampering accepted")
	}
	view, _ := jobs.Create("Blank")
	job, _ := jobs.Get(view.ID)
	_, release, err := jobs.Acquire(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, _, err = jobs.Acquire(job.ID); err == nil {
		t.Fatal("concurrent job operation allowed")
	}
	views, _ := jobs.Views()
	blob, _ := json.Marshal(views)
	if strings.Contains(string(blob), validRequest().Config.Password) {
		t.Fatal("public draft leaked credential")
	}
}

type jobFailureSender struct{ fakeSender }

func (s *jobFailureSender) Send(ctx context.Context, c Config, m Message) SendResult {
	result := s.fakeSender.Send(ctx, c, m)
	if c.Username == "fail@example.com" && !strings.HasPrefix(m.MessageID, "<test-") {
		return failedSend("failed", SMTPFailure("AUTH", c, fmt.Errorf("535 rejected"), false))
	}
	return result
}
func TestJobFailureDoesNotStopOtherWorker(t *testing.T) {
	sender := &jobFailureSender{}
	log, err := diagnostics.OpenLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := NewJobs(testStore(t), sender, newClock(), diagnostics.NewReporter(log), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, reqA, tokenA := createPreparedJob(t, jobs, "Fail", "fail@example.com")
	b, reqB, _ := createPreparedJob(t, jobs, "Success", "success@example.com")
	reqB.Body += " Different outreach"
	tokenB, err := b.Session.Test(context.Background(), reqB, 0, "test@example.org")
	if err != nil {
		t.Fatal(err)
	}
	if err = jobs.Start(a, reqA, tokenA); err != nil {
		t.Fatal(err)
	}
	if err = jobs.Start(b, reqB, tokenB); err != nil {
		t.Fatal(err)
	}
	waitEngine(t, a.Engine)
	waitEngine(t, b.Engine)
	stateA, _ := a.Engine.State()
	stateB, _ := b.Engine.State()
	if stateA.Problem == nil || !strings.Contains(stateA.Problem.Operation, a.ID) || stateA.Deliveries[0].Status != "failed" {
		t.Fatal("job failure lost", stateA)
	}
	if stateB.Problem != nil {
		t.Fatal("other job inherited error")
	}
	for _, d := range stateB.Deliveries {
		if d.Status != "sent" {
			t.Fatal("failure stopped another worker", stateB)
		}
	}
}
func TestScopedStoreRejectsAnotherJobQueue(t *testing.T) {
	store := testStore(t)
	p := scopePlan(samplePlan(t), "job-a")
	if err := store.ForJob("job-b").Save(p); err == nil {
		t.Fatal("cross-job queue accepted")
	}
	if err := store.ForJob("job-a").Save(p); err != nil {
		t.Fatal(err)
	}
	a, err := store.ForJob("job-a").State()
	if err != nil || a.Plan.ID != p.ID {
		t.Fatal(err)
	}
	b, err := store.ForJob("job-b").State()
	if err != nil || b.Plan != nil || len(b.Deliveries) != 0 {
		t.Fatal("state leaked across jobs", err)
	}
}

func TestBlankJobIsReusedAndDiscardedWithoutTouchingWork(t *testing.T) {
	store := testStore(t)
	jobs, err := NewJobs(store, &fakeSender{}, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := jobs.Create("Neuer Job")
	if err != nil {
		t.Fatal(err)
	}
	second, err := jobs.Create("Neuer Job")
	if err != nil || first.ID != second.ID || !second.Empty {
		t.Fatal("blank draft duplicated", err)
	}
	job, release, err := jobs.Acquire(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := blankDraft().Request
	req.Subject = "Keep this subject"
	if err = jobs.SaveDraft(job, "Neuer Job", req); err != nil {
		t.Fatal(err)
	}
	release()
	if err = jobs.DiscardEmpty(first.ID); err == nil {
		t.Fatal("modified draft discarded")
	}
	third, err := jobs.Create("Neuer Job")
	if err != nil || third.ID == first.ID {
		t.Fatal("modified draft reused", err)
	}
	if err = jobs.DiscardEmpty(third.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = jobs.Acquire(third.ID); err == nil {
		t.Fatal("discarded job accessible")
	}
	views, err := jobs.Views()
	if err != nil || len(views) != 1 || views[0].Draft.Request.Subject != req.Subject {
		t.Fatal("work lost", err)
	}
}
func TestDiscardProtectsChangedSettingsCSVAndHistoricalPlans(t *testing.T) {
	for _, kind := range []string{"name", "port", "schedule", "csv", "history"} {
		t.Run(kind, func(t *testing.T) {
			store := testStore(t)
			jobs, err := NewJobs(store, &fakeSender{}, newClock(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			view, err := jobs.Create("Neuer Job")
			if err != nil {
				t.Fatal(err)
			}
			job, _ := jobs.Get(view.ID)
			req := blankDraft().Request
			name := "Neuer Job"
			switch kind {
			case "name":
				name = "Named draft"
			case "port":
				req.Config.Port = 465
			case "schedule":
				req.Schedule.Seconds = 60
			case "csv":
				if _, err = job.Session.Import(fixture(t), "test.csv", false); err != nil {
					t.Fatal(err)
				}
			case "history":
				p := scopePlan(samplePlan(t), view.ID)
				if err = store.ForJob(view.ID).Save(p); err != nil {
					t.Fatal(err)
				}
				if _, err = store.db.Exec("DELETE FROM meta WHERE key=?", "active:"+view.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err = jobs.SaveDraft(job, name, req); err != nil {
				t.Fatal(err)
			}
			if err = jobs.DiscardEmpty(view.ID); err == nil {
				t.Fatal("nonempty job discarded")
			}
			next, err := jobs.Create("Neuer Job")
			if err != nil || next.ID == view.ID {
				t.Fatal("nonempty job reused", err)
			}
		})
	}
}
func TestConcurrentBlankCreationReusesOneDraft(t *testing.T) {
	jobs, err := NewJobs(testStore(t), &fakeSender{}, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := jobs.Create("Neuer Job")
			if e != nil {
				t.Error(e)
				return
			}
			ids <- v.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatal("parallel creation duplicated drafts", seen)
	}
}

func TestStartupPrunesOnlyUntouchedEmptyDrafts(t *testing.T) {
	store := testStore(t)
	jobs, err := NewJobs(store, &fakeSender{}, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(blankDraft())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"old-empty-1", "old-empty-2", "old-empty-3"} {
		if _, err = store.db.Exec("INSERT INTO jobs(id,name,draft) VALUES(?,?,?)", id, "Neuer Job", blob); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := jobs.Create("Mein Entwurf")
	if err != nil {
		t.Fatal(err)
	}
	protected, err := jobs.Create("Neuer Job")
	if err != nil {
		t.Fatal(err)
	}
	p := scopePlan(samplePlan(t), protected.ID)
	if err = store.ForJob(protected.ID).Save(p); err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{}
	restored, err := NewJobs(store, sender, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	views, err := restored.Views()
	if err != nil || len(views) != 2 {
		t.Fatal("wrong startup cleanup", len(views), err)
	}
	for _, v := range views {
		if v.ID != saved.ID && v.ID != protected.ID {
			t.Fatal("empty survived", v.ID)
		}
	}
	if sender.count() != 0 {
		t.Fatal("cleanup sent mail")
	}
}

func TestDeletePreservesHistoryAndDuplicateProtection(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy=", legacy), func(t *testing.T) {
			store := testStore(t)
			original := samplePlan(t)
			if legacy {
				if err := store.Save(original); err != nil {
					t.Fatal(err)
				}
			}
			jobs, err := NewJobs(store, &fakeSender{}, newClock(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			id := "legacy"
			if !legacy {
				v, err := jobs.Create("Zu löschen")
				if err != nil {
					t.Fatal(err)
				}
				id = v.ID
				original = scopePlan(original, id)
				if err := store.ForJob(id).Save(original); err != nil {
					t.Fatal(err)
				}
			}
			for i, status := range []string{"sent", "failed", "uncertain"} {
				m := original.Messages[i]
				if ok, err := store.Claim(original, m, time.Now()); err != nil || !ok {
					t.Fatal(ok, err)
				}
				if err := store.Finish(original, m, SendResult{Status: status}, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if err := jobs.Delete(id); err != nil {
				t.Fatal(err)
			}
			if _, _, err := jobs.Acquire(id); err == nil {
				t.Fatal("deleted job accessible")
			}
			restored, err := NewJobs(store, &fakeSender{}, newClock(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			views, err := restored.Views()
			if err != nil || len(views) != 0 {
				t.Fatal("deleted job returned", views, err)
			}
			for i, status := range []string{"sent", "failed", "uncertain"} {
				var actual string
				if err := store.db.QueryRow("SELECT status FROM deliveries WHERE plan_id=? AND key=?", original.ID, original.Messages[i].Key).Scan(&actual); err != nil || actual != status {
					t.Fatal("history lost", actual, err)
				}
			}
			next, err := restored.Create("Neu")
			if err != nil {
				t.Fatal(err)
			}
			p := scopePlan(samplePlan(t), next.ID)
			if err := store.ForJob(next.ID).Save(p); err != nil {
				t.Fatal(err)
			}
			for _, m := range p.Messages {
				if ok, err := store.Claim(p, m, time.Now()); err != nil || ok {
					t.Fatal("duplicate permitted", ok, err)
				}
			}
		})
	}
}

func TestDeleteRejectsBusyRunningAndFailedWrites(t *testing.T) {
	store := testStore(t)
	sender := &fakeSender{}
	jobs, err := NewJobs(store, sender, newClock(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, req, token := createPreparedJob(t, jobs, "Behalten", "sender@example.org")
	_, release, err := jobs.Acquire(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Delete(job.ID); err == nil {
		t.Fatal("deleted busy job")
	}
	release()
	sender.entered = make(chan struct{}, 1)
	sender.release = make(chan struct{})
	if err := jobs.Start(job, req, token); err != nil {
		t.Fatal(err)
	}
	<-sender.entered
	if err := jobs.Delete(job.ID); err == nil {
		t.Fatal("deleted running job")
	}
	job.Engine.Stop()
	if err := jobs.Delete(job.ID); err == nil {
		t.Fatal("deleted in-flight job after stop")
	}
	close(sender.release)
	waitEngine(t, job.Engine)
	if _, err := store.db.Exec(`CREATE TRIGGER prevent_delete BEFORE DELETE ON meta BEGIN SELECT RAISE(ABORT,'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Delete(job.ID); err == nil {
		t.Fatal("delete succeeded despite failed write")
	}
	if _, err := jobs.Get(job.ID); err != nil {
		t.Fatal("runtime entry lost", err)
	}
	views, err := jobs.Views()
	if err != nil || len(views) != 1 || views[0].State.Plan == nil {
		t.Fatal("transaction did not roll back", views, err)
	}
	if _, err := store.db.Exec("DROP TRIGGER prevent_delete"); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Delete(job.ID); err != nil {
		t.Fatal(err)
	}
}
