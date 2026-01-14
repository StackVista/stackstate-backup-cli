package scale

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restorelock"
)

const (
	podTerminationCheckInterval = 2 * time.Second
	podTerminationTimeout       = 120 * time.Second
)

// ScaleDown scales down deployments matching the label selector and logs the results.
// It waits for all pods to terminate before returning.
//
//nolint:revive // Package name "scale" with function "ScaleDown" is intentionally verbose for clarity
func ScaleDown(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) ([]k8s.AppsScale, error) {
	log.Infof("Scaling down deployments (selector: %s)...", labelSelector)

	scaledDeployments, err := k8sClient.ScaleDownDeployments(namespace, labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to scale down deployments: %w", err)
	}

	scaledApps := scaledDeployments

	scaledStatefulSets, err := k8sClient.ScaleDownStatefulSets(namespace, labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to scale down statefulsets: %w", err)
	}

	scaledApps = append(scaledApps, scaledStatefulSets...)

	if len(scaledApps) == 0 {
		log.Infof("No deployments or statefulsets found to scale down")
		return scaledApps, nil
	}

	log.Successf("Scaled down %d deployment(s):", len(scaledDeployments))
	for _, dep := range scaledDeployments {
		log.Infof("  - %s (replicas: %d -> 0)", dep.Name, dep.Replicas)
	}

	log.Successf("Scaled down %d statefulsets(s):", len(scaledStatefulSets))
	for _, dep := range scaledStatefulSets {
		log.Infof("  - %s (replicas: %d -> 0)", dep.Name, dep.Replicas)
	}

	// Wait for pods to terminate
	if err := waitForPodsToTerminate(k8sClient, namespace, labelSelector, log); err != nil {
		return scaledApps, fmt.Errorf("failed waiting for pods to terminate: %w", err)
	}

	return scaledApps, nil
}

// ScaleDownWithLockParams contains parameters for ScaleDownWithLock
//
//nolint:revive // Keeping full name for clarity as this is a parameter struct for ScaleDownWithLock
type ScaleDownWithLockParams struct {
	K8sClient     *k8s.Client
	Namespace     string
	LabelSelector string
	Datastore     string
	AllSelectors  restorelock.LabelSelectors
	Log           *logger.Logger
}

// ScaleDownWithLock scales down deployments and statefulsets with restore lock protection.
// It first checks for conflicting restore operations, acquires a lock, then scales down.
// Returns the list of scaled resources that can be used for scale-up later.
//
//nolint:revive // Package name "scale" with function "ScaleDownWithLock" is intentionally verbose for clarity
func ScaleDownWithLock(params ScaleDownWithLockParams) ([]k8s.AppsScale, error) {
	// Check for conflicting restore operations
	if err := restorelock.CheckForConflicts(
		params.K8sClient,
		params.Namespace,
		params.Datastore,
		params.AllSelectors,
		params.Log,
	); err != nil {
		return nil, err
	}

	// Acquire restore lock before scaling down
	if err := restorelock.AcquireLock(
		params.K8sClient,
		params.Namespace,
		params.LabelSelector,
		params.Datastore,
		params.Log,
	); err != nil {
		return nil, err
	}

	// Scale down (the lock will be released when scaling up or on cleanup)
	scaledApps, err := ScaleDown(params.K8sClient, params.Namespace, params.LabelSelector, params.Log)
	if err != nil {
		// Release lock on scale-down failure
		releaseLockErr := restorelock.ReleaseLock(params.K8sClient, params.Namespace, params.LabelSelector, params.Log)
		if releaseLockErr != nil {
			params.Log.Errorf("Failed to release lock for scale down deployments: %s", releaseLockErr.Error())
			params.Log.Warningf("To manually remove a restore lock, run:\n"+
				"  kubectl annotate deployment,statefulset -l %s %s- %s- -n %s",
				params.LabelSelector,
				k8s.RestoreInProgressAnnotation,
				k8s.RestoreStartedAtAnnotation,
				params.Namespace,
			)
		}
		return nil, err
	}

	return scaledApps, nil
}

// ScaleUpAndReleaseLock scales up resources from annotations and releases the restore lock
//
//nolint:revive // Package name "scale" with function "ScaleUpAndReleaseLock" is intentionally verbose for clarity
func ScaleUpAndReleaseLock(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) error {
	// Scale up first
	if err := ScaleUpFromAnnotations(k8sClient, namespace, labelSelector, log); err != nil {
		return err
	}

	// Release restore lock
	if err := restorelock.ReleaseLock(k8sClient, namespace, labelSelector, log); err != nil {
		log.Warningf("Failed to release restore lock: %v", err)
		// Don't return error - scale up succeeded, lock release is secondary
	}

	return nil
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

	scaledStatefulSets, err := k8sClient.ScaleUpStatefulSetsFromAnnotations(namespace, labelSelector)
	if err != nil {
		return fmt.Errorf("failed to scale up statefulsets from annotations: %w", err)
	}

	if len(scaledDeployments) == 0 && len(scaledStatefulSets) == 0 {
		log.Infof("No statefulsets found with pre-restore annotations to scale up")
		return nil
	}

	log.Successf("Scaled up %d deployment(s) successfully:", len(scaledDeployments))
	for _, dep := range scaledDeployments {
		log.Infof("  - %s (replicas: 0 -> %d)", dep.Name, dep.Replicas)
	}

	log.Successf("Scaled up %d statefulset(s) successfully:", len(scaledStatefulSets))
	for _, dep := range scaledStatefulSets {
		log.Infof("  - %s (replicas: 0 -> %d)", dep.Name, dep.Replicas)
	}

	return nil
}
