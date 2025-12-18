package clickhouse

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/clickhouse"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
)

const (
	defaultRestoreTimeout = 30 * time.Minute
	defaultPollInterval   = 10 * time.Second
)

// Check-and-finalize command flags
var (
	checkOperationID string
	waitForRestore   bool
)

func checkAndFinalizeCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-and-finalize",
		Short: "Check and finalize a ClickHouse restore operation",
		Long: `Check the status of a ClickHouse restore operation and finalize it.

This command is useful when a restore was started without --wait flag or was interrupted.
It will check the restore status and if complete, execute post-restore tasks and scale up resources.`,
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runCheckAndFinalize, cmdutils.MinioIsRequired)
		},
	}

	cmd.Flags().StringVar(&checkOperationID, "operation-id", "", "Operation ID of the restore operation (required)")
	cmd.Flags().BoolVar(&waitForRestore, "wait", false, "Wait for restore to complete before finalizing")
	_ = cmd.MarkFlagRequired("operation-id")

	return cmd
}

func runCheckAndFinalize(appCtx *app.Context) error {
	// Setup port-forward
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
	return checkAndFinalize(appCtx, checkOperationID, waitForRestore)
}

// checkAndFinalize checks restore status and finalizes if complete
func checkAndFinalize(appCtx *app.Context, operationID string, waitForComplete bool) error {
	// Check status
	appCtx.Logger.Println()
	appCtx.Logger.Infof("Checking restore status for operation: %s", operationID)
	status, err := appCtx.CHClient.GetRestoreStatus(appCtx.Context, operationID)
	if err != nil {
		return err
	}

	if status.Status == "error" {
		return fmt.Errorf("restore failed: %s", status.Error)
	}

	if status.Status == "success" {
		appCtx.Logger.Successf("Restore completed successfully")
		return finalizeRestore(appCtx)
	}

	// Restore still running
	appCtx.Logger.Infof("Restore is still in progress (status: %s)", status)

	// Status is "in progress" or other
	if waitForComplete {
		// Still running - wait
		appCtx.Logger.Infof("Restore is in progress, waiting for completion...")
		return waitAndFinalize(appCtx, appCtx.CHClient, operationID)
	}
	// Just print status
	appCtx.Logger.Println()
	restore.PrintAPIRunningRestoreStatus("clickhouse", operationID, appCtx.Namespace, appCtx.Logger)
	return nil
}

// waitAndFinalize waits for restore completion and finalizes
func waitAndFinalize(appCtx *app.Context, chClient clickhouse.Interface, operationID string) error {
	restore.PrintAPIWaitingMessage("clickhouse", operationID, appCtx.Namespace, appCtx.Logger)

	// Wait for restore using shared utility
	checkStatusFn := func() (string, bool, error) {
		status, err := chClient.GetRestoreStatus(appCtx.Context, operationID)
		if err != nil {
			return "", false, err
		}

		switch status.Status {
		case "success":
			return "SUCCESS", true, nil
		case "error":
			return "FAILED", true, fmt.Errorf("%s", status.Error)
		default:
			return "IN_PROGRESS", false, nil
		}
	}

	if err := restore.WaitForAPIRestore(checkStatusFn, defaultPollInterval, defaultRestoreTimeout, appCtx.Logger); err != nil {
		return err
	}

	appCtx.Logger.Successf("Restore completed successfully")

	// Finalize
	return finalizeRestore(appCtx)
}

// finalizeRestore finalizes the restore by executing SQL and scaling up
func finalizeRestore(appCtx *app.Context) error {
	if err := executePostRestoreSQL(appCtx); err != nil {
		appCtx.Logger.Warningf("Post-restore SQL failed: %v", err)
	}

	appCtx.Logger.Println()
	scaleSelector := appCtx.Config.Clickhouse.Restore.ScaleDownLabelSelector
	if err := scale.ScaleUpFromAnnotations(
		appCtx.K8sClient,
		appCtx.Namespace,
		scaleSelector,
		appCtx.Logger,
	); err != nil {
		return fmt.Errorf("failed to scale up: %w", err)
	}

	appCtx.Logger.Println()
	appCtx.Logger.Successf("Restore finalized successfully")
	return nil
}

// executePostRestoreSQL executes post-restore SQL commands
func executePostRestoreSQL(appCtx *app.Context) error {
	appCtx.Logger.Infof("Executing post-restore SQL commands...")

	// Setup port-forward to ClickHouse database service
	pf, err := portforward.SetupPortForward(
		appCtx.K8sClient,
		appCtx.Namespace,
		appCtx.Config.Clickhouse.Service.Name,
		appCtx.Config.Clickhouse.Service.LocalPortForwardPort,
		appCtx.Config.Clickhouse.Service.Port,
		appCtx.Logger,
	)
	if err != nil {
		return fmt.Errorf("failed to setup port-forward for SQL: %w", err)
	}
	defer close(pf.StopChan)

	// Create ClickHouse SQL connection
	conn, closeConn, err := appCtx.CHClient.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}
	defer func() {
		_ = closeConn()
	}()

	// Execute post-restore SQL command
	query := "SYSTEM DROP MARK CACHE"

	appCtx.Logger.Debugf("Executing SQL: %s", query)
	if err := conn.Exec(appCtx.Context, query); err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}

	appCtx.Logger.Successf("Post-restore SQL executed successfully")
	return nil
}
