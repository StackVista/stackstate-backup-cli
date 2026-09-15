package replication

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type workloadState struct {
	Name, Application, Role         string
	UID                             types.UID
	Generation                      int64
	Replicas                        *int32
	Observed, Deleting              bool
	ReadyReplicas                   int32
	CurrentRevision, UpdateRevision string
	Containers                      []string
}

type podState struct {
	Name, Node      string
	UID, Owner      types.UID
	Phase           corev1.PodPhase
	Deleting        bool
	Ready           corev1.ConditionStatus
	ReadyTransition metav1.Time
	Container       *containerState
}

type containerState struct {
	Name, ID, ImageID string
	Restarts          int32
	Ready             bool
	Started           *bool
	RunningSince      metav1.Time
}

func (i inventory) fingerprint(components []string) string {
	var entries []string
	containers := make(map[types.UID]string)
	for _, workload := range i.workloads {
		if !slices.Contains(components, workloadComponent(workload)) {
			continue
		}
		containers[workload.UID] = databaseContainer(workload)
		entries = append(entries, snapshotJSON(snapshotWorkload(workload)))
	}
	for _, pod := range i.pods {
		owner := metav1.GetControllerOf(&pod)
		if owner == nil || owner.Kind != "StatefulSet" {
			continue
		}
		container, selected := containers[owner.UID]
		if selected {
			entries = append(entries, snapshotJSON(snapshotPod(pod, owner.UID, container)))
		}
	}
	slices.Sort(entries)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(snapshotJSON(entries))))
}

func snapshotJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func snapshotWorkload(workload appsv1.StatefulSet) workloadState {
	state := workloadState{
		Name: workload.Name, UID: workload.UID, Generation: workload.Generation,
		Application: workload.Labels["app.kubernetes.io/name"], Role: workload.Labels["app.kubernetes.io/component"],
		Replicas: workload.Spec.Replicas, ReadyReplicas: workload.Status.ReadyReplicas,
		Observed: workload.Status.ObservedGeneration >= workload.Generation, Deleting: workload.DeletionTimestamp != nil,
		CurrentRevision: workload.Status.CurrentRevision, UpdateRevision: workload.Status.UpdateRevision,
	}
	for _, container := range workload.Spec.Template.Spec.Containers {
		state.Containers = append(state.Containers, container.Name)
	}
	slices.Sort(state.Containers)
	return state
}

func snapshotPod(pod corev1.Pod, owner types.UID, container string) podState {
	state := podState{Name: pod.Name, UID: pod.UID, Owner: owner, Node: pod.Spec.NodeName,
		Phase: pod.Status.Phase, Deleting: pod.DeletionTimestamp != nil}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			state.Ready, state.ReadyTransition = condition.Status, condition.LastTransitionTime
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != container {
			continue
		}
		state.Container = &containerState{
			Name: status.Name, ID: status.ContainerID, ImageID: status.ImageID,
			Restarts: status.RestartCount, Ready: status.Ready, Started: status.Started,
		}
		if status.State.Running != nil {
			state.Container.RunningSince = status.State.Running.StartedAt
		}
	}
	return state
}
