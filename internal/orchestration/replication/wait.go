package replication

import (
	"context"
	"fmt"
	"time"
)

const minimumVerificationRetry = 30 * time.Second

// WaitProgress describes the stability period after a completed observation.
type WaitProgress struct {
	ObservedAt time.Time
	HealthyFor time.Duration
	Required   time.Duration
	Reset      bool
	Complete   bool
	Verifying  bool
	RetryAfter time.Duration
}

type stabilityPeriod struct {
	since    time.Time
	topology string
}

func (s *stabilityPeriod) observe(report Report, required time.Duration) WaitProgress {
	now := time.Now()
	progress := WaitProgress{ObservedAt: now.UTC(), Required: required}
	switch report.Status {
	case NotApplicable:
		progress.Complete = true
	case Healthy:
		if !s.since.IsZero() && s.topology != report.topology {
			progress.Reset = true
			s.since = now
		}
		if s.since.IsZero() {
			s.since = now
		}
		progress.HealthyFor = now.Sub(s.since)
		progress.Complete = progress.HealthyFor >= required
	default:
		progress.Reset = !s.since.IsZero()
		s.since = time.Time{}
	}
	s.topology = report.topology
	return progress
}

// Wait polls until replication remains healthy for stableFor or the caller's deadline expires.
// Interrupted observations never replace the last completed report.
func Wait(ctx context.Context, check func(context.Context) Report, interval, stableFor time.Duration, observe func(Report, WaitProgress), verify func(context.Context, Report) Report) (Report, error) {
	if interval <= 0 || stableFor < 0 {
		return Report{}, fmt.Errorf("interval must be positive and stable-for cannot be negative")
	}
	var period stabilityPeriod
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
		progress := period.observe(report, stableFor)
		if progress.Complete && report.Status == Healthy && verify != nil {
			progress.Complete, progress.Verifying = false, true
			if observe != nil {
				observe(report, progress)
			}
			verified := verify(ctx, report)
			if err := ctx.Err(); err != nil {
				return report, fmt.Errorf("replication verification ended: %w", err)
			}
			report = verified
			progress.ObservedAt, progress.Verifying = time.Now().UTC(), false
			progress.Complete = report.Status == Healthy || report.Status == NotApplicable
			if !progress.Complete {
				period.since = time.Time{}
				progress.Reset, progress.HealthyFor = true, 0
				progress.RetryAfter = max(interval, minimumVerificationRetry)
			}
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
		if progress.RetryAfter > 0 {
			delay = progress.RetryAfter
		} else if report.Status == Healthy {
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
