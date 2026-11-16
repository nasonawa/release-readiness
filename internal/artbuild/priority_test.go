package artbuild

import (
	"context"
	"github.com/quay/release-readiness/internal/syncstatus"
	"net/url"
	"slices"
	"testing"
	"time"
)

func TestARTForegroundPrecedesBoundedHistoricalWork(t *testing.T) {
	f := &fakeART{}
	h, store := syncWith(t, f)
	store.apps = []string{"fbc-quay-3-17", "fbc-quay-3-16", "quay-5-0"}
	h.Activity = func(context.Context) (map[string]bool, error) {
		return map[string]bool{"quay-3-17": false, "quay-3-16": true, "quay-5-0": true}, nil
	}
	h.BackfillInterval = time.Hour
	h.BackfillBudget = time.Second
	h.SyncOnce(t.Context())
	if len(f.searches) != 13 {
		t.Fatalf("searches=%d", len(f.searches))
	}
	for _, q := range f.searches[:6] {
		if q.Get("group") == "quay-3.17" {
			t.Fatal("historical stream entered foreground")
		}
	}
	if f.searches[6].Get("dateRange") != "2026-10-09 to 2026-10-09" {
		t.Fatal("history started before current days completed")
	}
	f.searches = nil
	h.SyncOnce(t.Context())
	if len(f.searches) != 6 {
		t.Fatalf("backfill was not paced: %d", len(f.searches))
	}
	// Rotate through all groups, including historical-only streams.
	h.lastBackfill = h.now().Add(-2 * time.Hour)
	h.SyncOnce(t.Context())
	h.lastBackfill = h.now().Add(-2 * time.Hour)
	h.SyncOnce(t.Context())
	if !slices.ContainsFunc(f.searches, func(q url.Values) bool { return q.Get("group") == "quay-3.17" }) {
		t.Fatal("history rotation starved a stream")
	}
}

func TestARTBackfillFailureDoesNotInvalidateCurrent(t *testing.T) {
	f := &fakeART{failDate: "2026-10-09 to 2026-10-09"}
	h, store := syncWith(t, f)
	h.Activity = func(context.Context) (map[string]bool, error) { return map[string]bool{"quay-3-17": true}, nil }
	h.BackfillInterval = time.Hour
	status := syncstatus.New()
	h.Status = status.Track("art-history", time.Minute)
	h.BackfillStatus = status.Track("art-backfill", 0)
	h.SyncOnce(t.Context())
	problems := status.Problems(time.Now())
	if len(problems) != 1 || problems[0].Source != "art-backfill" {
		t.Fatalf("wrong freshness: %+v", problems)
	}
	if slices.Contains(store.scanned, "quay-3.17 fbc 2026-10-09") {
		t.Fatal("failed historical day marked complete")
	}
}

func TestARTForegroundTimeoutIsNotSuccess(t *testing.T) {
	h, store := syncWith(t, &fakeART{})
	h.pace.Reset(time.Hour)
	h.Activity = func(context.Context) (map[string]bool, error) { return nil, nil }
	h.ForegroundBudget = 10 * time.Millisecond
	h.BackfillInterval = time.Hour
	status := syncstatus.New()
	h.Status = status.Track("art-history", time.Minute)
	h.SyncOnce(t.Context())
	if len(store.scanned) != 0 || len(status.Problems(time.Now())) != 1 || !h.lastBackfill.IsZero() {
		t.Fatal("timeout reported success or ran history")
	}
}

func TestARTBudgetPauseIsNotSourceFailure(t *testing.T) {
	f := &fakeART{slowDate: "2026-10-09 to 2026-10-09"}
	h, store := syncWith(t, f)
	h.Activity = func(context.Context) (map[string]bool, error) { return nil, nil }
	h.BackfillInterval = time.Hour
	h.BackfillBudget = 20 * time.Millisecond
	status := syncstatus.New()
	h.Status = status.Track("art-history", time.Minute)
	h.BackfillStatus = status.Track("art-backfill", 0)
	h.SyncOnce(t.Context())
	if p := status.Problems(time.Now()); len(p) != 0 {
		t.Fatalf("intentional pause marked unhealthy: %+v", p)
	}
	if slices.Contains(store.scanned, "quay-3.17 fbc 2026-10-09") {
		t.Fatal("partial day was marked complete")
	}
	for _, source := range status.Sources() {
		if source.Source == "art-backfill" && source.LastSuccess != nil {
			t.Fatal("incomplete backfill reported complete")
		}
	}
}
