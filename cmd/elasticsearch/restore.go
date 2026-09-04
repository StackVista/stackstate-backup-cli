package elasticsearch

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	es "github.com/stackvista/stackstate-backup-cli/internal/clients/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
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
	snapshotName        string
	useLatest           bool
	runBackground       bool
	skipConfirmation    bool
	allowPartial        bool
	restoreNoProgressIn time.Duration
)

func restoreCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore Elasticsearch from a snapshot",
		Long:  `Restore Elasticsearch indices from a snapshot. Deletes existing STS indices before restore. Waits for completion by default; use --background to run asynchronously.`,
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runRestore, cmdutils.StorageIsRequired)
		}}

	cmd.Flags().StringVarP(&snapshotName, "snapshot", "s", "", "Snapshot name to restore (mutually exclusive with --latest)")
	cmd.Flags().BoolVar(&useLatest, "latest", false, "Restore from the most recent snapshot (mutually exclusive with --snapshot)")
	cmd.Flags().BoolVar(&runBackground, "background", false, "Run restore in background without waiting for completion")
	cmd.Flags().BoolVarP(&skipConfirmation, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&allowPartial, "allow-partial", false, "Allow restoring from a PARTIAL snapshot without extra confirmation")
	cmd.Flags().DurationVar(&restoreNoProgressIn, "no-progress-timeout", defaultNoProgressTimeout, noProgressTimeoutUsage)
	cmd.MarkFlagsMutuallyExclusive("snapshot", "latest")
	cmd.MarkFlagsOneRequired("snapshot", "latest")
	return cmd
}

func runRestore(appCtx *app.Context) error {
	// Setup port-forward to Elasticsearch (needed for both snapshot selection and restore)
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

	repository := appCtx.Config.Elasticsearch.Restore.Repository

	// Determine snapshot name (either from flag or latest)
	selectedSnapshot := snapshotName
	if useLatest {
		appCtx.Logger.Infof("Fetching latest snapshot from repository '%s'...", repository)
		latestSnapshot, err := getLatestSnapshot(esClient, repository)
		if err != nil {
			return err
		}
		selectedSnapshot = latestSnapshot
		appCtx.Logger.Successf("Latest snapshot found: %s", selectedSnapshot)
	}

	// Fetch snapshot details to check state
	snapshotDetails, err := esClient.GetSnapshot(repository, selectedSnapshot)
	if err != nil {
		return fmt.Errorf("failed to get snapshot details: %w", err)
	}

	// Validate snapshot state
	if err := validateSnapshotState(snapshotDetails, appCtx, skipConfirmation, allowPartial); err != nil {
		return err
	}

	// Reject snapshots that cannot be monitored safely before scaling down workloads or deleting
	// any indices. check-and-finalize repeats this validation for independently resumed operations.
	if _, err := restorableSnapshotIndices(snapshotDetails, appCtx.Config.Elasticsearch.Restore); err != nil {
		return err
	}

	// Confirm with user before starting destructive operation
	if !skipConfirmation {
		appCtx.Logger.Println()
		appCtx.Logger.Warningf("WARNING: Restoring from snapshot will DELETE all existing STS indices!")
		appCtx.Logger.Warningf("This operation cannot be undone.")
		appCtx.Logger.Println()
		appCtx.Logger.Infof("Snapshot to restore: %s", selectedSnapshot)
		appCtx.Logger.Infof("Snapshot state: %s", snapshotDetails.State)
		appCtx.Logger.Infof("Namespace: %s", appCtx.Namespace)
		appCtx.Logger.Println()

		if !restore.PromptForConfirmation() {
			return fmt.Errorf("restore operation cancelled by user")
		}
	}

	// Scale down deployments before restore (with lock protection)
	appCtx.Logger.Println()
	scaleDownLabelSelector := appCtx.Config.Elasticsearch.Restore.ScaleDownLabelSelector
	_, err = scale.ScaleDownWithLock(scale.ScaleDownWithLockParams{
		K8sClient:     appCtx.K8sClient,
		Namespace:     appCtx.Namespace,
		LabelSelector: scaleDownLabelSelector,
		Datastore:     config.DatastoreElasticsearch,
		AllSelectors:  appCtx.Config.GetAllScaleDownSelectors(),
		Log:           appCtx.Logger,
	})
	if err != nil {
		return err
	}

	// Delete all STS indices before restore
	appCtx.Logger.Println()
	if err := deleteAllSTSIndices(esClient, appCtx); err != nil {
		return err
	}

	// Trigger async restore
	appCtx.Logger.Println()
	isPartial := snapshotDetails.State == "PARTIAL"
	appCtx.Logger.Infof("Triggering restore for snapshot: %s", selectedSnapshot)
	if err := esClient.RestoreSnapshot(repository, selectedSnapshot, appCtx.Config.Elasticsearch.Restore.IndicesPattern, isPartial); err != nil {
		return fmt.Errorf("failed to trigger restore: %w", err)
	}
	appCtx.Logger.Successf("Restore triggered successfully")

	if runBackground {
		restore.PrintAPIRunningRestoreStatus("elasticsearch", selectedSnapshot, appCtx.Namespace, appCtx.Logger)
		return nil
	}

	return checkAndFinalize(esClient, appCtx, repository, selectedSnapshot, !runBackground, restoreNoProgressIn)
}

