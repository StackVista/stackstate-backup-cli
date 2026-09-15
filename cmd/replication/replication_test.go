package replication

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	checker "github.com/stackvista/stackstate-backup-cli/internal/orchestration/replication"
)

func TestJSONOutputIsSeparateFromProgress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	report, err := observe(context.Background(), func(context.Context) checker.Report {
		return checker.Report{Status: checker.Healthy, Checks: []checker.Result{
			{Component: "kafka", Status: checker.Healthy, Messages: []string{"all assigned replicas in sync"}},
		}}
	}, &flags{wait: true, interval: time.Millisecond}, &stderr, nil)
	require.NoError(t, err)
	require.NoError(t, writeReport(&stdout, "json", report))
	var decoded checker.Report
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	assert.Equal(t, checker.Healthy, decoded.Status)
	assert.Contains(t, stderr.String(), "kafka healthy")
}

func TestCheckValidatesBeforeConnecting(t *testing.T) {
	command := Cmd()
	command.SetArgs([]string{"check", "--namespace=test", "--components=kafka", "--output=invalid"})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	err := command.Execute()
	require.ErrorContains(t, err, "output must be table or json")
}

func TestHelpDoesNotRequireBackupConfiguration(t *testing.T) {
	command := Cmd()
	command.SetArgs([]string{"check", "--help"})
	var output bytes.Buffer
	command.SetOut(&output)
	require.NoError(t, command.Execute())
	assert.NotContains(t, output.String(), "--release")
	assert.NotContains(t, output.String(), "--secret")
	assert.NotContains(t, output.String(), "--configmap")
	assert.Contains(t, output.String(), "hdfs,elasticsearch,kafka,clickhouse,zookeeper")
}

func TestReportAndExitAgree(t *testing.T) {
	tests := []struct {
		name, status, expected string
		checkErr               error
	}{
		{"healthy", checker.Healthy, checker.Healthy, nil},
		{"not applicable", checker.NotApplicable, checker.NotApplicable, nil},
		{"degraded", checker.Degraded, checker.Degraded, nil},
		{"unknown", checker.Unknown, checker.Unknown, nil},
		{"timeout after healthy sample", checker.Healthy, checker.Unknown, context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := finishReport(&output, "json", checker.Report{Status: test.status}, test.checkErr)
			var report checker.Report
			require.NoError(t, json.Unmarshal(output.Bytes(), &report))
			assert.Equal(t, test.expected, report.Status)
			if test.expected == checker.Healthy || test.expected == checker.NotApplicable {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if test.checkErr != nil {
				require.ErrorIs(t, err, test.checkErr)
				assert.NotEmpty(t, report.Error)
			}
		})
	}
}

func TestWaitOutputShowsTimestampsAndStabilityProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var stderr bytes.Buffer
		report, err := observe(context.Background(), func(context.Context) checker.Report {
			return checker.Report{CheckedAt: time.Now().UTC(), Status: checker.Healthy,
				Checks: []checker.Result{{Component: "kafka", Status: checker.Healthy, Messages: []string{"all replicas in sync"}}}}
		}, &flags{wait: true, interval: 10 * time.Second, stableFor: 30 * time.Second, timeout: time.Minute}, &stderr, nil)
		require.NoError(t, err)
		assert.Equal(t, checker.Healthy, report.Status)
		for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
			assert.Regexp(t, `^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\] `, line)
		}
		assert.Contains(t, stderr.String(), "verifying stability: 0s/30s (30s remaining)")
		assert.Contains(t, stderr.String(), "verifying stability: 10s/30s (20s remaining)")
		assert.Contains(t, stderr.String(), "stability period satisfied (30s/30s)")
	})
}

func TestCancelledTableLabelsLastCompletedObservation(t *testing.T) {
	var stdout bytes.Buffer
	report := checker.Report{CheckedAt: time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC), Status: checker.Healthy,
		Checks: []checker.Result{{Component: "kafka", Status: checker.Healthy, Messages: []string{"all replicas in sync"}}}}
	err := finishReport(&stdout, "table", report, context.Canceled)
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, stdout.String(), "Replication result: unknown")
	assert.Contains(t, stdout.String(), "context canceled")
	assert.Contains(t, stdout.String(), "Last completed observation started: 2026-09-11T12:00:00Z")
	assert.Contains(t, stdout.String(), "all replicas in sync")
}

func TestCancellationBeforeFirstObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	report, err := observe(ctx, func(context.Context) checker.Report {
		cancel()
		return checker.Report{Namespace: "test", Checks: []checker.Result{{Messages: []string{"aborted request URL"}}}}
	}, &flags{wait: true, interval: time.Second}, &stderr, nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "test", report.Namespace)
	assert.Empty(t, report.Checks)
	assert.NotContains(t, stderr.String(), "aborted")
	require.ErrorIs(t, finishReport(&stdout, "table", report, err), context.Canceled)
	assert.Contains(t, stdout.String(), "No completed observation.")
}

func TestHealthyTopologyChangeExplainsStabilityReset(t *testing.T) {
	message := stabilityMessage(checker.Report{Status: checker.Healthy}, checker.WaitProgress{Reset: true, Required: 30 * time.Second})
	assert.Contains(t, message, "topology, readiness or runtime changed")
	assert.Contains(t, message, "stability period restarted (0s/30s)")
}

func TestSingleObservationRequiresFinalVerification(t *testing.T) {
	var progress bytes.Buffer
	calls := 0
	report, err := observe(context.Background(), func(context.Context) checker.Report {
		return checker.Report{Status: checker.Healthy}
	}, &flags{}, &progress, func(_ context.Context, report checker.Report) checker.Report {
		calls++
		report.Status = checker.Unknown
		return report
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	assert.Equal(t, checker.Unknown, report.Status)
	var output bytes.Buffer
	require.Error(t, finishReport(&output, "json", report, nil))
	assert.Contains(t, progress.String(), "Running final replication verification")
}

func TestAuditProgressDoesNotPrematurelyClaimSuccess(t *testing.T) {
	assert.Contains(t, stabilityMessage(checker.Report{Status: checker.Healthy},
		checker.WaitProgress{Verifying: true}), "running final replication verification")
	assert.Contains(t, stabilityMessage(checker.Report{Status: checker.Unknown},
		checker.WaitProgress{RetryAfter: 30 * time.Second}), "next observation in 30s")
}
