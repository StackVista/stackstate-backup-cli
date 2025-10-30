package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ContainerLog holds logs from a single container
type ContainerLog struct {
	Name   string // Container name
	IsInit bool   // True if this is an init container
	Logs   string // Container logs
}

// PodLogs holds logs from all containers in a pod
type PodLogs struct {
	PodName       string
	ContainerLogs []ContainerLog
}

// GetJobLogs retrieves logs from all pods belonging to a job
// Returns a slice of PodLogs, one for each pod in the job
func (c *Client) GetJobLogs(namespace, jobName string) ([]PodLogs, error) {
	ctx := context.Background()

	// Find pods for this job
	podList, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods found for job %s", jobName)
	}

	// Collect logs from each pod
	var allPodLogs []PodLogs
	for _, pod := range podList.Items {
		podLogs, err := c.GetPodLogs(namespace, pod.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get logs for pod %s: %w", pod.Name, err)
		}
		allPodLogs = append(allPodLogs, podLogs)
	}

	return allPodLogs, nil
}

// GetPodLogs retrieves logs from all containers in a specific pod
func (c *Client) GetPodLogs(namespace, podName string) (PodLogs, error) {
	ctx := context.Background()

	// Get pod to access its container list
	pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return PodLogs{}, fmt.Errorf("failed to get pod: %w", err)
	}

	result := PodLogs{
		PodName:       podName,
		ContainerLogs: make([]ContainerLog, 0),
	}

	// Get logs from init containers
	for _, container := range pod.Spec.InitContainers {
		logs, err := c.getContainerLogs(namespace, podName, container.Name)
		if err != nil {
			// Don't fail entirely if one container's logs are unavailable
			logs = fmt.Sprintf("Error fetching logs: %v", err)
		}
		result.ContainerLogs = append(result.ContainerLogs, ContainerLog{
			Name:   container.Name,
			IsInit: true,
			Logs:   logs,
		})
	}

	// Get logs from main containers
	for _, container := range pod.Spec.Containers {
		logs, err := c.getContainerLogs(namespace, podName, container.Name)
		if err != nil {
			// Don't fail entirely if one container's logs are unavailable
			logs = fmt.Sprintf("Error fetching logs: %v", err)
		}
		result.ContainerLogs = append(result.ContainerLogs, ContainerLog{
			Name:   container.Name,
			IsInit: false,
			Logs:   logs,
		})
	}

	return result, nil
}

// getContainerLogs retrieves logs from a specific container in a pod
func (c *Client) getContainerLogs(namespace, podName, containerName string) (string, error) {
	ctx := context.Background()

	req := c.clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
	})

	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
