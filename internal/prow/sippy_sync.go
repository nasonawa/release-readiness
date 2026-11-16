package prow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
	"github.com/quay/release-readiness/internal/workpool"
)

type SippyStore interface {
	InventoryStore
	ProwApplicationActivity(context.Context) (map[string]bool, error)
	ListProwRunsByApplication(context.Context, string) ([]Run, error)
	UpsertProwRuns(context.Context, []*Run) error
	// CandidateWindowStart is the earliest known ART FBC build start of an
	// active release in this stream. Zero requests the retention fallback.
	CandidateWindowStart(context.Context, string) (time.Time, error)
}

// SippySync re-reads a bounded candidate window on every pass. This deliberately
// avoids a timestamp-only incremental cursor, which can miss late-indexed runs.
// Complete verified artifacts are cached persistently in prow_runs.
type SippySync struct {
	Client              *SippyClient
	Artifacts           *Syncer
	Store               SippyStore
	Activity            func(context.Context) (map[string]bool, error)
	WindowStart         func(context.Context, string) (time.Time, error)
	Status              *syncstatus.Source
	Workers             int
	Retention, Lookback time.Duration
	// LegacyInterval refreshes running/triggered jobs missing from Sippy.
	// Zero disables this additional GCS discovery. It is independently paced.
	LegacyInterval time.Duration
	lastLegacy     time.Time
	// History is best effort, after foreground work, one stream per interval.
	// Zero HistoryInterval disables history. Failures have separate freshness.
	HistoryInterval, HistoryBudget time.Duration
	HistoryStatus                  *syncstatus.Source
	lastHistory                    time.Time
	historyCursor                  int
	now                            func() time.Time
}

