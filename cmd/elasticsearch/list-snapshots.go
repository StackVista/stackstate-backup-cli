package elasticsearch

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/config"
	"github.com/stackvista/stackstate-backup-cli/internal/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/output"
)

func listSnapshotsCmd(cliCtx *config.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "list-snapshots",
		Short: "List available Elasticsearch snapshots",
		Run: func(_ *cobra.Command, _ []string) {
			if err := runListSnapshots(cliCtx); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func runListSnapshots(cliCtx *config.Context) error {
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

	pf, err := portforward.SetupPortForward(k8sClient, cliCtx.Config.Namespace, serviceName, localPort, remotePort, log)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	// Create Elasticsearch client
	esClient, err := elasticsearch.NewClient(fmt.Sprintf("http://localhost:%d", pf.LocalPort))
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// List snapshots
	repository := cfg.Elasticsearch.Restore.Repository
	log.Infof("Fetching snapshots from repository '%s'...", repository)

	snapshots, err := esClient.ListSnapshots(repository)
	if err != nil {
		return fmt.Errorf("failed to list snapshots: %w", err)
	}

	// Format and print snapshots
	formatter := output.NewFormatter(cliCtx.Config.OutputFormat)

	if len(snapshots) == 0 {
		formatter.PrintMessage("No snapshots found")
		return nil
	}

	table := output.Table{
		Headers: []string{"SNAPSHOT", "STATE", "START TIME", "DURATION (ms)", "FAILURES"},
		Rows:    make([][]string, 0, len(snapshots)),
	}

	for _, snapshot := range snapshots {
		failures := "0"
		if len(snapshot.Failures) > 0 {
			failures = fmt.Sprintf("%d", len(snapshot.Failures))
		}

		row := []string{
			snapshot.Snapshot,
			snapshot.State,
			snapshot.StartTime,
			fmt.Sprintf("%d", snapshot.DurationInMillis),
			failures,
		}
		table.Rows = append(table.Rows, row)
	}

	return formatter.PrintTable(table)
}
