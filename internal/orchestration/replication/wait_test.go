package replication

import (
	"context"
	"testing"
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
	}, time.Millisecond, 0, nil)
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
	}, time.Millisecond, 0, nil)
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
	}, time.Millisecond, 5*time.Millisecond, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls, 4)
}

func TestWaitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, err := Wait(ctx, func(context.Context) Report {
		cancel()
		return Report{Status: Unknown}
	}, time.Hour, 0, nil)
	require.ErrorIs(t, err, context.Canceled)
}
