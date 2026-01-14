package settings

import (
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
)

// Check and finalize command flags
var (
	checkJobName string
	waitForJob   bool
)

func checkAndFinalizeCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-and-finalize",
		Short: "Check and finalize a Settings restore job",
		Long: `Check the status of a background Settings restore job and clean up resources.

This command is useful when a restore job was started with --background flag or was interrupted (Ctrl+C).
It will check the job status, print logs if it failed, and clean up the job and PVC resources.

Examples:
  # Check job status without waiting
  sts-backup settings check-and-finalize --job settings-restore-20250128t143000 -n my-namespace

  # Wait for job completion and cleanup
  sts-backup settings check-and-finalize --job settings-restore-20250128t143000 --wait -n my-namespace`,
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runCheckAndFinalize, cmdutils.MinioIsNotRequired)
		},
	}

	cmd.Flags().StringVarP(&checkJobName, "job", "j", "", "Settings restore job name (required)")
	cmd.Flags().BoolVarP(&waitForJob, "wait", "w", false, "Wait for job to complete before cleanup")
	_ = cmd.MarkFlagRequired("job")

	return cmd
}

func runCheckAndFinalize(appCtx *app.Context) error {
	return restore.CheckAndFinalize(restore.CheckAndFinalizeParams{
		K8sClient:     appCtx.K8sClient,
		Namespace:     appCtx.Namespace,
		JobName:       checkJobName,
		ServiceName:   "settings",
		ScaleUpFn:     scale.ScaleUpAndReleaseLock,
		ScaleDownFn:   scale.ScaleDown,
		ScaleSelector: appCtx.Config.Settings.Restore.ScaleDownLabelSelector,
		CleanupPVC:    false,
		WaitForJob:    waitForJob,
		Log:           appCtx.Logger,
	})
}
