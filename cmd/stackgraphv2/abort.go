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
	abortNameTemplate = "stackgraph-v2-abort"
	abortScript       = "/backup-restore-scripts/restore-stackgraph-backup-v2-abort.sh"
)

func abortCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "abort",
		Short: "Abort a partially backfilled restore.",
		Long: "Abort a partially backfilled restore. This will not load (backfill) any data but keep the data as is." +
			"Be aware this leaves the restore incomplete, historical data will not be recovered, but leave the instance in a usable state." +
			"This should be used when backfilling is failing and the database restore is accepted as-is.",
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runAbort, cmdutils.StorageIsRequired)
		},
	}

	return cmd
}

func runAbort(appCtx *app.Context) error {
	// Setup Kubernetes resources for restore job
	appCtx.Logger.Println()
	if err := restore.EnsureResources(appCtx.K8sClient, appCtx.Namespace, appCtx.Config, appCtx.Logger); err != nil {
		return err
	}

	// Create restore job
	appCtx.Logger.Println()
	appCtx.Logger.Infof("Creating abort job.")

	jobName := fmt.Sprintf("%s-%s", abortNameTemplate, time.Now().Format("20060102t150405"))

	if err := createAbortJob(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Config); err != nil {
		return fmt.Errorf("failed to create abort job: %w", err)
	}

	appCtx.Logger.Successf("Abort job created: %s", jobName)

	err := waitAndCleanupAbortJob(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger)

	if err != nil {
		logAfterJobResult(appCtx.Logger, checkJobName, false)
		return err
	}

	return nil
}

// waitAndCleanupAbortJob waits for job completion and cleans up resources
func waitAndCleanupAbortJob(k8sClient *k8s.Client, namespace, jobName string, log *logger.Logger) error {
	restore.PrintWaitingMessage(log, "stackgraph-v2", jobName, namespace)
	return restore.WaitAndCleanup(k8sClient, namespace, jobName, log, false)
}

// createAbortJob creates a Kubernetes Job for aborting a partially backfilled restore
func createAbortJob(k8sClient *k8s.Client, namespace, jobName string, config *config.Config) error {
	return createSimpleJob(k8sClient, namespace, jobName, "abort", abortScript, config)
}
