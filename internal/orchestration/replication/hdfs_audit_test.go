package replication

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHDFSAuditVerification(t *testing.T) {
	tests := []struct {
		name, status string
	}{
		{"healthy", Healthy}, {"truncated", Unknown}, {"query failure", Unknown},
		{"replication one", Degraded}, {"deadline", Unknown}, {"topology changed", Unknown}, {"health changed", Degraded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects := databaseObjects("hbase", "hdfs-nn", "namenode", 1)
			objects = append(objects, databaseObjects("hbase", "hdfs-dn", "datanode", 3)...)
			client := fake.NewSimpleClientset(objects...)
			audits := 0
			kube := &fakeKubernetes{client: client,
				exec: func(context.Context, string, string, string, []string) ([]byte, error) {
					return hdfsFixture(t, func(fields map[string]any) {
						if test.name == "health changed" && audits > 0 {
							fields["UnderReplicatedBlocks"] = 1
						}
					}), nil
				},
				stream: func(ctx context.Context, namespace, pod, container string, command []string, out io.Writer) error {
					audits++
					assert.Equal(t, "hdfs-nn-0", pod)
					assert.Equal(t, "namenode", container)
					assert.Equal(t, []string{"bash", "-ec", hdfsAuditQuery}, command)
					if test.name == "deadline" {
						<-ctx.Done()
						return ctx.Err()
					}
					data := fsckFixture()
					switch test.name {
					case "truncated":
						data = strings.Split(data, "Status:")[0]
					case "replication one":
						data = strings.Replace(data, "replication=2", "replication=1", 1)
					case "query failure":
						return fmt.Errorf("query failed")
					case "topology changed":
						p, err := client.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
						require.NoError(t, err)
						p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "namenode", RestartCount: 1}}
						_, err = client.CoreV1().Pods(namespace).Update(ctx, p, metav1.UpdateOptions{})
						require.NoError(t, err)
					}
					_, err := io.WriteString(out, data)
					return err
				},
			}
			options := testOptions()
			options.Components, options.HDFSAuditTimeout = []string{"hdfs"}, time.Second
			if test.name == "deadline" {
				options.HDFSAuditTimeout = 10 * time.Millisecond
			}
			probe, err := New(kube, options)
			require.NoError(t, err)
			before := probe.Check(context.Background())
			require.Equal(t, Healthy, before.Status)
			after := probe.Verify(context.Background(), before)
			assert.Equal(t, test.status, after.Status, after)
			assert.Equal(t, 1, audits)
			assert.Equal(t, Healthy, before.Checks[0].Status, "verification must not mutate the prior observation")
			if test.status == Healthy {
				assert.Contains(t, strings.Join(after.Checks[0].Messages, "; "), "completed block entries")
				assert.Equal(t, 2, kube.calls, "lightweight health is rechecked after the audit")
			}
		})
	}
}

func TestNoHDFSAuditForInapplicableOrUnhealthyChecks(t *testing.T) {
	probe, err := New(&fakeKubernetes{client: fake.NewSimpleClientset()}, testOptions())
	require.NoError(t, err)
	for _, report := range []Report{
		{Status: NotApplicable},
		{Status: Degraded, Checks: []Result{{Component: "hdfs", Status: Healthy}}},
		{Status: Healthy, Checks: []Result{{Component: "hdfs", Status: NotApplicable}}},
		{Status: Healthy, Checks: []Result{{Component: "kafka", Status: Healthy}}},
	} {
		assert.Equal(t, report, probe.Verify(context.Background(), report))
	}
}

func TestHDFSRequiresMinimumWriteReplication(t *testing.T) {
	data := string(hdfsFixture(t, func(map[string]any) {}))
	assert.Equal(t, Degraded, evaluateHDFS([]byte(strings.Replace(data, `"minimumReplication":2`, `"minimumReplication":1`, 1)), 3).Status)
	assert.Equal(t, Unknown, evaluateHDFS([]byte(strings.Replace(data, `"minimumReplication":2`, `"otherMinimum":2`, 1)), 3).Status)
}
