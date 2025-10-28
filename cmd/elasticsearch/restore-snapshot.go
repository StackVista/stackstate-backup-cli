package elasticsearch

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
)

const (
	// defaultMaxIndexDeleteAttempts is the maximum number of attempts to verify index deletion
	defaultMaxIndexDeleteAttempts = 30
	// defaultIndexDeleteRetryInterval is the time to wait between index deletion verification attempts
	defaultIndexDeleteRetryInterval = 1 * time.Second
)

// Restore command flags
var (
	snapshotName     string
	dropAllIndices   bool
	skipConfirmation bool
)

func restoreCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore-snapshot",
		Short: "Restore Elasticsearch from a snapshot",
		Long:  `Restore Elasticsearch indices from a snapshot. Can optionally delete existing indices before restore.`,
		Run: func(_ *cobra.Command, _ []string) {
			appCtx, err := app.NewContext(globalFlags)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if err := runRestore(appCtx); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		}}

	cmd.Flags().StringVarP(&snapshotName, "snapshot-name", "s", "", "Snapshot name to restore (required)")
	cmd.Flags().BoolVarP(&dropAllIndices, "drop-all-indices", "r", false, "Delete all existing STS indices before restore")
	cmd.Flags().BoolVar(&skipConfirmation, "yes", false, "Skip confirmation prompt")
	_ = cmd.MarkFlagRequired("snapshot-name")
	return cmd
}

func runRestore(appCtx *app.Context) error {
	// Scale down deployments before restore
	scaledDeployments, err := scale.ScaleDown(appCtx.K8sClient, appCtx.Namespace, appCtx.Config.Elasticsearch.Restore.ScaleDownLabelSelector, appCtx.Logger)
	if err != nil {
		return err
	}

	// Ensure deployments are scaled back up on exit (even if restore fails)
	defer func() {
		if len(scaledDeployments) > 0 {
			appCtx.Logger.Println()
			if err := scale.ScaleUp(appCtx.K8sClient, appCtx.Namespace, scaledDeployments, appCtx.Logger); err != nil {
				appCtx.Logger.Warningf("Failed to scale up deployments: %v", err)
			}
		}
	}()

	// Setup port-forward to Elasticsearch
	serviceName := appCtx.Config.Elasticsearch.Service.Name
	localPort := appCtx.Config.Elasticsearch.Service.LocalPortForwardPort
	remotePort := appCtx.Config.Elasticsearch.Service.Port

	pf, err := portforward.SetupPortForward(appCtx.K8sClient, appCtx.Namespace, serviceName, localPort, remotePort, appCtx.Logger)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	repository := appCtx.Config.Elasticsearch.Restore.Repository

	// Get all indices and filter for STS indices
	appCtx.Logger.Infof("Fetching current Elasticsearch indices...")
	allIndices, err := appCtx.ESClient.ListIndices("*")
	if err != nil {
		return fmt.Errorf("failed to list indices: %w", err)
	}

	stsIndices := filterSTSIndices(allIndices, appCtx.Config.Elasticsearch.Restore.IndexPrefix, appCtx.Config.Elasticsearch.Restore.DatastreamIndexPrefix)

	if dropAllIndices {
		appCtx.Logger.Println()
		if err := deleteIndices(appCtx.ESClient, stsIndices, appCtx.Config, appCtx.Logger, skipConfirmation); err != nil {
			return err
		}
	}

	// Restore snapshot
	appCtx.Logger.Println()
	appCtx.Logger.Infof("Restoring snapshot '%s' from repository '%s'", snapshotName, repository)

	// Get snapshot details to show indices
	snapshot, err := appCtx.ESClient.GetSnapshot(repository, snapshotName)
	if err != nil {
		return fmt.Errorf("failed to get snapshot details: %w", err)
	}

	appCtx.Logger.Debugf("Indices pattern: %s", appCtx.Config.Elasticsearch.Restore.IndicesPattern)

	if len(snapshot.Indices) == 0 {
		appCtx.Logger.Warningf("Snapshot contains no indices")
	} else {
		appCtx.Logger.Infof("Snapshot contains %d index(es)", len(snapshot.Indices))
		for _, index := range snapshot.Indices {
			appCtx.Logger.Debugf("  - %s", index)
		}
	}

	appCtx.Logger.Infof("Starting restore - this may take several minutes...")

	if err := appCtx.ESClient.RestoreSnapshot(repository, snapshotName, appCtx.Config.Elasticsearch.Restore.IndicesPattern, true); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	appCtx.Logger.Println()
	appCtx.Logger.Successf("Restore completed successfully")
	return nil
}

