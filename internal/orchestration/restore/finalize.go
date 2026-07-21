package restore

import (
	"fmt"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	batchv1 "k8s.io/api/batch/v1"
)

// IsJobComplete checks if a job is in a terminal state
// Returns (completed, succeeded) where:
// - completed: true if job is in a terminal state (succeeded or failed)
// - succeeded: true if job completed successfully
func IsJobComplete(job *batchv1.Job) (completed bool, succeeded bool) {
	if job.Status.Succeeded > 0 {
		return true, true
	}
	if job.Status.Failed > 0 {
		return true, false
	}
	return false, false
}

// HandleCompletedJobParams contains parameters for HandleCompletedJob
type HandleCompletedJobParams struct {
	K8sClient     *k8s.Client
	Namespace     string
	JobName       string
	ServiceName   string
	ScaleUpFn     func(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) error
	ScaleDownFn   func(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) ([]k8s.AppsScale, error)
	ScaleSelector string
	CleanupPVC    bool
	Log           *logger.Logger
	JobSucceeded  bool
}

// HandleCompletedJob handles a job that's already complete
// This includes printing status, fetching logs on failure, scaling up, and cleanup
func HandleCompletedJob(params HandleCompletedJobParams) error {
	params.Log.Println()
	if params.JobSucceeded {
		params.Log.Successf("Job completed successfully: %s", params.JobName)
		params.Log.Println()

		// Scale up deployments that were scaled down before restore
		if err := params.ScaleUpFn(params.K8sClient, params.Namespace, params.ScaleSelector, params.Log); err != nil {
			params.Log.Warningf("Failed to scale up workload: %v", err)
		}
	} else {
		params.Log.Errorf("Job failed: %s", params.JobName)
		params.Log.Println()
		params.Log.Infof("Fetching logs...")
		params.Log.Println()
		if err := PrintJobLogs(params.K8sClient, params.Namespace, params.JobName, params.Log); err != nil {
			params.Log.Warningf("Failed to fetch logs: %v", err)
		}
	}

	// Cleanup resources
	params.Log.Println()
	return CleanupResources(params.K8sClient, params.Namespace, params.JobName, "", params.Log, params.CleanupPVC)
}

// WaitAndFinalizeParams contains parameters for WaitAndFinalize
type WaitAndFinalizeParams struct {
	K8sClient     *k8s.Client
	Namespace     string
	JobName       string
	ServiceName   string
	ScaleUpFn     func(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) error
	ScaleDownFn   func(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) ([]k8s.AppsScale, error)
	ScaleSelector string
	CleanupPVC    bool
	Log           *logger.Logger
}

// WaitAndFinalize waits for job completion and then cleans up
func WaitAndFinalize(params WaitAndFinalizeParams) error {
	PrintWaitingMessage(params.Log, params.ServiceName, params.JobName, params.Namespace)

	if err := WaitForJobCompletion(params.K8sClient, params.Namespace, params.JobName, params.Log); err != nil {
		params.Log.Errorf("Job failed: %v", err)
		// Still cleanup even if failed
		params.Log.Println()
		_ = CleanupResources(params.K8sClient, params.Namespace, params.JobName, "", params.Log, params.CleanupPVC)
		return err
	}

	params.Log.Println()
	params.Log.Successf("Job completed successfully: %s", params.JobName)
	params.Log.Println()

	// Scale up deployments that were scaled down before restore
	if err := params.ScaleUpFn(params.K8sClient, params.Namespace, params.ScaleSelector, params.Log); err != nil {
		params.Log.Warningf("Failed to scale up workload: %v", err)
	}

	params.Log.Println()
	return CleanupResources(params.K8sClient, params.Namespace, params.JobName, "", params.Log, params.CleanupPVC)
}

// CheckAndFinalizeParams contains parameters for CheckAndFinalize
type CheckAndFinalizeParams struct {
	K8sClient     *k8s.Client
	Namespace     string
	JobName       string
	ServiceName   string
	ScaleUpFn     func(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) error
	ScaleDownFn   func(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) ([]k8s.AppsScale, error)
	ScaleSelector string
	CleanupPVC    bool
	WaitForJob    bool
	Log           *logger.Logger
}

// CheckAndFinalize checks the status of a background restore job and cleans up resources
// This is useful when a restore job was started with --background flag or was interrupted (Ctrl+C)
// Returns whether the job succeeded
func CheckAndFinalize(params CheckAndFinalizeParams) error {
	// Get job
	params.Log.Infof("Checking status of job: %s", params.JobName)
	job, err := params.K8sClient.GetJob(params.Namespace, params.JobName)
	if err != nil {
		return fmt.Errorf("failed to get job '%s': %w (job may not exist or has been deleted)", params.JobName, err)
	}

	// Check if job is already complete
	completed, succeeded := IsJobComplete(job)

	if completed {
		// Job already finished - print status and cleanup
		return HandleCompletedJob(HandleCompletedJobParams{
			K8sClient:     params.K8sClient,
			Namespace:     params.Namespace,
			JobName:       params.JobName,
			ServiceName:   params.ServiceName,
			ScaleUpFn:     params.ScaleUpFn,
			ScaleDownFn:   params.ScaleDownFn,
			ScaleSelector: params.ScaleSelector,
			CleanupPVC:    params.CleanupPVC,
			Log:           params.Log,
			JobSucceeded:  succeeded,
		})
	}

	// Job still running
	if params.WaitForJob {
		// Wait for completion, then cleanup
		return WaitAndFinalize(WaitAndFinalizeParams{
			K8sClient:     params.K8sClient,
			Namespace:     params.Namespace,
			JobName:       params.JobName,
			ServiceName:   params.ServiceName,
			ScaleUpFn:     params.ScaleUpFn,
			ScaleDownFn:   params.ScaleDownFn,
			ScaleSelector: params.ScaleSelector,
			CleanupPVC:    params.CleanupPVC,
			Log:           params.Log,
		})
	}

	// Not waiting - just print status
	PrintRunningJobStatus(params.Log, params.ServiceName, params.JobName, params.Namespace, job.Status.Active)
	return nil
}
