package elasticsearch

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/config"
	"github.com/stackvista/stackstate-backup-cli/internal/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/output"
)

func listIndicesCmd(cliCtx *config.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "list-indices",
		Short: "List Elasticsearch indices",
		Run: func(_ *cobra.Command, _ []string) {
			if err := runListIndices(cliCtx); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func runListIndices(cliCtx *config.Context) error {
	// Create logger
	log := logger.New(cliCtx.Config.Quiet, cliCtx.Config.Debug)

	// Create Kubernetes client
	k8sClient, err := k8s.NewClient(cliCtx.Config.Kubeconfig, cliCtx.Config.Debug)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Load configuration
	cfg, err := config.LoadConfig(k8sClient.Clientset(), cliCtx.Config.Namespace, cliCtx.Config.ConfigMapName, cliCtx.Config.SecretName)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Setup port-forward to Elasticsearch
	serviceName := cfg.Elasticsearch.Service.Name
	localPort := cfg.Elasticsearch.Service.LocalPortForwardPort
	remotePort := cfg.Elasticsearch.Service.Port

	log.Infof("Setting up port-forward to %s:%d in namespace %s...", serviceName, remotePort, cliCtx.Config.Namespace)

	stopChan, readyChan, err := k8sClient.PortForwardService(cliCtx.Config.Namespace, serviceName, localPort, remotePort)
	if err != nil {
		return fmt.Errorf("failed to setup port-forward: %w", err)
	}
	defer close(stopChan)

	// Wait for port-forward to be ready
	<-readyChan

	log.Successf("Port-forward established successfully")

	// Create Elasticsearch client
	esClient, err := elasticsearch.NewClient(fmt.Sprintf("http://localhost:%d", localPort))
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// List indices with cat API
	log.Infof("Fetching Elasticsearch indices...")

	indices, err := esClient.ListIndicesDetailed()
	if err != nil {
		return fmt.Errorf("failed to list indices: %w", err)
	}

	// Format and print indices
	formatter := output.NewFormatter(cliCtx.Config.OutputFormat)

	if len(indices) == 0 {
		formatter.PrintMessage("No indices found")
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

	return formatter.PrintTable(table)
}
