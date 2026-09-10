package replication

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// Kubernetes is the read/query surface needed by the checker.
type Kubernetes interface {
	Clientset() kubernetes.Interface
	Exec(context.Context, string, string, string, []string) ([]byte, error)
}

type inventory struct {
	workloads []appsv1.StatefulSet
	pods      []corev1.Pod
}

type member struct {
	pod      corev1.Pod
	replicas int
}

func (c *Checker) discover(ctx context.Context) (inventory, error) {
	ctx, cancel := context.WithTimeout(ctx, c.options.RequestTimeout)
	defer cancel()
	options := metav1.ListOptions{LabelSelector: labels.Set{"app.kubernetes.io/instance": c.options.Release}.String()}
	sets, err := c.kube.Clientset().AppsV1().StatefulSets(c.options.Namespace).List(ctx, options)
	if err != nil {
		return inventory{}, fmt.Errorf("list release StatefulSets: %w", err)
	}
	pods, err := c.kube.Clientset().CoreV1().Pods(c.options.Namespace).List(ctx, options)
	if err != nil {
		return inventory{}, fmt.Errorf("list release pods: %w", err)
	}
	return inventory{workloads: sets.Items, pods: pods.Items}, nil
}

func hasContainer(containers []corev1.Container, name string) bool {
	return slices.ContainsFunc(containers, func(container corev1.Container) bool { return container.Name == name })
}

func (i inventory) members(container string) ([]member, error) {
	var members []member
	found := false
	for _, workload := range i.workloads {
		if !hasContainer(workload.Spec.Template.Spec.Containers, container) {
			continue
		}
		found = true
		pods, err := i.workloadPods(workload)
		if err != nil {
			return nil, err
		}
		for _, pod := range pods {
			members = append(members, member{pod: pod, replicas: int(*workload.Spec.Replicas)})
		}
	}
	if !found {
		return nil, fmt.Errorf("no chart-managed StatefulSet with container %q; check release, selected components and chart layout", container)
	}
	slices.SortFunc(members, func(a, b member) int {
		if a.pod.Name < b.pod.Name {
			return -1
		}
		if a.pod.Name > b.pod.Name {
			return 1
		}
		return 0
	})
	return members, nil
}

func (i inventory) workloadPods(workload appsv1.StatefulSet) ([]corev1.Pod, error) {
	if workload.Spec.Replicas == nil || *workload.Spec.Replicas < 1 {
		return nil, fmt.Errorf("%s has no desired replicas", workload.Name)
	}
	expected := *workload.Spec.Replicas
	if workload.DeletionTimestamp != nil || workload.Status.ObservedGeneration < workload.Generation ||
		workload.Status.ReadyReplicas != expected || workload.Status.CurrentRevision != workload.Status.UpdateRevision {
		return nil, fmt.Errorf("%s is not converged: %d/%d replicas ready", workload.Name, workload.Status.ReadyReplicas, expected)
	}
	var pods []corev1.Pod
	for _, pod := range i.pods {
		owner := metav1.GetControllerOf(&pod)
		if owner == nil || owner.UID != workload.UID || owner.Kind != "StatefulSet" {
			continue
		}
		if pod.DeletionTimestamp != nil || pod.Spec.NodeName == "" || !podReady(pod) {
			return nil, fmt.Errorf("%s is terminating, unscheduled or not Ready", pod.Name)
		}
		pods = append(pods, pod)
	}
	if len(pods) != int(expected) {
		return nil, fmt.Errorf("%s has %d/%d current pods", workload.Name, len(pods), expected)
	}
	return pods, nil
}

func podReady(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning && slices.ContainsFunc(pod.Status.Conditions, func(condition corev1.PodCondition) bool {
		return condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue
	})
}

func (i inventory) fingerprint() string {
	var entries []string
	for _, workload := range i.workloads {
		entries = append(entries, fmt.Sprintf("sts:%s:%s", workload.UID, workload.ResourceVersion))
	}
	for _, pod := range i.pods {
		entries = append(entries, fmt.Sprintf("pod:%s:%s", pod.UID, pod.ResourceVersion))
	}
	slices.Sort(entries)
	data, _ := json.Marshal(entries)
	return string(data)
}
