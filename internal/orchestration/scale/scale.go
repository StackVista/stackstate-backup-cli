package scale

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

const (
	podTerminationCheckInterval = 2 * time.Second
	podTerminationTimeout       = 120 * time.Second
)

// ScaleDown scales down deployments matching the label selector and logs the results.
// It waits for all pods to terminate before returning.
//
//nolint:revive // Package name "scale" with function "ScaleDown" is intentionally verbose for clarity
func ScaleDown(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) ([]k8s.DeploymentScale, error) {
	log.Infof("Scaling down deployments (selector: %s)...", labelSelector)

	scaledDeployments, err := k8sClient.ScaleDownDeployments(namespace, labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to scale down deployments: %w", err)
	}

	if len(scaledDeployments) == 0 {
		log.Infof("No deployments found to scale down")
		return scaledDeployments, nil
	}

	log.Successf("Scaled down %d deployment(s):", len(scaledDeployments))
	for _, dep := range scaledDeployments {
		log.Infof("  - %s (replicas: %d -> 0)", dep.Name, dep.Replicas)
	}

	// Wait for pods to terminate
	if err := waitForPodsToTerminate(k8sClient, namespace, labelSelector, log); err != nil {
		return scaledDeployments, fmt.Errorf("failed waiting for pods to terminate: %w", err)
	}

	return scaledDeployments, nil
}

// waitForPodsToTerminate polls for pod termination until all pods matching the label selector are gone
func waitForPodsToTerminate(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) error {
	ctx := context.Background()
	ticker := time.NewTicker(podTerminationCheckInterval)
	defer ticker.Stop()

	timeout := time.After(podTerminationTimeout)

	log.Infof("Waiting for pods to terminate...")

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for pods to terminate after %v", podTerminationTimeout)
		case <-ticker.C:
			podList, err := k8sClient.Clientset().CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if err != nil {
				return fmt.Errorf("failed to list pods: %w", err)
			}

			// Filter out pods in terminal states (Succeeded, Failed) or being deleted
			activePods := 0
			for _, pod := range podList.Items {
				if pod.Status.Phase != corev1.PodSucceeded &&
					pod.Status.Phase != corev1.PodFailed &&
					pod.DeletionTimestamp == nil {
					activePods++
				}
			}

			if activePods == 0 && len(podList.Items) == 0 {
				log.Successf("All pods have terminated")
				return nil
			}

			log.Infof("Waiting for %d pod(s) to terminate...", activePods+len(podList.Items)-activePods)
		}
	}
}

// ScaleUpFromAnnotations scales up deployments that have pre-restore-replicas annotations
// This is used to scale up deployments after a background restore job completes
//
//nolint:revive // Package name "scale" with function "ScaleUpFromAnnotations" is intentionally verbose for clarity
func ScaleUpFromAnnotations(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) error {
	log.Infof("Scaling up deployments from annotations (selector: %s)...", labelSelector)

	scaledDeployments, err := k8sClient.ScaleUpDeploymentsFromAnnotations(namespace, labelSelector)
	if err != nil {
		return fmt.Errorf("failed to scale up deployments from annotations: %w", err)
	}

	if len(scaledDeployments) == 0 {
		log.Infof("No deployments found with pre-restore annotations to scale up")
		return nil
	}

	log.Successf("Scaled up %d deployment(s) successfully:", len(scaledDeployments))
	for _, dep := range scaledDeployments {
		log.Infof("  - %s (replicas: 0 -> %d)", dep.Name, dep.Replicas)
	}

	return nil
}
