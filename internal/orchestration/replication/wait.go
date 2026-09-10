package replication

import (
	"context"
	"fmt"
	"time"
)

// Wait polls until replication remains healthy for stableFor or the caller's deadline expires.
// The observer receives every report and can write progress to stderr.
func Wait(ctx context.Context, check func(context.Context) Report, interval, stableFor time.Duration, observe func(Report)) (Report, error) {
	if interval <= 0 || stableFor < 0 {
		return Report{}, fmt.Errorf("interval must be positive and stable-for cannot be negative")
	}
	var healthySince time.Time
	var report Report
	for {
		if err := ctx.Err(); err != nil {
			return report, fmt.Errorf("replication wait ended: %w", err)
		}
		report = check(ctx)
		if observe != nil {
			observe(report)
		}
		if err := ctx.Err(); err != nil {
			return report, fmt.Errorf("replication wait ended: %w", err)
		}
		if report.Status != Healthy {
			healthySince = time.Time{}
		} else {
			if healthySince.IsZero() {
				healthySince = time.Now()
			}
			if time.Since(healthySince) >= stableFor {
				return report, nil
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return report, fmt.Errorf("replication wait ended: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