func (s *SippySync) clock() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func (s *SippySync) Run(ctx context.Context, interval time.Duration) {
	s.lastLegacy = s.clock()
	for {
		pass, cancel := context.WithTimeout(ctx, 10*time.Minute)
		err := s.SyncOnce(pass)
		s.Status.Report(err)
		if err != nil {
			slog.Warn("Prow foreground sync failed", "error", err)
		}
		cancel()
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *SippySync) SyncOnce(ctx context.Context) error {
	definitions, revision, err := s.Store.JobInventory(ctx)
	if err != nil {
		return err
	}
	if revision == "" || len(definitions) == 0 {
		return fmt.Errorf("awaiting complete periodic inventory")
	}
	groups := map[string][]JobDefinition{}
	for _, d := range definitions {
		if !d.Removed {
			groups[d.Application] = append(groups[d.Application], d)
		}
	}
	activityFn := s.Activity
	if activityFn == nil {
		activityFn = s.Store.ProwApplicationActivity
	}
	activity, err := activityFn(ctx)
	if err != nil {
		return err
	}
	var active, unknown, historical []string
	for app := range groups {
		if isActive, known := activity[app]; !known {
			unknown = append(unknown, app)
		} else if isActive {
			active = append(active, app)
		} else {
			historical = append(historical, app)
		}
	}
	sort.Strings(active)
	sort.Strings(historical)
	sort.Strings(unknown)
	active = append(active, unknown...)
	slog.Info("Prow scheduling", "foreground", active, "historical", historical)
	var pass syncstatus.Pass
	for _, app := range active {
		if ctx.Err() != nil {
			return errors.Join(pass.Err(), ctx.Err())
		}
		pass.Add(s.syncStream(ctx, app, groups[app]))
	}
	if s.LegacyInterval > 0 && (s.lastLegacy.IsZero() || s.clock().Sub(s.lastLegacy) >= s.LegacyInterval) {
		for _, app := range active {
			for _, d := range groups[app] {
				if ctx.Err() != nil {
					return errors.Join(pass.Err(), ctx.Err())
				}
				_, err := s.Artifacts.syncJob(ctx, d.Job())
				pass.Add(err)
			}
		}
		s.lastLegacy = s.clock()
	}
	// Do not spend time on history while current releases need a retry.
	if err := pass.Err(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.HistoryInterval <= 0 || len(historical) == 0 || (!s.lastHistory.IsZero() && s.clock().Sub(s.lastHistory) < s.HistoryInterval) {
		return nil
	}
	app := historical[s.historyCursor%len(historical)]
	s.historyCursor++
	budget := s.HistoryBudget
	if budget <= 0 {
		budget = 30 * time.Second
	}
	historyCtx, cancel := context.WithTimeout(ctx, budget)
	err = s.syncStream(historyCtx, app, groups[app])
	cancel()
	s.lastHistory = s.clock()
	s.HistoryStatus.Report(err)
	if err != nil {
		slog.Warn("Prow historical sync failed", "application", app, "error", err)
	}
	return nil
}

func (s *SippySync) syncStream(ctx context.Context, app string, definitions []JobDefinition) (err error) {
	started := time.Now()
	metrics := &requestMetrics{}
	ctx = context.WithValue(ctx, requestMetricsKey{}, metrics)
	var indexed, cached, deferred, persisted int
	var indexDuration time.Duration
	defer func() {
		slog.Info("Prow stream synced", "application", app, "duration_ms", time.Since(started).Milliseconds(), "sippy_ms", indexDuration.Milliseconds(), "sippy_pages", metrics.sippy.Load(), "gcs_requests", metrics.gcs.Load(), "indexed_runs", indexed, "cache_hits", cached, "retry_deferred", deferred, "persisted_runs", persisted, "failed", err != nil)
	}()

	parts := strings.Split(app, "-")
	if len(parts) != 3 || parts[0] != "quay" {
		return fmt.Errorf("unsupported Sippy application %q", app)
	}
	now := s.clock()
	retention := s.Retention
	if retention <= 0 || retention > 90*24*time.Hour {
		retention = 90 * 24 * time.Hour
	}
	from := now.Add(-retention)
	windowFn := s.WindowStart
	if windowFn == nil {
		windowFn = s.Store.CandidateWindowStart
	}
	start, err := windowFn(ctx, app)
	if err != nil {
		return err
	}
	if !start.IsZero() {
		candidateFrom := start.Add(-s.Lookback)
		if candidateFrom.After(from) && !candidateFrom.After(now) {
			from = candidateFrom
		}
	}
	known, err := s.Store.ListProwRunsByApplication(ctx, app)
	if err != nil {
		return err
	}
	cache := map[string]Run{}
	for _, r := range known {
		cache[r.JobName+"/"+r.BuildID] = r
	}
	indexStart := time.Now()
	rows, err := s.Client.Runs(ctx, "quay-"+parts[1]+"."+parts[2], from, now)
	indexDuration = time.Since(indexStart)
	indexed = len(rows)
	if err != nil {
		return fmt.Errorf("%s: %w", app, err)
	}
	jobs := map[string]JobDefinition{}
	for _, d := range definitions {
		jobs[d.Name] = d
	}
	seen := map[string]bool{}
	var pending []IndexedRun
	for _, row := range rows {
		if _, ok := jobs[row.Job]; !ok {
			continue
		}
		if err := validateIndexedRun(row); err != nil {
			return fmt.Errorf("%s: %w", app, err)
		}
		if row.Timestamp.Before(from) || row.Timestamp.After(now) {
			return fmt.Errorf("sippy returned run outside requested window")
		}
		key := row.Job + "/" + row.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		r := cache[key]
		if r.CompletedAt != nil && r.ArtifactState == ArtifactPresent && digest(r.CatalogRef) != "" {
			cached++
			continue
		}
		if !r.FetchedAt.IsZero() && !r.FetchedAt.After(now) && now.Sub(r.FetchedAt) < evidenceRetryDelay(r, now) {
			deferred++
			continue
		}
		pending = append(pending, row)
	}
	// Prefer recent evidence even if the index returns an unexpected ordering.
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].Timestamp.After(pending[j].Timestamp) })
	var batch []*Run
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := s.Store.UpsertProwRuns(ctx, batch)
		if err == nil {
			persisted += len(batch)
		}
		batch = nil
		return err
	}
	err = workpool.Run(ctx, pending, s.Workers, func(ctx context.Context, row IndexedRun) (*Run, error) {
		r, err := s.Artifacts.fetchIndexedRun(ctx, jobs[row.Job], row)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", row.Job, row.ID, err)
		}
		r.FetchedAt = s.clock()
		return r, nil
	}, func(row IndexedRun, r *Run) error {
		// A temporary missing finish file cannot erase persisted terminal data.
		if old := cache[row.Job+"/"+row.ID]; old.CompletedAt != nil && r.CompletedAt == nil {
			return nil
		}
		batch = append(batch, r)
		if len(batch) >= 20 {
			return flush()
		}
		return nil
	})
	return errors.Join(err, flush())
}

// Fetch caching is distinct from readiness: legacy evidence never acquires an
// exact identity merely because its next fetch has been deferred.
func evidenceRetryDelay(r Run, now time.Time) time.Duration {
	if r.CompletedAt == nil {
		return 0
	} // observe running/unknown state every pass
	if now.Sub(*r.CompletedAt) < 24*time.Hour {
		return 2 * time.Minute
	}
	if r.ArtifactState == ArtifactPresent || r.ArtifactState == ArtifactOLMCSVs {
		return 24 * time.Hour
	}
	return 6 * time.Hour
}
