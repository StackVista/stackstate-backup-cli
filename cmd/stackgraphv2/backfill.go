package stackgraphv2

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
)

const (
	backfillNameTemplate = "stackgraph-backfill-v2"
	backfillScript       = "/backup-restore-scripts/restore-stackgraph-backup-v2-backfill.sh"
)

func backfillCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Complete an interrupted restore by backfilling old data",
		Long: "Complete an interrupted restore by backfilling old data. This process can run while the system is already up and running." +
			"After the backfill is done, the restore process is complete. " +
			"During a normal restore the restore command will take care of backfilling the data, but if that gets interrupted (Ctrl-C) " +
			"or somehow crashes due to cluster instability, this command allows retrying the backfill portion of the restore.",
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runBackfill, cmdutils.StorageIsRequired)
		},
	}

	return cmd
}

func runBackfill(appCtx *app.Context) error {
	// Setup Kubernetes resources for restore job
	appCtx.Logger.Println()
	if err := restore.EnsureResources(appCtx.K8sClient, appCtx.Namespace, appCtx.Config, appCtx.Logger); err != nil {
		return err
	}

	// Create restore job
	appCtx.Logger.Println()
	appCtx.Logger.Infof("Creating backfill job.")

	jobName := fmt.Sprintf("%s-%s", backfillNameTemplate, time.Now().Format("20060102t150405"))

	if err := createBackfillJob(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Config); err != nil {
		return fmt.Errorf("failed to create backfill job: %w", err)
	}

	appCtx.Logger.Successf("Backfill job created: %s", jobName)

	err := waitAndCleanupBackfillJob(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger)

	if err != nil {
		appCtx.Logger.Println()
		appCtx.Logger.Infof("Backfill failed. It is possible to restart the backfill without starting a complete restore.")
		return err
	}

	return nil
}

// waitAndCleanupBackfillJob waits for job completion and cleans up resources
func waitAndCleanupBackfillJob(k8sClient *k8s.Client, namespace, jobName string, log *logger.Logger) error {
	restore.PrintWaitingMessage(log, "stackgraph-v2", jobName, namespace)
	return restore.WaitAndCleanup(k8sClient, namespace, jobName, log, false)
}

// createBackfillJob creates a Kubernetes Job for backfilling old data during a restore
func createBackfillJob(k8sClient *k8s.Client, namespace, jobName string, config *config.Config) error {
	return createSimpleJob(k8sClient, namespace, jobName, "backfill", backfillScript, config)
}
