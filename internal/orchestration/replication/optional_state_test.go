package replication

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTransactionTopicAbsenceRequiresSuccessfulVerification(t *testing.T) {
	tests := []struct {
		presence string
		status   string
		failure  bool
	}{
		{"absent\n", Healthy, false},
		{"present\n", Unknown, false},
		{"unverified\n", Unknown, false},
		{"", Unknown, true},
	}
	for _, test := range tests {
		t.Run(test.presence, func(t *testing.T) {
			kube := &fakeKubernetes{client: fake.NewSimpleClientset(kafkaObjects()...), exec: func(_ context.Context, _, _, _ string, command []string) ([]byte, error) {
				if command[2] == kafkaQuery {
					return []byte(strings.ReplaceAll(kafkaFixture(), "__transaction_state", "nontransactional-topic")), nil
				}
				assert.Equal(t, transactionTopicQuery, command[2])
				if test.failure {
					return nil, fmt.Errorf("cannot query metadata")
				}
				return []byte(test.presence), nil
			}}
			probe, err := New(kube, testOptions())
			require.NoError(t, err)
			report := probe.Check(context.Background())
			assert.Equal(t, test.status, report.Status, report)
			assert.Equal(t, 2, kube.calls)
			if test.status == Healthy {
				assert.Contains(t, strings.Join(report.Checks[0].Messages, " "), "not applicable")
			} else {
				assert.NotContains(t, strings.Join(report.Checks[0].Messages, " "), "is absent")
			}
		})
	}
}

func TestUnverifiedTransactionTopicPreservesPartitionProblems(t *testing.T) {
	for _, presence := range []string{"absent\n", "present\n", "unverified\n", ""} {
		t.Run(presence, func(t *testing.T) {
			kube := &fakeKubernetes{client: fake.NewSimpleClientset(kafkaObjects()...), exec: func(_ context.Context, _, _, _ string, command []string) ([]byte, error) {
				if command[2] == kafkaQuery {
					data := strings.ReplaceAll(kafkaFixture(), "__transaction_state", "nontransactional-topic")
					return []byte(strings.Replace(data, "Isr: 1,0", "Isr: 0", 1)), nil
				}
				if presence == "" {
					return nil, fmt.Errorf("query failed")
				}
				return []byte(presence), nil
			}}
			probe, err := New(kube, testOptions())
			require.NoError(t, err)
			report := probe.Check(context.Background())
			messages := strings.Join(report.Checks[0].Messages, "; ")
			assert.Contains(t, messages, "events partition 0: assigned replicas are not all in sync")
			if presence == "absent\n" {
				assert.Equal(t, Degraded, report.Status)
				assert.Contains(t, messages, "is absent")
			} else {
				assert.Equal(t, Unknown, report.Status)
				assert.Contains(t, messages, "absence could not be verified")
				assert.NotContains(t, messages, "is absent")
			}
		})
	}
}

func TestZeroLogPointersRequireAnEmptyKeeperLog(t *testing.T) {
	tests := []struct {
		name, evidence, status string
		pending, readonly      int
	}{
		{"empty log", `{"data":[{"entries":"0"}]}`, Healthy, 0, 0},
		{"first log entry still unprocessed", `{"data":[{"entries":"1"}]}`, Degraded, 0, 0},
		{"no evidence", `{"data":[]}`, Unknown, 0, 0},
		{"missing count", `{"data":[{}]}`, Unknown, 0, 0},
		{"invalid count", `{"data":[{"entries":-1}]}`, Unknown, 0, 0},
		{"query failure", "", Unknown, 0, 0},
		{"pending queue still fails", `{"data":[{"entries":0}]}`, Degraded, 1, 0},
		{"read only still fails", `{"data":[{"entries":0}]}`, Degraded, 0, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects := databaseObjects("clickhouse", "clickhouse", "clickhouse", 2)
			kube := &fakeKubernetes{client: fake.NewSimpleClientset(objects...), exec: func(_ context.Context, _, pod, _ string, command []string) ([]byte, error) {
				if command[4] == clickhouseSQL {
					row := clickhouseFixture()
					row["log_pointer"], row["log_max_index"] = "0", "0"
					row["replica_name"], row["pending_data_tasks"], row["is_readonly"] = pod, test.pending, test.readonly
					return encode(t, map[string]any{"data": []any{row}}), nil
				}
				assert.Equal(t, clickhouseEmptyLogSQL, command[4])
				assert.Equal(t, "--param_log_path=/clickhouse/tables/shard0/traces/log", command[5])
				if test.evidence == "" {
					return nil, fmt.Errorf("Keeper query denied")
				}
				return []byte(test.evidence), nil
			}}
			options := testOptions()
			options.Components = []string{"clickhouse"}
			probe, err := New(kube, options)
			require.NoError(t, err)
			report := probe.Check(context.Background())
			assert.Equal(t, test.status, report.Status, report)
			if test.status != Unknown {
				assert.Equal(t, 4, kube.calls, "each replica's empty log is verified")
			}
		})
	}
}
