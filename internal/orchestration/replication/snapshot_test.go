package replication

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func runningKafkaObjects() []runtime.Object {
	objects := kafkaObjects()
	for _, object := range objects {
		if pod, ok := object.(*corev1.Pod); ok {
			pod.Status.Conditions[0].LastTransitionTime = metav1.NewTime(time.Unix(100, 0))
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "kafka", ContainerID: "containerd://original", Ready: true, Started: ptr.To(true),
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Unix(90, 0))}}},
				{Name: "metrics", ContainerID: "containerd://metrics", Ready: true},
			}
		}
	}
	return objects
}

func TestChangesDuringQueries(t *testing.T) {
	tests := []struct {
		name   string
		status string
		change func(*appsv1.StatefulSet, *corev1.Pod)
	}{
		{"metadata and heartbeat", Healthy, func(sts *appsv1.StatefulSet, pod *corev1.Pod) {
			sts.Annotations = map[string]string{"housekeeping": "updated"}
			pod.Annotations = map[string]string{"housekeeping": "updated"}
			pod.Status.Conditions[0].LastProbeTime = metav1.NewTime(time.Unix(110, 0))
			pod.Status.Conditions[0].Message = "updated diagnostic"
		}},
		{"sidecar restart while pod stays ready", Healthy, func(_ *appsv1.StatefulSet, pod *corev1.Pod) {
			pod.Status.ContainerStatuses[1].RestartCount++
		}},
		{"ready condition", Unknown, func(_ *appsv1.StatefulSet, pod *corev1.Pod) {
			pod.Status.Conditions[0].Status = corev1.ConditionFalse
		}},
		{"readiness flap recovered", Unknown, func(_ *appsv1.StatefulSet, pod *corev1.Pod) {
			pod.Status.Conditions[0].LastTransitionTime = metav1.NewTime(time.Unix(110, 0))
		}},
		{"database restart", Unknown, func(_ *appsv1.StatefulSet, pod *corev1.Pod) {
			pod.Status.ContainerStatuses[0].RestartCount++
		}},
		{"container replacement", Unknown, func(_ *appsv1.StatefulSet, pod *corev1.Pod) {
			pod.Status.ContainerStatuses[0].ContainerID = "containerd://replacement"
		}},
		{"container start time", Unknown, func(_ *appsv1.StatefulSet, pod *corev1.Pod) {
			pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(time.Unix(110, 0))
		}},
		{"node assignment", Unknown, func(_ *appsv1.StatefulSet, pod *corev1.Pod) {
			pod.Spec.NodeName = "replacement-node"
		}},
		{"termination", Unknown, func(_ *appsv1.StatefulSet, pod *corev1.Pod) {
			pod.DeletionTimestamp = ptr.To(metav1.NewTime(time.Unix(110, 0)))
		}},
		{"ownership", Unknown, func(_ *appsv1.StatefulSet, pod *corev1.Pod) {
			pod.OwnerReferences[0].UID = "another-statefulset"
		}},
		{"scaling", Unknown, func(sts *appsv1.StatefulSet, _ *corev1.Pod) {
			sts.Spec.Replicas = ptr.To(int32(1))
		}},
		{"observed spec change", Unknown, func(sts *appsv1.StatefulSet, _ *corev1.Pod) {
			sts.Generation++
			sts.Status.ObservedGeneration++
		}},
		{"controller not converged", Unknown, func(sts *appsv1.StatefulSet, _ *corev1.Pod) {
			sts.Status.ObservedGeneration = 0
		}},
		{"workload readiness", Unknown, func(sts *appsv1.StatefulSet, _ *corev1.Pod) {
			sts.Status.ReadyReplicas--
		}},
		{"rollout", Unknown, func(sts *appsv1.StatefulSet, _ *corev1.Pod) {
			sts.Status.UpdateRevision = "next"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(runningKafkaObjects()...)
			kube := &fakeKubernetes{client: client, exec: func(ctx context.Context, namespace, podName, _ string, _ []string) ([]byte, error) {
				pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
				require.NoError(t, err)
				sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, "kafka", metav1.GetOptions{})
				require.NoError(t, err)
				sts.ResourceVersion, pod.ResourceVersion = "2", "2"
				test.change(sts, pod)
				_, err = client.CoreV1().Pods(namespace).Update(ctx, pod, metav1.UpdateOptions{})
				require.NoError(t, err)
				_, err = client.AppsV1().StatefulSets(namespace).Update(ctx, sts, metav1.UpdateOptions{})
				require.NoError(t, err)
				return []byte(kafkaFixture()), nil
			}}
			probe, err := New(kube, testOptions())
			require.NoError(t, err)
			assert.Equal(t, test.status, probe.Check(context.Background()).Status)
		})
	}
}

