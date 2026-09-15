package replication

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitContinuesThroughUnknownAndDegraded(t *testing.T) {
	statuses := []string{Unknown, Degraded, Healthy}
	calls := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report, err := Wait(ctx, func(context.Context) Report {
		status := statuses[calls]
		calls++
		return Report{Status: status}
	}, time.Millisecond, 0, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, Healthy, report.Status)
	assert.Equal(t, 3, calls)
}

func TestWaitHonorsDeadlineDuringQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := Wait(ctx, func(ctx context.Context) Report {
		<-ctx.Done()
		return Report{Status: Healthy}
	}, time.Millisecond, 0, nil, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWaitRequiresSustainedHealthyObservations(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Wait(ctx, func(context.Context) Report {
		calls++
		if calls == 2 {
			return Report{Status: Degraded}
		}
		return Report{Status: Healthy}
	}, time.Millisecond, 5*time.Millisecond, nil, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls, 4)
}

func TestWaitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, err := Wait(ctx, func(context.Context) Report {
		cancel()
		return Report{Status: Unknown}
	}, time.Hour, 0, nil, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNoApplicableReplicationChecksDoesNotWaitForever(t *testing.T) {
	calls := 0
	report, err := Wait(context.Background(), func(context.Context) Report {
		calls++
		return Report{Status: NotApplicable}
	}, time.Second, time.Minute, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, NotApplicable, report.Status)
	assert.Equal(t, 1, calls)
}

func TestWaitExitsAfterDefaultStabilityPeriodWithSlowChecks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var progress []WaitProgress
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		report, err := Wait(ctx, func(context.Context) Report {
			time.Sleep(2 * time.Second)
			return Report{Status: Healthy}
		}, 10*time.Second, 30*time.Second, func(_ Report, state WaitProgress) {
			progress = append(progress, state)
		}, nil)
		require.NoError(t, err)
		assert.Equal(t, Healthy, report.Status)
		require.Len(t, progress, 4)
		assert.Equal(t, time.Duration(0), progress[0].HealthyFor)
		assert.Equal(t, 12*time.Second, progress[1].HealthyFor)
		assert.Equal(t, 32*time.Second, progress[3].HealthyFor)
		assert.True(t, progress[3].Complete)
		assert.Equal(t, 34*time.Second, time.Since(start))
	})
}

func TestWaitRechecksAtStabilityDeadlineBeforeLongInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		calls := 0
		_, err := Wait(ctx, func(context.Context) Report {
			calls++
			return Report{Status: Healthy}
		}, time.Minute, 30*time.Second, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, 2, calls)
		assert.Equal(t, 30*time.Second, time.Since(start))
	})
}

func TestWaitResetsStabilityProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		var progress []WaitProgress
		_, err := Wait(context.Background(), func(context.Context) Report {
			calls++
			if calls == 3 {
				return Report{Status: Degraded}
			}
			return Report{Status: Healthy}
		}, 10*time.Second, 30*time.Second, func(_ Report, state WaitProgress) {
			progress = append(progress, state)
		}, nil)
		require.NoError(t, err)
		require.Len(t, progress, 7)
		assert.True(t, progress[2].Reset)
		assert.Zero(t, progress[3].HealthyFor)
		assert.Equal(t, 30*time.Second, progress[6].HealthyFor)
		assert.True(t, progress[6].Complete)
	})
}

func TestWaitCancellationPreservesLastCompletedObservation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls, observed := 0, 0
		lastCompleted := Report{Namespace: "test", CheckedAt: time.Now(), Status: Healthy,
			Checks: []Result{result("kafka", Healthy, "all partitions in sync")}}
		report, err := Wait(ctx, func(context.Context) Report {
			calls++
			if calls == 3 {
				cancel()
				return Report{Status: Unknown, Checks: []Result{result("kafka", Unknown, "aborted query URL")}}
			}
			return lastCompleted
		}, 10*time.Second, 30*time.Second, func(Report, WaitProgress) { observed++ }, nil)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, lastCompleted, report)
		assert.Equal(t, 2, observed)
	})
}

func TestWaitAuditsOnlyAfterStabilityAndPacesFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		audits := 0
		var auditTimes []time.Duration
		var progress []WaitProgress
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		report, err := Wait(ctx, func(context.Context) Report {
			return Report{Status: Healthy, topology: "unchanged"}
		}, 10*time.Second, 30*time.Second, func(_ Report, state WaitProgress) {
			progress = append(progress, state)
		}, func(_ context.Context, report Report) Report {
			audits++
			auditTimes = append(auditTimes, time.Since(start))
			if audits == 1 {
				report.Status = Degraded
			}
			return report
		})
		require.NoError(t, err)
		assert.Equal(t, Healthy, report.Status)
		assert.Equal(t, []time.Duration{30 * time.Second, 90 * time.Second}, auditTimes)
		verifying, retries := 0, 0
		for _, state := range progress {
			if state.Verifying {
				verifying++
				assert.False(t, state.Complete, "stability alone must not announce completion")
			}
			if state.RetryAfter > 0 {
				retries++
				assert.Equal(t, 30*time.Second, state.RetryAfter)
			}
		}
		assert.Equal(t, 2, verifying)
		assert.Equal(t, 1, retries)
		assert.True(t, progress[len(progress)-1].Complete)
	})
}

func TestWaitAuditCancellationPreservesLastObservation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		last := Report{Status: Healthy, Namespace: "test"}
		report, err := Wait(ctx, func(context.Context) Report { return last }, time.Second, 0, nil,
			func(ctx context.Context, _ Report) Report {
				<-ctx.Done()
				return Report{Status: Unknown, Error: "interrupted audit"}
			})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, last, report)
	})
}

func TestAuditFailureWithNoStabilityStillHasRetryDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		attempts := 0
		_, err := Wait(context.Background(), func(context.Context) Report { return Report{Status: Healthy} },
			time.Millisecond, 0, nil, func(_ context.Context, report Report) Report {
				attempts++
				if attempts == 1 {
					report.Status = Unknown
				}
				return report
			})
		require.NoError(t, err)
		assert.Equal(t, 2, attempts)
		assert.Equal(t, 30*time.Second, time.Since(start))
	})
}
