package elasticsearch

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/output"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
)

func listIndicesCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list-indices",
		Short: "List Elasticsearch indices",
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runListIndices, cmdutils.MinioIsRequired)
		},
	}
}

func runListIndices(appCtx *app.Context) error {
	// Setup port-forward to Elasticsearch
	serviceName := appCtx.Config.Elasticsearch.Service.Name
	remotePort := appCtx.Config.Elasticsearch.Service.Port

	pf, err := portforward.SetupPortForward(appCtx.K8sClient, appCtx.Namespace, serviceName, remotePort, appCtx.Logger)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	// Create ES client with actual port
	esClient, err := appCtx.NewESClient(pf.LocalPort)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// List indices with cat API
	appCtx.Logger.Infof("Fetching Elasticsearch indices...")

	indices, err := esClient.ListIndicesDetailed()
	if err != nil {
		return fmt.Errorf("failed to list indices: %w", err)
	}

	if len(indices) == 0 {
		appCtx.Formatter.PrintMessage("No indices found")
		return nil
	}

	table := output.Table{
		Headers: []string{"HEALTH", "STATUS", "INDEX", "UUID", "PRI", "REP", "DOCS.COUNT", "DOCS.DELETED", "STORE.SIZE", "PRI.STORE.SIZE", "DATASET.SIZE"},
		Rows:    make([][]string, 0, len(indices)),
	}

	for _, idx := range indices {
		row := []string{
			idx.Health,
			idx.Status,
			idx.Index,
			idx.UUID,
			idx.Pri,
			idx.Rep,
			idx.DocsCount,
			idx.DocsDeleted,
			idx.StoreSize,
			idx.PriStoreSize,
			idx.DatasetSize,
		}
		table.Rows = append(table.Rows, row)
	}

	return appCtx.Formatter.PrintTable(table)
}
