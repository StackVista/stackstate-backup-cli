package stackgraphv2

import (
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
)

func Cmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stackgraph-v2",
		Short: "Stackgraph backup and restore (v2) operations",
	}

	cmd.AddCommand(listCmd(globalFlags))
	cmd.AddCommand(restoreCmd(globalFlags))
	cmd.AddCommand(backfillCmd(globalFlags))
	cmd.AddCommand(abortCmd(globalFlags))
	cmd.AddCommand(checkAndFinalizeCmd(globalFlags))

	return cmd
}
