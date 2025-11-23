package clickhouse

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/output"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
)

func listCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available Clickhouse backups",
		Long:  `List all Clickhouse backups from the ClickHouse Backup API.`,
		Run: func(_ *cobra.Command, _ []string) {
			appCtx, err := app.NewContext(globalFlags)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if err := runList(appCtx); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func runList(appCtx *app.Context) error {
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

	// List backups
	appCtx.Logger.Infof("Listing Clickhouse backups...")
	appCtx.Logger.Println()

	backups, err := appCtx.CHClient.ListBackups()
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	if len(backups) == 0 {
		appCtx.Formatter.PrintMessage("No backups found")
		return nil
	}

	// Sort by created time (most recent first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Created > backups[j].Created
	})

	table := output.Table{
		Headers: []string{"NAME", "CREATED", "SIZE"},
		Rows:    make([][]string, 0, len(backups)),
	}

	for _, backup := range backups {
		row := []string{
			backup.Name,
			backup.Created,
			output.FormatBytes(backup.Size),
		}
		table.Rows = append(table.Rows, row)
	}

	return appCtx.Formatter.PrintTable(table)
}