// getLatestSnapshot retrieves the most recent snapshot from the repository
func getLatestSnapshot(esClient es.Interface, repository string) (string, error) {
	snapshots, err := esClient.ListSnapshots(repository)
	if err != nil {
		return "", fmt.Errorf("failed to list snapshots: %w", err)
	}

	if len(snapshots) == 0 {
		return "", fmt.Errorf("no snapshots found in repository '%s'", repository)
	}

	// Sort snapshots by start time in descending order (newest first)
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].StartTimeMillis > snapshots[j].StartTimeMillis
	})

	return snapshots[0].Snapshot, nil
}

// validateSnapshotState checks the snapshot state and handles PARTIAL snapshots
func validateSnapshotState(snapshot *es.Snapshot, appCtx *app.Context, skipConfirm, allowPartialFlag bool) error {
	switch snapshot.State {
	case es.StatusSuccess:
		return nil
	case "PARTIAL":
		failureCount := len(snapshot.Failures)
		if allowPartialFlag {
			appCtx.Logger.Warningf("Snapshot '%s' is PARTIAL (%d shard failure(s)), proceeding due to --allow-partial flag",
				snapshot.Snapshot, failureCount)
			return nil
		}
		if skipConfirm {
			return fmt.Errorf("snapshot '%s' is PARTIAL with %d shard failure(s); "+
				"use --allow-partial together with --yes to restore a partial snapshot non-interactively",
				snapshot.Snapshot, failureCount)
		}
		// Interactive mode: warn and ask for explicit confirmation
		appCtx.Logger.Println()
		appCtx.Logger.Warningf("WARNING: Snapshot '%s' is in PARTIAL state!", snapshot.Snapshot)
		appCtx.Logger.Warningf("  %d shard(s) failed out of %d total (%d successful)",
			snapshot.Shards.Failed, snapshot.Shards.Total, snapshot.Shards.Successful)
		appCtx.Logger.Warningf("Restoring this snapshot will result in incomplete data for the failed shards.")
		appCtx.Logger.Println()
		if !restore.PromptForConfirmation() {
			return fmt.Errorf("restore operation cancelled by user")
		}
		return nil
	default:
		return fmt.Errorf("snapshot '%s' is in %s state and cannot be restored", snapshot.Snapshot, snapshot.State)
	}
}

// deleteAllSTSIndices deletes all STS indices including datastream rollover if needed
func deleteAllSTSIndices(esClient es.Interface, appCtx *app.Context) error {
	appCtx.Logger.Infof("Fetching current Elasticsearch indices...")
	allIndices, err := esClient.ListIndices("*")
	if err != nil {
		return fmt.Errorf("failed to list indices: %w", err)
	}

	stsIndices := filterSTSIndices(allIndices, appCtx.Config.Elasticsearch.Restore.IndexPrefix, appCtx.Config.Elasticsearch.Restore.DatastreamIndexPrefix)

	if len(stsIndices) == 0 {
		appCtx.Logger.Infof("No STS indices found to delete")
		return nil
	}

	appCtx.Logger.Infof("Found %d STS index(es) to delete", len(stsIndices))
	for _, index := range stsIndices {
		appCtx.Logger.Debugf("  - %s", index)
	}

	// Check for datastream and rollover if needed
	if hasDatastreamIndices(stsIndices, appCtx.Config.Elasticsearch.Restore.DatastreamIndexPrefix) {
		appCtx.Logger.Infof("Rolling over datastream '%s'...", appCtx.Config.Elasticsearch.Restore.DatastreamName)
		if err := esClient.RolloverDatastream(appCtx.Config.Elasticsearch.Restore.DatastreamName); err != nil {
			return fmt.Errorf("failed to rollover datastream: %w", err)
		}
		appCtx.Logger.Successf("Datastream rolled over successfully")
	}

	// Delete all indices
	appCtx.Logger.Infof("Deleting %d index(es)...", len(stsIndices))
	for _, index := range stsIndices {
		if err := deleteIndexWithVerification(esClient, index, appCtx.Logger); err != nil {
			return err
		}
	}
	appCtx.Logger.Successf("All indices deleted successfully")
	return nil
}

// filterSTSIndices filters indices that match the configured STS prefixes
func filterSTSIndices(allIndices []string, indexPrefix, datastreamPrefix string) []string {
	var stsIndices []string
	for _, index := range allIndices {
		if strings.HasPrefix(index, indexPrefix) || isDatastreamBackingIndex(index, datastreamPrefix) {
			stsIndices = append(stsIndices, index)
		}
	}
	return stsIndices
}

// hasDatastreamIndices checks if any indices belong to a datastream
func hasDatastreamIndices(indices []string, datastreamPrefix string) bool {
	for _, index := range indices {
		if isDatastreamBackingIndex(index, datastreamPrefix) {
			return true
		}
	}
	return false
}

// deleteIndexWithVerification deletes an index and verifies it's gone
func deleteIndexWithVerification(esClient es.Interface, index string, log *logger.Logger) error {
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
