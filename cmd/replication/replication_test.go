package replication

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
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
	}, &flags{wait: true, interval: time.Millisecond}, &stderr)
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
			if test.expected == checker.Healthy {
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
