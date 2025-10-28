package elasticsearch

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/output"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
)

func listSnapshotsCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list-snapshots",
		Short: "List available Elasticsearch snapshots",
		Run: func(_ *cobra.Command, _ []string) {
			appCtx, err := app.NewContext(globalFlags)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if err := runListSnapshots(appCtx); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func runListSnapshots(appCtx *app.Context) error {
	// Setup port-forward to Elasticsearch
	serviceName := appCtx.Config.Elasticsearch.Service.Name
	localPort := appCtx.Config.Elasticsearch.Service.LocalPortForwardPort
	remotePort := appCtx.Config.Elasticsearch.Service.Port

	pf, err := portforward.SetupPortForward(appCtx.K8sClient, appCtx.Namespace, serviceName, localPort, remotePort, appCtx.Logger)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	// List snapshots
	repository := appCtx.Config.Elasticsearch.Restore.Repository
	appCtx.Logger.Infof("Fetching snapshots from repository '%s'...", repository)

	snapshots, err := appCtx.ESClient.ListSnapshots(repository)
	if err != nil {
		return fmt.Errorf("failed to list snapshots: %w", err)
	}

	if len(snapshots) == 0 {
		appCtx.Formatter.PrintMessage("No snapshots found")
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

	return appCtx.Formatter.PrintTable(table)
}
