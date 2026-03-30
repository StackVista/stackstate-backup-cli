package elasticsearch

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/output"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
)

func listCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available Elasticsearch snapshots",
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runListSnapshots, cmdutils.StorageIsRequired)
		},
	}
}

func runListSnapshots(appCtx *app.Context) error {
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

	// List snapshots
	repository := appCtx.Config.Elasticsearch.Restore.Repository
	appCtx.Logger.Infof("Fetching snapshots from repository '%s'...", repository)

	snapshots, err := esClient.ListSnapshots(repository)
	if err != nil {
		return fmt.Errorf("failed to list snapshots: %w", err)
	}

	if len(snapshots) == 0 {
		appCtx.Formatter.PrintMessage("No snapshots found")
		return nil
	}

	// Sort snapshots by start time in descending order (newest first)
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].StartTimeMillis > snapshots[j].StartTimeMillis
	})

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

	return appCtx.Formatter.PrintTable(table)
}
