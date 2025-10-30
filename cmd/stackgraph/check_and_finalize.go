package stackgraph

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
	batchv1 "k8s.io/api/batch/v1"
)

// Check and finalize command flags
var (
	checkJobName string
	waitForJob   bool
)

func checkAndFinalizeCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-and-finalize",
		Short: "Check and finalize a Stackgraph restore job",
		Long: `Check the status of a background Stackgraph restore job and clean up resources.

This command is useful when a restore job was started with --background flag or was interrupted (Ctrl+C).
It will check the job status, print logs if it failed, and clean up the job and PVC resources.

Examples:
  # Check job status without waiting
  sts-backup stackgraph check-and-finalize --job stackgraph-restore-20250128t143000 -n my-namespace

  # Wait for job completion and cleanup
  sts-backup stackgraph check-and-finalize --job stackgraph-restore-20250128t143000 --wait -n my-namespace`,
		Run: func(_ *cobra.Command, _ []string) {
			appCtx, err := app.NewContext(globalFlags)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if err := runCheckAndFinalize(appCtx); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVarP(&checkJobName, "job", "j", "", "Stackgraph restore job name (required)")
	cmd.Flags().BoolVarP(&waitForJob, "wait", "w", false, "Wait for job to complete before cleanup")
	_ = cmd.MarkFlagRequired("job")

	return cmd
}

func runCheckAndFinalize(appCtx *app.Context) error {
	// Get job
	appCtx.Logger.Infof("Checking status of job: %s", checkJobName)
	job, err := appCtx.K8sClient.GetJob(appCtx.Namespace, checkJobName)
	if err != nil {
		return fmt.Errorf("failed to get job '%s': %w (job may not exist or has been deleted)", checkJobName, err)
	}

	// Check if job is already complete
	completed, succeeded := isJobComplete(job)

	if completed {
		// Job already finished - print status and cleanup
		return handleCompletedJob(appCtx, checkJobName, succeeded)
	}

	// Job still running
	if waitForJob {
		// Wait for completion, then cleanup
		return waitAndFinalize(appCtx, checkJobName)
	}

	// Not waiting - just print status
	printRunningJobStatus(appCtx.Logger, checkJobName, appCtx.Namespace, job.Status.Active)
	return nil
}

// isJobComplete checks if job is in a terminal state
func isJobComplete(job *batchv1.Job) (completed bool, succeeded bool) {
	if job.Status.Succeeded > 0 {
		return true, true
	}
	if job.Status.Failed > 0 {
		return true, false
	}
	return false, false
}

// handleCompletedJob handles a job that's already complete
func handleCompletedJob(appCtx *app.Context, jobName string, succeeded bool) error {
	appCtx.Logger.Println()
	if succeeded {
		appCtx.Logger.Successf("Job completed successfully: %s", jobName)
		appCtx.Logger.Println()

		// Scale up deployments that were scaled down before restore
		scaleDownLabelSelector := appCtx.Config.Stackgraph.Restore.ScaleDownLabelSelector
		if err := scale.ScaleUpFromAnnotations(appCtx.K8sClient, appCtx.Namespace, scaleDownLabelSelector, appCtx.Logger); err != nil {
			appCtx.Logger.Warningf("Failed to scale up deployments: %v", err)
		}
	} else {
		appCtx.Logger.Errorf("Job failed: %s", jobName)
		appCtx.Logger.Println()
		appCtx.Logger.Infof("Fetching logs...")
		appCtx.Logger.Println()
		if err := printJobLogs(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger); err != nil {
			appCtx.Logger.Warningf("Failed to fetch logs: %v", err)
		}
	}

	// Cleanup resources
	appCtx.Logger.Println()
	return cleanupRestoreResources(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger)
}

// waitAndFinalize waits for job completion and then cleans up
func waitAndFinalize(appCtx *app.Context, jobName string) error {
	printWaitingMessage(appCtx.Logger, jobName, appCtx.Namespace)

	if err := waitForJobCompletion(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger); err != nil {
		appCtx.Logger.Errorf("Job failed: %v", err)
		// Still cleanup even if failed
		appCtx.Logger.Println()
		_ = cleanupRestoreResources(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger)
		return err
	}

	appCtx.Logger.Println()
	appCtx.Logger.Successf("Job completed successfully: %s", jobName)
	appCtx.Logger.Println()

	// Scale up deployments that were scaled down before restore
	scaleDownLabelSelector := appCtx.Config.Stackgraph.Restore.ScaleDownLabelSelector
	if err := scale.ScaleUpFromAnnotations(appCtx.K8sClient, appCtx.Namespace, scaleDownLabelSelector, appCtx.Logger); err != nil {
		appCtx.Logger.Warningf("Failed to scale up deployments: %v", err)
	}

	appCtx.Logger.Println()
	return cleanupRestoreResources(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger)
}
