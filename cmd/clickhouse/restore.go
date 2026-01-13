package clickhouse

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
)

// Restore command flags
var (
	restoreSnapshotName     string
	restoreUseLatest        bool
	restoreBackground       bool
	restoreSkipConfirmation bool
)

func restoreCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore ClickHouse from a backup archive",
		Long:  `Restore ClickHouse data from a backup archive via ClickHouse Backup API. Waits for completion by default; use --background to run asynchronously.`,
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runRestore, cmdutils.MinioIsRequired)
		},
	}

	cmd.Flags().StringVar(&restoreSnapshotName, "snapshot", "", "Specific snapshot/archive name to restore (e.g., full_2025-11-18T11-45-04)")
	cmd.Flags().BoolVar(&restoreUseLatest, "latest", false, "Restore from the most recent backup")
	cmd.Flags().BoolVar(&restoreBackground, "background", false, "Run restore in background without waiting for completion")
	cmd.Flags().BoolVarP(&restoreSkipConfirmation, "yes", "y", false, "Skip confirmation prompt")
	cmd.MarkFlagsMutuallyExclusive("snapshot", "latest")
	cmd.MarkFlagsOneRequired("snapshot", "latest")

	return cmd
}

func runRestore(appCtx *app.Context) error {
	// Determine which backup to restore
	backupName := restoreSnapshotName
	if restoreUseLatest {
		appCtx.Logger.Infof("Finding latest backup...")
		latest, err := getLatestBackupForRestore(appCtx)
		if err != nil {
			return err
		}
		backupName = latest
		appCtx.Logger.Infof("Using latest backup: %s", backupName)
	}

	// Warn user and ask for confirmation
	if !restoreSkipConfirmation {
		appCtx.Logger.Println()
		appCtx.Logger.Warningf("WARNING: Restoring from backup will overwrite existing ClickHouse data!")
		appCtx.Logger.Warningf("This operation cannot be undone.")
		appCtx.Logger.Println()
		appCtx.Logger.Infof("Backup to restore: %s", backupName)
		appCtx.Logger.Infof("Namespace: %s", appCtx.Namespace)
		appCtx.Logger.Println()

		if !restore.PromptForConfirmation() {
			return fmt.Errorf("restore operation cancelled by user")
		}
	}

	// Scale down deployments/statefulsets before restore (with lock protection)
	appCtx.Logger.Println()
	scaleDownLabelSelector := appCtx.Config.Clickhouse.Restore.ScaleDownLabelSelector
	_, err := scale.ScaleDownWithLock(scale.ScaleDownWithLockParams{
		K8sClient:     appCtx.K8sClient,
		Namespace:     appCtx.Namespace,
		LabelSelector: scaleDownLabelSelector,
		Datastore:     config.DatastoreClickhouse,
		AllSelectors:  appCtx.Config.GetAllScaleDownSelectors(),
		Log:           appCtx.Logger,
	})
	if err != nil {
		return err
	}

	// Execute restore workflow
	waitForComplete := !restoreBackground
	return executeRestore(appCtx, backupName, waitForComplete)
}

// executeRestore orchestrates the complete ClickHouse restore workflow
func executeRestore(appCtx *app.Context, backupName string, waitForComplete bool) error {
	// Setup port-forward to ClickHouse Backup API
	pf, err := portforward.SetupPortForward(
		appCtx.K8sClient,
		appCtx.Namespace,
		appCtx.Config.Clickhouse.BackupService.Name,
		appCtx.Config.Clickhouse.BackupService.LocalPortForwardPort,
		appCtx.Config.Clickhouse.BackupService.Port,
		appCtx.Logger,
	)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	// Trigger restore
	appCtx.Logger.Println()
	appCtx.Logger.Infof("Triggering restore for backup: %s", backupName)
	operationID, err := appCtx.CHClient.TriggerRestore(appCtx.Context, backupName)
	if err != nil {
		return fmt.Errorf("failed to trigger restore: %w", err)
	}
	appCtx.Logger.Successf("Restore triggered successfully (operation ID: %s)", operationID)

	if !waitForComplete {
		restore.PrintAPIRunningRestoreStatus("clickhouse", operationID, appCtx.Namespace, appCtx.Logger)
		return nil
	}

	return checkAndFinalize(appCtx, operationID, waitForComplete)
}

// getLatestBackupForRestore retrieves the most recent backup
func getLatestBackupForRestore(appCtx *app.Context) (string, error) {
	// Setup port-forward to ClickHouse Backup API
	pf, err := portforward.SetupPortForward(
		appCtx.K8sClient,
		appCtx.Namespace,
		appCtx.Config.Clickhouse.BackupService.Name,
		appCtx.Config.Clickhouse.BackupService.LocalPortForwardPort,
		appCtx.Config.Clickhouse.BackupService.Port,
		appCtx.Logger,
	)
	if err != nil {
		return "", err
	}
	defer close(pf.StopChan)

	// List backups
	backups, err := appCtx.CHClient.ListBackups(appCtx.Context)
	if err != nil {
		return "", fmt.Errorf("failed to list backups: %w", err)
	}

	if len(backups) == 0 {
		return "", fmt.Errorf("no backups found")
	}

	// Sort by created time (most recent first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Created > backups[j].Created
	})

	return backups[0].Name, nil
}
