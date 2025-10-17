package elasticsearch

import (
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/config"
)

func Cmd(cliCtx *config.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "elasticsearch",
		Short: "Elasticsearch backup and restore operations",
	}

	cmd.AddCommand(listSnapshotsCmd(cliCtx))
	cmd.AddCommand(listIndicesCmd(cliCtx))
	cmd.AddCommand(restoreCmd(cliCtx))
	cmd.AddCommand(configureCmd(cliCtx))

	return cmd
}
