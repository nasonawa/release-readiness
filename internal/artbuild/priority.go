package artbuild

import (
	"context"
	"errors"
	"fmt"
	"github.com/quay/release-readiness/internal/syncstatus"
	"slices"
	"strings"
	"time"
)

// syncPrioritized reports current-release freshness before best-effort history.
// Both phases are bounded; partial days are never marked complete.
func (h *HistorySync) syncPrioritized(ctx context.Context) {
	started := time.Now()
	activity, err := h.Activity(ctx)
	if err != nil {
		h.logger.Warn("ART priority metadata unavailable; treating configured groups as current", "error", err)
		activity = nil
	}
	var active, historical []string
	for _, group := range h.Groups {
		app := strings.ReplaceAll(group, ".", "-")
		if current, known := activity[app]; !known || current {
			active = append(active, group)
		} else {
			historical = append(historical, group)
		}
	}
	if len(active) == 0 && len(historical) == 0 {
		h.Status.Report(errWaitingForStreams)
		h.logger.Info("ART waiting for group configuration")
		return
	}
	slices.Sort(active)
	slices.Sort(historical)
	if len(active) > 0 {
		offset := h.foregroundCursor % len(active)
		h.foregroundCursor++
		active = append(slices.Clone(active[offset:]), active[:offset]...)
	}
	h.logger.Info("ART scheduling", "foreground", active, "historical", historical)
	budget := h.ForegroundBudget
	if budget <= 0 {
		budget = 2 * time.Minute
	}
	current, cancel := context.WithTimeout(ctx, budget)
	var pass syncstatus.Pass
	scanned := 0
	for day := range refreshDays {
		for _, group := range active {
			if current.Err() != nil {
				break
			}
			err := h.scanAndMark(current, group, h.now().UTC().AddDate(0, 0, -day).Format(time.DateOnly))
			pass.Add(err)
			if err == nil {
				scanned++
			}
		}
	}
	pass.Add(current.Err())
	cancel()
	if len(active) > 0 {
		h.Status.Report(pass.Err())
		h.logger.Info("ART foreground synced", "duration_ms", time.Since(started).Milliseconds(), "days_scanned", scanned, "failed", pass.Err() != nil)
	} else {
		h.logger.Info("ART foreground idle", "reason", "only historical streams discovered")
	}
	if pass.Err() != nil || ctx.Err() != nil {
		return
	}
	if h.BackfillInterval <= 0 || (!h.lastBackfill.IsZero() && h.now().Sub(h.lastBackfill) < h.BackfillInterval) {
		return
	}
	groups := append(slices.Clone(active), historical...)
	slices.Sort(groups)
	if len(groups) == 0 {
		return
	}
	group := groups[h.backfillCursor%len(groups)]
	h.backfillCursor++
	budget = h.BackfillBudget
	if budget <= 0 {
		budget = 30 * time.Second
	}
	backfill, stop := context.WithTimeout(ctx, budget)
	started = time.Now()
	err = h.backfillGroup(backfill, group, slices.Contains(active, group))
	// Source metadata is best effort too; it cannot extend the history budget.
	if err == nil {
		var sources syncstatus.Pass
		h.backfillSources(backfill, &sources)
		err = errors.Join(sources.Err(), backfill.Err())
	}
	budgetExhausted := errors.Is(backfill.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	stop()
	h.lastBackfill = h.now()
	if budgetExhausted {
		h.logger.Info("ART backfill paused", "group", group, "duration_ms", time.Since(started).Milliseconds(), "reason", "time budget exhausted")
		return
	}
	h.BackfillStatus.Report(err)
	h.logger.Info("ART backfill synced", "group", group, "duration_ms", time.Since(started).Milliseconds(), "failed", err != nil)
	if err != nil {
		h.logger.Warn("ART backfill incomplete", "group", group, "error", err)
	}
}

func (h *HistorySync) scanAndMark(ctx context.Context, group, day string) error {
	if err := h.scanDay(ctx, group, day); err != nil {
		return fmt.Errorf("scan %s %s: %w", group, day, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return h.store.MarkArtHistoryDayScanned(ctx, group, TypeFBC, day, h.now())
}

func (h *HistorySync) backfillGroup(ctx context.Context, group string, active bool) error {
	scanned, err := h.store.ScannedArtHistoryDays(ctx, group, TypeFBC)
	if err != nil {
		return err
	}
	// Current streams already scanned the newest days in the foreground phase.
	first := 0
	if active {
		first = refreshDays
	}
	count := 0
	for day := first; day < historyDays && count < backfillPerPass; day++ {
		date := h.now().UTC().AddDate(0, 0, -day).Format(time.DateOnly)
		if day >= refreshDays && slices.Contains(scanned, date) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := h.scanAndMark(ctx, group, date); err != nil {
			return err
		}
		count++
	}
	return ctx.Err()
}
