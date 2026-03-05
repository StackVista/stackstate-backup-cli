package elasticsearch

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
)

// Check-and-finalize command flags
var (
	checkOperationID string
	checkWait        bool
)

func checkAndFinalizeCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-and-finalize",
		Short: "Check restore status and finalize if complete",
		Long: `Check the status of a restore operation and perform finalization (scale up deployments) if complete.
If the restore is still running and --wait is specified, wait for completion before finalizing.`,
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runCheckAndFinalize, cmdutils.MinioIsRequired)
		},
	}

	cmd.Flags().StringVar(&checkOperationID, "operation-id", "", "Operation ID of the restore operation (required)")
	cmd.Flags().BoolVar(&checkWait, "wait", false, "Wait for restore to complete if still running")
	_ = cmd.MarkFlagRequired("operation-id")

	return cmd
}

func runCheckAndFinalize(appCtx *app.Context) error {
	// Setup port-forward to Elasticsearch
	serviceName := appCtx.Config.Elasticsearch.Service.Name
	localPort := appCtx.Config.Elasticsearch.Service.LocalPortForwardPort
	remotePort := appCtx.Config.Elasticsearch.Service.Port

	pf, err := portforward.SetupPortForward(appCtx.K8sClient, appCtx.Namespace, serviceName, localPort, remotePort, appCtx.Logger)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	repository := appCtx.Config.Elasticsearch.Restore.Repository

	return checkAndFinalize(appCtx, repository, checkOperationID, checkWait)
}

func checkAndFinalize(appCtx *app.Context, repository, snapshotName string, waitForComplete bool) error {
	// Get restore status
	appCtx.Logger.Infof("Checking restore status for snapshot: %s", snapshotName)
	status, isComplete, err := appCtx.ESClient.GetRestoreStatus(repository, snapshotName)
	if err != nil {
		return fmt.Errorf("failed to get restore status: %w", err)
	}

	appCtx.Logger.Debugf("Restore status: %s (complete: %v)", status, isComplete)

	// Handle different scenarios
	if isComplete {
		switch status {
		case "SUCCESS":
			appCtx.Logger.Successf("Restore completed successfully")
			return finalizeRestore(appCtx)
		case "NOT_FOUND":
			appCtx.Logger.Infof("No restore operation found for snapshot: %s", snapshotName)
			appCtx.Logger.Infof("The restore may have already been finalized")
			appCtx.Logger.Println()
			appCtx.Logger.Infof("Checking if deployments need to be scaled up...")
			return attemptScaleUp(appCtx)
		case "FAILED":
			return fmt.Errorf("restore failed with status: %s", status)
		default:
			return fmt.Errorf("restore completed with unexpected status: %s", status)
		}
	}

	// Restore still running
	appCtx.Logger.Infof("Restore is still in progress (status: %s)", status)

	if waitForComplete {
		appCtx.Logger.Println()
		return waitAndFinalize(appCtx, repository, snapshotName)
	}

	// Not waiting - print status and exit
	appCtx.Logger.Println()
	restore.PrintAPIRunningRestoreStatus("elasticsearch", snapshotName, appCtx.Namespace, appCtx.Logger)
	return nil
}

// waitAndFinalize waits for restore to complete and finalizes (scale up)
func waitAndFinalize(appCtx *app.Context, repository, snapshotName string) error {
	restore.PrintAPIWaitingMessage("elasticsearch", snapshotName, appCtx.Namespace, appCtx.Logger)

	// Wait for restore to complete
	checkStatusFn := func() (string, bool, error) {
		return appCtx.ESClient.GetRestoreStatus(repository, snapshotName)
	}

	if err := restore.WaitForAPIRestore(checkStatusFn, 0, appCtx.Logger); err != nil {
		return err
	}

	// Finalize restore (scale up)
	return finalizeRestore(appCtx)
}

// finalizeRestore performs post-restore finalization (scale up deployments and release lock)
func finalizeRestore(appCtx *app.Context) error {
	appCtx.Logger.Println()
	labelSelector := appCtx.Config.Elasticsearch.Restore.ScaleDownLabelSelector
	scaleUpFn := func() error {
		return scale.ScaleUpAndReleaseLock(appCtx.K8sClient, appCtx.Namespace, labelSelector, appCtx.Logger)
	}

	return restore.FinalizeRestore(scaleUpFn, appCtx.Logger)
}

// attemptScaleUp tries to scale up deployments and release lock (used when restore is not found/already complete)
func attemptScaleUp(appCtx *app.Context) error {
	labelSelector := appCtx.Config.Elasticsearch.Restore.ScaleDownLabelSelector
	scaleUpFn := func() error {
		return scale.ScaleUpAndReleaseLock(appCtx.K8sClient, appCtx.Namespace, labelSelector, appCtx.Logger)
	}

	if err := scaleUpFn(); err != nil {
		// Don't fail if no deployments found to scale up
		appCtx.Logger.Infof("No deployments found to scale up (this is normal if already finalized)")
		return nil
	}

	appCtx.Logger.Successf("Finalization completed successfully")
	return nil
}
