package elasticsearch

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
)

var describeSnapshotName string

func describeCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe",
		Short: "Show detailed information about an Elasticsearch snapshot",
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runDescribeSnapshot, cmdutils.StorageIsRequired)
		},
	}

	cmd.Flags().StringVarP(&describeSnapshotName, "snapshot", "s", "", "Snapshot name to describe")
	_ = cmd.MarkFlagRequired("snapshot")

	return cmd
}

func runDescribeSnapshot(appCtx *app.Context) error {
	serviceName := appCtx.Config.Elasticsearch.Service.Name
	remotePort := appCtx.Config.Elasticsearch.Service.Port

	pf, err := portforward.SetupPortForward(appCtx.K8sClient, appCtx.Namespace, serviceName, remotePort, appCtx.Logger)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	esClient, err := appCtx.NewESClient(pf.LocalPort)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	repository := appCtx.Config.Elasticsearch.Restore.Repository
	appCtx.Logger.Infof("Fetching snapshot '%s' from repository '%s'...", describeSnapshotName, repository)

	snapshot, err := esClient.GetSnapshot(repository, describeSnapshotName)
	if err != nil {
		return fmt.Errorf("failed to get snapshot: %w", err)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}
