package replication

import (
	"context"
	"fmt"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

type fakeKubernetes struct {
	client kubernetes.Interface
	exec   func(context.Context, string, string, string, []string) ([]byte, error)
	stream func(context.Context, string, string, string, []string, io.Writer) error
	calls  int
}

func (f *fakeKubernetes) ExecTo(ctx context.Context, namespace, pod, container string, command []string, output io.Writer) error {
	if f.stream != nil {
		return f.stream(ctx, namespace, pod, container, command, output)
	}
	data, err := f.Exec(ctx, namespace, pod, container, command)
	if err != nil {
		return err
	}
	_, err = output.Write(data)
	return err
}

func (f *fakeKubernetes) Clientset() kubernetes.Interface { return f.client }

func (f *fakeKubernetes) Exec(ctx context.Context, namespace, pod, container string, command []string) ([]byte, error) {
	f.calls++
	return f.exec(ctx, namespace, pod, container, command)
}

func testOptions() Options {
	return Options{Namespace: "test", Components: []string{"kafka"}}
}

func kafkaObjects() []runtime.Object {
	return databaseObjects("kafka", "kafka", "kafka", 2)
}

func databaseObjects(application, component, container string, replicas int32) []runtime.Object {
	labels := map[string]string{
		"app.kubernetes.io/name": application, "app.kubernetes.io/component": component, "app.kubernetes.io/instance": "arbitrary-release",
	}
	workload := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: component, Namespace: "test", UID: types.UID("sts-" + component), Generation: 1, ResourceVersion: "1", Labels: labels},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(replicas),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: container}}}},
		},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: replicas, ObservedGeneration: 1, CurrentRevision: "one", UpdateRevision: "one"},
	}
	objects := []runtime.Object{workload}
	for n := int32(0); n < replicas; n++ {
		name := fmt.Sprintf("%s-%d", component, n)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "test", UID: types.UID(name), ResourceVersion: "1", Labels: labels,
				OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: component, UID: workload.UID, Controller: ptr.To(true)}},
			},
			Spec: corev1.PodSpec{NodeName: fmt.Sprintf("node-%d", n), Containers: []corev1.Container{{Name: container}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			}},
		}
		objects = append(objects, pod)
	}
	return objects
}

func TestCheckerQueriesReadyPodsAndDoesNotMutateKubernetes(t *testing.T) {
	client := fake.NewSimpleClientset(kafkaObjects()...)
	kube := &fakeKubernetes{client: client, exec: func(ctx context.Context, namespace, pod, container string, command []string) ([]byte, error) {
		assert.Equal(t, "test", namespace)
		assert.Equal(t, "kafka-0", pod)
		assert.Equal(t, "kafka", container)
		assert.Equal(t, []string{"bash", "-ec", kafkaQuery, "replication-check", "--bootstrap-server", "localhost:9092", "--describe"}, command)
		_, deadline := ctx.Deadline()
		assert.True(t, deadline)
		return []byte(kafkaFixture()), nil
	}}
	probe, err := New(kube, testOptions())
	require.NoError(t, err)
	report := probe.Check(context.Background())
	require.Equal(t, Healthy, report.Status, report)
	assert.Equal(t, 1, kube.calls)
	for _, action := range client.Actions() {
		assert.Equal(t, "list", action.GetVerb())
	}
}

func TestCheckerRejectsIncompleteTopology(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]runtime.Object) []runtime.Object
	}{
		{"missing pod", func(objects []runtime.Object) []runtime.Object { return objects[:2] }},
		{"not ready", func(objects []runtime.Object) []runtime.Object {
			objects[1].(*corev1.Pod).Status.Conditions[0].Status = corev1.ConditionFalse
			return objects
		}},
		{"terminating", func(objects []runtime.Object) []runtime.Object {
			objects[1].(*corev1.Pod).DeletionTimestamp = ptr.To(metav1.Now())
			return objects
		}},
		{"wrong owner", func(objects []runtime.Object) []runtime.Object {
			objects[1].(*corev1.Pod).OwnerReferences[0].UID = "other"
			return objects
		}},
		{"wrong database label", func(objects []runtime.Object) []runtime.Object {
			objects[0].(*appsv1.StatefulSet).Labels = map[string]string{"app.kubernetes.io/name": "other", "app.kubernetes.io/component": "kafka"}
			return objects
		}},
		{"wrong component label", func(objects []runtime.Object) []runtime.Object {
			objects[0].(*appsv1.StatefulSet).Labels["app.kubernetes.io/component"] = "unrelated"
			return objects
		}},
		{"different namespace", func(objects []runtime.Object) []runtime.Object {
			for _, object := range objects {
				object.(metav1.Object).SetNamespace("other")
			}
			return objects
		}},
		{"rolling update", func(objects []runtime.Object) []runtime.Object {
			objects[0].(*appsv1.StatefulSet).Status.UpdateRevision = "two"
			return objects
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kube := &fakeKubernetes{client: fake.NewSimpleClientset(test.mutate(kafkaObjects())...)}
			probe, err := New(kube, testOptions())
			require.NoError(t, err)
			assert.Equal(t, Unknown, probe.Check(context.Background()).Status)
			assert.Zero(t, kube.calls)
		})
	}
}

