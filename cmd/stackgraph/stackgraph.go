package stackgraph

import (
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
)

func Cmd(cliCtx *config.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stackgraph",
		Short: "Stackgraph backup and restore operations",
	}

	cmd.AddCommand(listCmd(cliCtx))
	cmd.AddCommand(restoreCmd(cliCtx))

	return cmd
}