// filterSTSIndices filters indices that match the configured STS prefixes
func filterSTSIndices(allIndices []string, indexPrefix, datastreamPrefix string) []string {
	var stsIndices []string
	for _, index := range allIndices {
		if strings.HasPrefix(index, indexPrefix) || strings.HasPrefix(index, datastreamPrefix) {
			stsIndices = append(stsIndices, index)
		}
	}
	return stsIndices
}

// confirmDeletion prompts the user to confirm index deletion
func confirmDeletion() error {
	fmt.Print("\nAre you sure you want to delete these indices? (yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "yes" && response != "y" {
		return fmt.Errorf("restore cancelled by user")
	}
	return nil
}

// hasDatastreamIndices checks if any indices belong to a datastream
func hasDatastreamIndices(indices []string, datastreamPrefix string) bool {
	for _, index := range indices {
		if strings.HasPrefix(index, datastreamPrefix+"-") {
			return true
		}
	}
	return false
}

// deleteIndexWithVerification deletes an index and verifies it's gone
func deleteIndexWithVerification(esClient elasticsearch.Interface, index string, log *logger.Logger) error {
	log.Infof("  Deleting index: %s", index)
	if err := esClient.DeleteIndex(index); err != nil {
		return fmt.Errorf("failed to delete index %s: %w", index, err)
	}

	// Verify deletion with timeout
	for attempt := 0; attempt < defaultMaxIndexDeleteAttempts; attempt++ {
		exists, err := esClient.IndexExists(index)
		if err != nil {
			return fmt.Errorf("failed to check index existence: %w", err)
		}
		if !exists {
			log.Debugf("Index successfully deleted: %s", index)
			return nil
		}
		if attempt >= defaultMaxIndexDeleteAttempts-1 {
			return fmt.Errorf("timeout waiting for index %s to be deleted", index)
		}
		time.Sleep(defaultIndexDeleteRetryInterval)
	}
	return nil
}

// deleteIndices handles the deletion of all STS indices including datastream rollover
func deleteIndices(esClient elasticsearch.Interface, stsIndices []string, cfg *config.Config, log *logger.Logger, skipConfirm bool) error {
	if len(stsIndices) == 0 {
		log.Infof("No STS indices found to delete")
		return nil
	}

	log.Infof("Found %d STS index(es) to delete", len(stsIndices))
	for _, index := range stsIndices {
		log.Debugf("  - %s", index)
	}

	// Confirmation prompt
	if !skipConfirm {
		if err := confirmDeletion(); err != nil {
			return err
		}
	}

	// Check for datastream and rollover if needed
	if hasDatastreamIndices(stsIndices, cfg.Elasticsearch.Restore.DatastreamIndexPrefix) {
		log.Infof("Rolling over datastream '%s'...", cfg.Elasticsearch.Restore.DatastreamName)
		if err := esClient.RolloverDatastream(cfg.Elasticsearch.Restore.DatastreamName); err != nil {
			return fmt.Errorf("failed to rollover datastream: %w", err)
		}
		log.Successf("Datastream rolled over successfully")
	}

	// Delete all indices
	log.Infof("Deleting %d index(es)...", len(stsIndices))
	for _, index := range stsIndices {
		if err := deleteIndexWithVerification(esClient, index, log); err != nil {
			return err
		}
	}
	log.Successf("All indices deleted successfully")
	return nil
}
