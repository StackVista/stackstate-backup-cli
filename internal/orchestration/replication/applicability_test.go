package replication

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func TestSingleReplicaConfiguration(t *testing.T) {
	tests := []struct {
		component, application, role, container string
	}{
		{"clickhouse", "clickhouse", "clickhouse", "clickhouse"},
		{"kafka", "kafka", "kafka", "kafka"},
		{"elasticsearch", "elasticsearch", "master", "elasticsearch"},
		{"zookeeper", "zookeeper", "zookeeper", "zookeeper"},
		{"hdfs", "hbase", "stackgraph", "stackgraph"},
	}
	for _, test := range tests {
		t.Run(test.component, func(t *testing.T) {
			objects := databaseObjects(test.application, test.role, test.container, 1)
			client := fake.NewSimpleClientset(objects...)
			kube := &fakeKubernetes{client: client}
			options := testOptions()
			options.Components = []string{test.component}
			probe, err := New(kube, options)
			require.NoError(t, err)
			report := probe.Check(context.Background())
			assert.Equal(t, NotApplicable, report.Status, report)
			assert.Zero(t, kube.calls)
			for _, action := range client.Actions() {
				assert.Equal(t, "list", action.GetVerb())
				assert.Contains(t, []string{"pods", "statefulsets"}, action.GetResource().Resource)
			}
		})
	}
}

func TestReplicationUsesConfiguredTopology(t *testing.T) {
	kube := &fakeKubernetes{
		client: fake.NewSimpleClientset(databaseObjects("clickhouse", "clickhouse", "clickhouse", 3)...),
		exec: func(_ context.Context, _, pod, _ string, _ []string) ([]byte, error) {
			row := clickhouseFixture()
			row["replica_name"], row["total_replicas"], row["active_replicas"] = pod, "3", "3"
			return encode(t, map[string]any{"data": []any{row}}), nil
		},
	}
	options := testOptions()
	options.Components = []string{"clickhouse"}
	probe, err := New(kube, options)
	require.NoError(t, err)
	report := probe.Check(context.Background())
	assert.Equal(t, Healthy, report.Status, report)
	assert.Equal(t, 3, kube.calls)
}

func TestMissingEvidenceCannotBecomeNotApplicable(t *testing.T) {
	tests := []struct {
		name   string
		change func([]runtime.Object) []runtime.Object
	}{
		{"missing workload", func(_ []runtime.Object) []runtime.Object { return nil }},
		{"missing pod", func(objects []runtime.Object) []runtime.Object { return objects[:1] }},
		{"not ready", func(objects []runtime.Object) []runtime.Object {
			objects[1].(*corev1.Pod).Status.Conditions[0].Status = corev1.ConditionFalse
			return objects
		}},
		{"one survivor of three", func(objects []runtime.Object) []runtime.Object {
			objects[0].(*appsv1.StatefulSet).Spec.Replicas = ptr.To(int32(3))
			return objects
		}},
		{"scaled to zero", func(objects []runtime.Object) []runtime.Object {
			objects[0].(*appsv1.StatefulSet).Spec.Replicas = ptr.To(int32(0))
			return objects
		}},
		{"unsupported container", func(objects []runtime.Object) []runtime.Object {
			objects[0].(*appsv1.StatefulSet).Spec.Template.Spec.Containers[0].Name = "custom"
			return objects
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects := test.change(databaseObjects("clickhouse", "clickhouse", "clickhouse", 1))
			kube := &fakeKubernetes{client: fake.NewSimpleClientset(objects...)}
			options := testOptions()
			options.Components = []string{"clickhouse"}
			probe, err := New(kube, options)
			require.NoError(t, err)
			assert.Equal(t, Unknown, probe.Check(context.Background()).Status)
			assert.Zero(t, kube.calls)
		})
	}
}

func TestNotApplicableAggregation(t *testing.T) {
	assert.Equal(t, NotApplicable, reportStatus([]Result{{Status: NotApplicable}}))
	assert.Equal(t, Healthy, reportStatus([]Result{{Status: Healthy}, {Status: NotApplicable}}))
	assert.Equal(t, Degraded, reportStatus([]Result{{Status: Degraded}, {Status: NotApplicable}}))
	assert.Equal(t, Unknown, reportStatus([]Result{{Status: Unknown}, {Status: NotApplicable}}))
	assert.Equal(t, Unknown, reportStatus(nil))
}
