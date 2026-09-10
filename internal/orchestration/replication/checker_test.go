package replication

import (
	"context"
	"fmt"
	"testing"
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
	calls  int
}

func (f *fakeKubernetes) Clientset() kubernetes.Interface { return f.client }

func (f *fakeKubernetes) Exec(ctx context.Context, namespace, pod, container string, command []string) ([]byte, error) {
	f.calls++
	return f.exec(ctx, namespace, pod, container, command)
}

func testOptions() Options {
	return Options{Namespace: "test", Release: "observability", Components: []string{"kafka"}, RequestTimeout: time.Second, ElasticsearchScheme: "http"}
}

func kafkaObjects() []runtime.Object {
	labels := map[string]string{"app.kubernetes.io/instance": "observability"}
	workload := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "kafka", Namespace: "test", UID: "sts-kafka", Generation: 1, ResourceVersion: "1", Labels: labels},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(2)),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "kafka"}}}},
		},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: 2, ObservedGeneration: 1, CurrentRevision: "one", UpdateRevision: "one"},
	}
	objects := []runtime.Object{workload}
	for n := 0; n < 2; n++ {
		name := fmt.Sprintf("kafka-%d", n)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "test", UID: types.UID(name), ResourceVersion: "1", Labels: labels,
				OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "kafka", UID: workload.UID, Controller: ptr.To(true)}},
			},
			Spec: corev1.PodSpec{NodeName: fmt.Sprintf("node-%d", n), Containers: []corev1.Container{{Name: "kafka"}}},
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
		assert.Equal(t, []string{"kafka-topics.sh", "--bootstrap-server", "localhost:9092", "--describe"}, command)
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
		{"wrong release", func(objects []runtime.Object) []runtime.Object {
			objects[0].(*appsv1.StatefulSet).Labels = map[string]string{"app.kubernetes.io/instance": "other"}
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

func TestCheckerRejectsMembershipChangeDuringQueries(t *testing.T) {
	client := fake.NewSimpleClientset(kafkaObjects()...)
	kube := &fakeKubernetes{client: client, exec: func(ctx context.Context, namespace, pod, _ string, _ []string) ([]byte, error) {
		current, err := client.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
		require.NoError(t, err)
		current.ResourceVersion = "2"
		_, err = client.CoreV1().Pods(namespace).Update(ctx, current, metav1.UpdateOptions{})
		require.NoError(t, err)
		return []byte(kafkaFixture()), nil
	}}
	probe, err := New(kube, testOptions())
	require.NoError(t, err)
	report := probe.Check(context.Background())
	assert.Equal(t, Unknown, report.Status)
	assert.Contains(t, report.Checks[0].Messages, "Kubernetes membership or status changed during the checks; repeat the observation")
}

func TestQueryFailureCannotPass(t *testing.T) {
	kube := &fakeKubernetes{client: fake.NewSimpleClientset(kafkaObjects()...), exec: func(context.Context, string, string, string, []string) ([]byte, error) {
		return nil, fmt.Errorf("query not authorized")
	}}
	probe, err := New(kube, testOptions())
	require.NoError(t, err)
	assert.Equal(t, Unknown, probe.Check(context.Background()).Status)
}

func TestInvalidScopeRejected(t *testing.T) {
	for _, components := range [][]string{nil, {"kafka", "kafka"}, {"not-a-store"}} {
		options := testOptions()
		options.Components = components
		_, err := New(nil, options)
		require.Error(t, err)
	}
}
