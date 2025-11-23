package elasticsearch

import (
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
)

func Cmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "elasticsearch",
		Short: "Elasticsearch backup and restore operations",
	}

	cmd.AddCommand(listCmd(globalFlags))
	cmd.AddCommand(listIndicesCmd(globalFlags))
	cmd.AddCommand(restoreCmd(globalFlags))
	cmd.AddCommand(checkAndFinalizeCmd(globalFlags))
	cmd.AddCommand(configureCmd(globalFlags))

	return cmd
}