func TestSnapshotsIgnoreUnselectedWorkloadsAndListOrder(t *testing.T) {
	const otherDatabase = "zookeeper"
	objects := append(runningKafkaObjects(), databaseObjects(otherDatabase, otherDatabase, otherDatabase, 3)...)
	kube := &fakeKubernetes{client: fake.NewSimpleClientset(objects...)}
	probe, err := New(kube, testOptions())
	require.NoError(t, err)
	inventory, err := probe.discover(context.Background())
	require.NoError(t, err)
	kafkaBefore := inventory.fingerprint([]string{"kafka"})
	allBefore := inventory.fingerprint([]string{"kafka", otherDatabase})
	slices.Reverse(inventory.workloads)
	slices.Reverse(inventory.pods)
	assert.Equal(t, allBefore, inventory.fingerprint([]string{"kafka", otherDatabase}))
	for n := range inventory.pods {
		if inventory.pods[n].Labels["app.kubernetes.io/name"] == otherDatabase {
			inventory.pods[n].Status.Conditions[0].Status = corev1.ConditionFalse
		}
	}
	orphan := inventory.pods[len(inventory.pods)-1].DeepCopy()
	orphan.UID, orphan.OwnerReferences = "diagnostic-pod", nil
	inventory.pods = append(inventory.pods, *orphan)
	assert.Equal(t, kafkaBefore, inventory.fingerprint([]string{"kafka"}))
	assert.NotEqual(t, allBefore, inventory.fingerprint([]string{"kafka", otherDatabase}))
}

func TestWaitTracksMeaningfulChangesBetweenHealthyChecks(t *testing.T) {
	for _, restart := range []bool{false, true} {
		name := "metadata only"
		expected := 30 * time.Second
		if restart {
			name, expected = "database restart", 50*time.Second
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := fake.NewSimpleClientset(runningKafkaObjects()...)
				kube := &fakeKubernetes{client: client, exec: func(context.Context, string, string, string, []string) ([]byte, error) {
					return []byte(kafkaFixture()), nil
				}}
				probe, err := New(kube, testOptions())
				require.NoError(t, err)
				start := time.Now()
				calls := 0
				var progress []WaitProgress
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				report, err := Wait(ctx, func(ctx context.Context) Report {
					calls++
					if calls == 3 {
						pod, err := client.CoreV1().Pods("test").Get(ctx, "kafka-0", metav1.GetOptions{})
						require.NoError(t, err)
						pod.ResourceVersion = "2"
						pod.Annotations = map[string]string{"updated": "yes"}
						if restart {
							pod.Status.ContainerStatuses[0].RestartCount++
						}
						_, err = client.CoreV1().Pods("test").Update(ctx, pod, metav1.UpdateOptions{})
						require.NoError(t, err)
					}
					return probe.Check(ctx)
				}, 10*time.Second, 30*time.Second, func(report Report, state WaitProgress) {
					assert.Equal(t, Healthy, report.Status)
					progress = append(progress, state)
				})
				require.NoError(t, err)
				assert.Equal(t, Healthy, report.Status)
				assert.Equal(t, expected, time.Since(start))
				assert.Equal(t, restart, progress[2].Reset)
				assert.Equal(t, 30*time.Second, progress[len(progress)-1].HealthyFor)
			})
		})
	}
}
