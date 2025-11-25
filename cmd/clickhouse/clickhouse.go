package clickhouse

import (
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
)

// NewClickhouseCmd creates the clickhouse parent command
func NewClickhouseCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clickhouse",
		Short: "Manage Clickhouse backups and restores",
		Long:  `Commands for listing, restoring, and managing Clickhouse backups.`,
	}

	cmd.AddCommand(listCmd(globalFlags))
	cmd.AddCommand(restoreCmd(globalFlags))
	cmd.AddCommand(checkAndFinalizeCmd(globalFlags))

	return cmd
}
