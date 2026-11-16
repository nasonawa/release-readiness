package prow

import (
	"context"
	"github.com/quay/release-readiness/internal/syncstatus"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

type sippyMemory struct {
	activity     map[string]bool
	definitions  []JobDefinition
	runs         map[string]Run
	start        time.Time
	largestBatch int
}

func (m *sippyMemory) ProwApplicationActivity(context.Context) (map[string]bool, error) {
	return m.activity, nil
}
func (m *sippyMemory) JobInventory(context.Context) ([]JobDefinition, string, error) {
	return m.definitions, "revision", nil
}
func (m *sippyMemory) SaveJobInventory(context.Context, []JobDefinition, string) error { return nil }
func (m *sippyMemory) CandidateWindowStart(context.Context, string) (time.Time, error) {
	return m.start, nil
}
func (m *sippyMemory) ListProwRunsByApplication(_ context.Context, app string) ([]Run, error) {
	var out []Run
	for _, r := range m.runs {
		if r.Application == app {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *sippyMemory) UpsertProwRuns(_ context.Context, runs []*Run) error {
	m.largestBatch = max(m.largestBatch, len(runs))
	for _, r := range runs {
		m.runs[r.JobName+"/"+r.BuildID] = *r
	}
	return nil
}

func productionSippy(t *testing.T, jobs []experimentJob) (*SippySync, *experimentAPI, *sippyMemory) {
	t.Helper()
	a := newExperimentAPI(t, jobs)
	m := &sippyMemory{runs: map[string]Run{}}
	for _, j := range jobs {
		m.definitions = append(m.definitions, JobDefinition{Name: j.Name, Application: j.Application, Target: j.Target})
	}
	s := &SippySync{Client: NewSippyClient(a.server.URL), Artifacts: experimentSyncer(a, nil, jobs), Store: m, Workers: 4, Retention: 30 * 24 * time.Hour, Lookback: 24 * time.Hour, now: func() time.Time { return time.Date(2026, 11, 16, 5, 0, 0, 0, time.UTC) }}
	return s, a, m
}

func TestSippyProductionBatchesAndPersistentCache(t *testing.T) {
	s, a, m := productionSippy(t, experimentInventory(t))
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	counts := experimentCategories(a.takeCounts())
	if len(m.runs) != 126 || m.largestBatch > 20 || counts["sippy"] != 9 || counts["prowjob"] != 0 || counts["listing"] != 0 || counts["tested-images"] != 126 {
		t.Fatalf("runs=%d batch=%d counts=%v", len(m.runs), m.largestBatch, counts)
	}
	// Recreate the reader; completed evidence comes from the store, not an
	// in-process cache or high-water mark that disappears on restart.
	s.Client = NewSippyClient(a.server.URL)
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	counts = experimentCategories(a.takeCounts())
	for kind, n := range counts {
		if kind != "sippy" && n != 0 {
			t.Fatalf("cached GCS reads: %v", counts)
		}
	}
}

func TestSippyProductionWindowAndLateArtifacts(t *testing.T) {
	jobs := experimentInventory(t)[:1]
	s, a, m := productionSippy(t, jobs)
	rows := a.rows[jobs[0].Release]
	m.start = rows[0].Timestamp.Add(30 * time.Second)
	s.Lookback = 0
	path := jobs[0].prowJob().Prefix + rows[1].ID + "/artifacts/" + jobs[0].Target + "/quay-gather/artifacts/tested-images.json"
	artifact := a.objects[path]
	delete(a.objects, path)
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(m.runs) != 2 {
		t.Fatalf("window ingested %d runs", len(m.runs))
	}
	key := jobs[0].Name + "/" + rows[1].ID
	if m.runs[key].ArtifactState == ArtifactPresent {
		t.Fatal("missing identity marked present")
	}
	a.objects[path] = artifact
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if m.runs[key].ArtifactState != ArtifactPresent {
		t.Fatal("late artifact not retried")
	}
}

func TestSippyProductionPageFailureAndUntrustedIdentity(t *testing.T) {
	for _, bad := range []string{"page", "identity"} {
		t.Run(bad, func(t *testing.T) {
			jobs := experimentInventory(t)[:1]
			s, a, m := productionSippy(t, jobs)
			s.Client.PageSize = 1
			if bad == "page" {
				a.fail["/api/jobs/runs?page=1"] = http.StatusServiceUnavailable
			} else {
				a.rows[jobs[0].Release][0].URL = "https://example.invalid/credential"
			}
			if err := s.SyncOnce(t.Context()); err == nil {
				t.Fatal("accepted incomplete or untrusted response")
			}
			if len(m.runs) != 0 {
				t.Fatal("persisted partial stream")
			}
			counts := experimentCategories(a.takeCounts())
			if counts["tested-images"] != 0 || counts["finished"] != 0 {
				t.Fatalf("fetched unvalidated evidence %v", counts)
			}
		})
	}
}

func TestSippyLegacyRefreshFindsUnindexedPendingRun(t *testing.T) {
	jobs := experimentInventory(t)[:1]
	s, a, _ := productionSippy(t, jobs)
	legacy := &memStore{runs: map[string]*Run{}}
	s.Artifacts.store = legacy
	id := "2100388335968063999"
	dir := jobs[0].prowJob().Prefix + id + "/"
	a.lists[jobs[0].prowJob().Prefix] = append(a.lists[jobs[0].prowJob().Prefix], dir)
	start := s.clock().Add(-time.Minute)
	a.objects[dir+"prowjob.json"] = experimentJSON(t, map[string]any{"status": map[string]any{"state": "pending", "startTime": start, "url": "https://prow.ci.openshift.org/view/gcs/" + bucket + "/" + dir}})
	s.LegacyInterval = time.Hour
	s.lastLegacy = s.clock().Add(-2 * time.Hour)
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if legacy.runs[id] == nil || legacy.runs[id].State != "pending" {
		t.Fatal("unindexed pending run not discovered")
	}
	counts := experimentCategories(a.takeCounts())
	if counts["listing"] != 1 {
		t.Fatalf("legacy discovery counts=%v", counts)
	}
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if counts = experimentCategories(a.takeCounts()); counts["listing"] != 0 {
		t.Fatalf("legacy refresh repeated before interval: %v", counts)
	}
}

func TestUnreleasedFirstAndHistoryPacedAndRotated(t *testing.T) {
	jobs := experimentInventory(t)
	s, a, m := productionSippy(t, jobs)
	// Two known historical streams; unknown streams stay foreground.
	m.activity = map[string]bool{"quay-3-10": false, "quay-3-12": false, "quay-3-18": true}
	s.HistoryInterval = time.Hour
	s.HistoryBudget = time.Second
	getCalls := func() []string {
		a.mu.Lock()
		defer a.mu.Unlock()
		out := slices.Clone(a.releases)
		a.releases = nil
		return out
	}
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	calls := getCalls()
	if len(calls) != 8 || calls[0] != "quay-3.18" || calls[len(calls)-1] != "quay-3.10" || slices.Contains(calls, "quay-3.12") {
		t.Fatalf("foreground then one history: %v", calls)
	}
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	calls = getCalls()
	if len(calls) != 7 || slices.Contains(calls, "quay-3.10") || slices.Contains(calls, "quay-3.12") {
		t.Fatalf("history not paced: %v", calls)
	}
	s.lastHistory = s.clock().Add(-2 * time.Hour)
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	calls = getCalls()
	if calls[len(calls)-1] != "quay-3.12" {
		t.Fatalf("history did not rotate: %v", calls)
	}
}

func TestHistoryTimeoutDoesNotFailForeground(t *testing.T) {
	jobs := experimentInventory(t)[:1]
	s, _, m := productionSippy(t, jobs)
	m.activity = map[string]bool{jobs[0].Application: false}
	s.HistoryInterval = time.Hour
	s.HistoryBudget = 20 * time.Millisecond
	status := syncstatus.New()
	s.HistoryStatus = status.Track("prow-history", 0)
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	s.Client = NewSippyClient(slow.URL)
	start := time.Now()
	if err := s.SyncOnce(t.Context()); err != nil {
		t.Fatalf("history failed foreground: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("history exceeded budget")
	}
	problems := status.Problems(time.Now())
	if len(problems) != 1 || problems[0].Source != "prow-history" {
		t.Fatalf("missing history failure: %v", problems)
	}
}

func TestForegroundFailureSkipsHistory(t *testing.T) {
	s, a, m := productionSippy(t, experimentInventory(t))
	m.activity = map[string]bool{"quay-3-10": false}
	s.HistoryInterval = time.Hour
	a.fail["/api/jobs/runs"] = http.StatusServiceUnavailable
	if err := s.SyncOnce(t.Context()); err == nil {
		t.Fatal("foreground error ignored")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if slices.Contains(a.releases, "quay-3.10") {
		t.Fatal("history ran while foreground failed")
	}
}

func TestCompletionResultCaseAndUnknownRemainConservative(t *testing.T) {
	for _, result := range []string{"success", "failure", "error", "aborted", "SUCCESS", "unexpected"} {
		t.Run(result, func(t *testing.T) {
			jobs := experimentInventory(t)[:1]
			s, a, m := productionSippy(t, jobs)
			row := a.rows[jobs[0].Release][0]
			path := jobs[0].prowJob().Prefix + row.ID + "/finished.json"
			a.objects[path] = experimentJSON(t, finished{Timestamp: row.Timestamp.Add(time.Hour).Unix(), Result: result})
			err := s.SyncOnce(t.Context())
			if result == "unexpected" {
				if err == nil {
					t.Fatal("unknown terminal state accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			run := m.runs[row.Job+"/"+row.ID]
			if run.State != strings.ToLower(result) || run.CompletedAt == nil {
				t.Fatalf("wrong terminal result: %+v", run)
			}
		})
	}
}