func TestHDFSDiscoveryDistinguishesSecondaryNameNode(t *testing.T) {
	objects := databaseObjects("hbase", "hdfs-nn", "namenode", 1)
	objects = append(objects, databaseObjects("hbase", "hdfs-snn", "namenode", 1)...)
	objects = append(objects, databaseObjects("hbase", "hdfs-dn", "datanode", 3)...)
	kube := &fakeKubernetes{client: fake.NewSimpleClientset(objects...), exec: func(_ context.Context, namespace, pod, container string, _ []string) ([]byte, error) {
		assert.Equal(t, "test", namespace)
		assert.Equal(t, "hdfs-nn-0", pod)
		assert.Equal(t, "namenode", container)
		return hdfsFixture(t, func(map[string]any) {}), nil
	}}
	options := testOptions()
	options.Components = []string{"hdfs"}
	probe, err := New(kube, options)
	require.NoError(t, err)
	report := probe.Check(context.Background())
	assert.Equal(t, Healthy, report.Status, report)
	assert.Equal(t, 1, kube.calls)
}

func TestDiscoveryDoesNotRequireHelmReleaseLabel(t *testing.T) {
	objects := kafkaObjects()
	for _, object := range objects {
		delete(object.(metav1.Object).GetLabels(), "app.kubernetes.io/instance")
	}
	kube := &fakeKubernetes{client: fake.NewSimpleClientset(objects...), exec: func(context.Context, string, string, string, []string) ([]byte, error) {
		return []byte(kafkaFixture()), nil
	}}
	probe, err := New(kube, testOptions())
	require.NoError(t, err)
	assert.Equal(t, Healthy, probe.Check(context.Background()).Status)
}

func TestCheckerRejectsMembershipChangeDuringQueries(t *testing.T) {
	client := fake.NewSimpleClientset(kafkaObjects()...)
	kube := &fakeKubernetes{client: client, exec: func(ctx context.Context, namespace, pod, _ string, _ []string) ([]byte, error) {
		current, err := client.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
		require.NoError(t, err)
		current.UID = "replacement-pod"
		_, err = client.CoreV1().Pods(namespace).Update(ctx, current, metav1.UpdateOptions{})
		require.NoError(t, err)
		return []byte(kafkaFixture()), nil
	}}
	probe, err := New(kube, testOptions())
	require.NoError(t, err)
	report := probe.Check(context.Background())
	assert.Equal(t, Unknown, report.Status)
	assert.Contains(t, report.Checks[0].Messages, "database topology, readiness or runtime changed during the checks; repeat the observation")
}

func TestQueryFailureCannotPass(t *testing.T) {
	kube := &fakeKubernetes{client: fake.NewSimpleClientset(kafkaObjects()...), exec: func(context.Context, string, string, string, []string) ([]byte, error) {
		return nil, fmt.Errorf("query not authorized")
	}}
	probe, err := New(kube, testOptions())
	require.NoError(t, err)
	assert.Equal(t, Unknown, probe.Check(context.Background()).Status)
}

func TestOrdinaryProbeRespectsInternalAndOverallDeadlines(t *testing.T) {
	for _, overall := range []time.Duration{5 * time.Second, 2 * time.Minute} {
		t.Run(overall.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), overall)
				defer cancel()
				kube := &fakeKubernetes{exec: func(ctx context.Context, _, _, _ string, _ []string) ([]byte, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}}
				probe, err := New(kube, testOptions())
				require.NoError(t, err)
				start := time.Now()
				_, err = probe.query(ctx, "kafka-0", "kafka", nil)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Equal(t, min(overall, 30*time.Second), time.Since(start))
				if overall > 30*time.Second {
					require.NoError(t, ctx.Err(), "a stalled probe must leave time for further observations")
				}
			})
		})
	}
}

func TestInvalidScopeRejected(t *testing.T) {
	for _, components := range [][]string{nil, {"kafka", "kafka"}, {"not-a-store"}} {
		options := testOptions()
		options.Components = components
		_, err := New(nil, options)
		require.Error(t, err)
	}
}

func TestCancellationStopsRemainingQueriesAndDiscovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := fake.NewSimpleClientset(kafkaObjects()...)
	kube := &fakeKubernetes{client: client, exec: func(context.Context, string, string, string, []string) ([]byte, error) {
		cancel()
		return nil, fmt.Errorf("aborted request with query URL")
	}}
	options := testOptions()
	options.Components = []string{"kafka", "zookeeper", "clickhouse"}
	probe, err := New(kube, options)
	require.NoError(t, err)
	report := probe.Check(ctx)
	assert.Equal(t, Unknown, report.Status)
	require.Len(t, report.Checks, 1)
	assert.Equal(t, []string{"context canceled"}, report.Checks[0].Messages)
	assert.Equal(t, 1, kube.calls)
	assert.Len(t, client.Actions(), 2, "no second discovery after cancellation")
}
