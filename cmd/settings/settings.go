package settings

import (
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
)

func Cmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Settings backup and restore operations",
	}

	cmd.AddCommand(listCmd(globalFlags))
	cmd.AddCommand(restoreCmd(globalFlags))
	cmd.AddCommand(checkAndFinalizeCmd(globalFlags))

	return cmd
}
