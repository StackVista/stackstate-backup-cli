package replication

import (
	"context"
	"fmt"
	"time"
)

// WaitProgress describes the stability period after a completed observation.
type WaitProgress struct {
	ObservedAt time.Time
	HealthyFor time.Duration
	Required   time.Duration
	Reset      bool
	Complete   bool
}

// Wait polls until replication remains healthy for stableFor or the caller's deadline expires.
// Interrupted observations never replace the last completed report.
func Wait(ctx context.Context, check func(context.Context) Report, interval, stableFor time.Duration, observe func(Report, WaitProgress)) (Report, error) {
	if interval <= 0 || stableFor < 0 {
		return Report{}, fmt.Errorf("interval must be positive and stable-for cannot be negative")
	}
	var healthySince time.Time
	var report Report
	for {
		if err := ctx.Err(); err != nil {
			return report, fmt.Errorf("replication wait ended: %w", err)
		}
		next := check(ctx)
		if err := ctx.Err(); err != nil {
			if report.Namespace == "" {
				report.Namespace = next.Namespace
			}
			return report, fmt.Errorf("replication wait ended: %w", err)
		}
		report = next
		now := time.Now()
		progress := WaitProgress{ObservedAt: now.UTC(), Required: stableFor}
		switch report.Status {
		case NotApplicable:
			progress.Complete = true
		case Healthy:
			if healthySince.IsZero() {
				healthySince = now
			}
			progress.HealthyFor = now.Sub(healthySince)
			progress.Complete = progress.HealthyFor >= stableFor
		default:
			progress.Reset = !healthySince.IsZero()
			healthySince = time.Time{}
		}
		if observe != nil {
			observe(report, progress)
		}
		if err := ctx.Err(); err != nil {
			return report, fmt.Errorf("replication wait ended: %w", err)
		}
		if progress.Complete {
			return report, nil
		}
		delay := interval
		if report.Status == Healthy {
			delay = min(delay, stableFor-progress.HealthyFor)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return report, fmt.Errorf("replication wait ended: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
