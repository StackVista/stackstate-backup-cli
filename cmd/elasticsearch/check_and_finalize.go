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

// Check-and-finalize command flags
var (
	checkOperationID  string
	checkWait         bool
	checkNoProgressIn time.Duration
	checkFinalizeOnly bool
)

func checkAndFinalizeCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-and-finalize",
		Short: "Check restore status and finalize if complete",
		Long: `Check the status of a restore operation and perform finalization (scale up deployments) if complete.
If the restore is still running and --wait is specified, wait for completion before finalizing.`,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if !checkFinalizeOnly && checkOperationID == "" {
				return fmt.Errorf("--operation-id is required unless --finalize-only is set")
			}

			return nil
		},
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runCheckAndFinalize, cmdutils.StorageIsRequired)
		},
	}

	cmd.Flags().StringVar(&checkOperationID, "operation-id", "",
		"Snapshot name of the restore operation (required unless --finalize-only)")
	cmd.Flags().BoolVar(&checkWait, "wait", false, "Wait for restore to complete if still running")
	cmd.Flags().DurationVar(&checkNoProgressIn, "no-progress-timeout", defaultNoProgressTimeout, noProgressTimeoutUsage)
	cmd.Flags().BoolVar(&checkFinalizeOnly, "finalize-only", false,
		"Scale the deployments back up and release the restore lock without checking restore status. "+
			"Use when the snapshot can no longer be read and the restore is known to be finished")
	cmd.MarkFlagsMutuallyExclusive("finalize-only", "wait")

	return cmd
}

func runCheckAndFinalize(appCtx *app.Context) error {
	// Finalizing needs Kubernetes only, so it stays available when Elasticsearch or the snapshot
	// cannot be reached at all - otherwise nothing in the CLI can release the restore lock.
	if checkFinalizeOnly {
		appCtx.Logger.Warningf("Skipping the restore status check on request; finalizing")
		return finalizeRestore(appCtx)
	}

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

	repository := appCtx.Config.Elasticsearch.Restore.Repository

	return checkAndFinalize(esClient, appCtx, repository, checkOperationID, checkWait, checkNoProgressIn)
}

const (
	// defaultNoProgressTimeout bounds the wait so a stuck restore fails instead of polling until
	// the caller's own timeout with the workloads scaled down and the restore lock held. Generous
	// on purpose: a spurious failure aborts a working restore, while the only cost of waiting too
	// long is a later failure. Progress is counted per primary shard, so tripping this means no
	// shard at all completed in the window.
	defaultNoProgressTimeout = 2 * time.Hour

	noProgressTimeoutUsage = "Fail if no primary shard finishes restoring within this duration. " +
		"Bounds inactivity, not total restore time; 0 waits indefinitely"

	// restoreStatusMaxErrors tolerates a port-forward dropping mid-restore. Polling now spans the
	// whole restore, so a pod restart must not end a restore that is still running server-side.
	restoreStatusMaxErrors = 5
)

// reconnectingHealthClient rebuilds the port-forward and Elasticsearch client after a failed
// health check. It starts from the caller's client so the happy path opens no extra port-forward.
type reconnectingHealthClient struct {
	appCtx *app.Context
	client indicesHealthGetter
	pf     *portforward.Conn
}

func (r *reconnectingHealthClient) GetIndicesHealth() (map[string]es.IndexHealth, error) {
	if r.client == nil {
		if err := r.connect(); err != nil {
			return nil, err
		}
	}

	health, err := r.client.GetIndicesHealth()
	if err != nil {
		// The tunnel is bound to a fixed local port, so a broken one never recovers. Drop it and
		// let the next poll dial a fresh pod.
		r.disconnect()
		return nil, err
	}

	return health, nil
}

func (r *reconnectingHealthClient) connect() error {
	pf, err := portforward.SetupPortForward(
		r.appCtx.K8sClient,
		r.appCtx.Namespace,
		r.appCtx.Config.Elasticsearch.Service.Name,
		r.appCtx.Config.Elasticsearch.Service.Port,
		r.appCtx.Logger,
	)
	if err != nil {
		return err
	}

	client, err := r.appCtx.NewESClient(pf.LocalPort)
	if err != nil {
		close(pf.StopChan)
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	r.pf, r.client = pf, client

	return nil
}

// disconnect drops the client and closes only a port-forward this type opened itself; the one the
// caller passed in is closed by the caller.
func (r *reconnectingHealthClient) disconnect() {
	if r.pf != nil {
		close(r.pf.StopChan)
		r.pf = nil
	}

	r.client = nil
}

// expectedRestoredIndices returns the snapshot's indices that this restore recreates. The snapshot
// is re-read rather than passed in so that check-and-finalize works from just a snapshot name;
// snapshots are immutable, so the list cannot drift.
func expectedRestoredIndices(esClient es.Interface, appCtx *app.Context, repository, snapshotName string) ([]string, error) {
	snapshot, err := esClient.GetSnapshot(repository, snapshotName)
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot details: %w", err)
	}

	return restorableSnapshotIndices(snapshot, appCtx.Config.Elasticsearch.Restore)
}

func restorableSnapshotIndices(snapshot *es.Snapshot, restoreConfig config.RestoreConfig) ([]string, error) {
	candidates := filterSTSIndices(
		snapshot.Indices,
		restoreConfig.IndexPrefix,
		restoreConfig.DatastreamIndexPrefix,
	)
	expected, err := filterIndicesByPattern(candidates, restoreConfig.IndicesPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid Elasticsearch restore indicesPattern: %w", err)
	}
	if len(expected) == 0 {
		return nil, fmt.Errorf(
			"snapshot %s contains no indices matching the configured STS prefixes and indicesPattern",
			snapshot.Snapshot,
		)
	}

	hasRequiredIndex := false
	for _, index := range expected {
		if !isDatastreamBackingIndex(index, restoreConfig.DatastreamIndexPrefix) {
			hasRequiredIndex = true
			break
		}
	}
	if !hasRequiredIndex {
		return nil, fmt.Errorf(
			"snapshot %s contains only lifecycle-managed data-stream backing indices; "+
				"restore completion cannot be determined safely",
			snapshot.Snapshot,
		)
	}

	return expected, nil
}

// restoreProgress summarises how far a restore has got. Replicas are deliberately ignored: a
// cluster with fewer nodes than replicas keeps them unassigned forever, so requiring green would
// never complete there.
type restoreProgress struct {
	// indicesRestored counts present expected indices with every primary shard active.
	indicesRestored int
	// primariesActive counts active primary shards, and drives stall detection. Whole indices are
	// too coarse for that: a large multi-shard index can restore for a long time without finishing.
	primariesActive int
	// requiredExpected and requiredRestored track indices that ILM cannot remove.
	requiredExpected int
	requiredRestored int
	// lifecycleExpected and lifecyclePresent track data-stream backing indices. ILM can remove an
	// oldest contiguous prefix while the restore is running.
	lifecycleExpected int
	lifecyclePresent  int
	lifecycleMissing  int
	lifecycleGap      bool
}

func measureRestore(expected []string, datastreamPrefix string, health map[string]es.IndexHealth) restoreProgress {
	var progress restoreProgress
	var lifecycleIndices []string

	for _, index := range expected {
		if isDatastreamBackingIndex(index, datastreamPrefix) {
			lifecycleIndices = append(lifecycleIndices, index)
			continue
		}

		progress.requiredExpected++
		indexHealth, exists := health[index]
		if !exists {
			continue
		}

		progress.primariesActive += indexHealth.ActivePrimaryShards
		if indexHealth.NumberOfShards > 0 && indexHealth.ActivePrimaryShards == indexHealth.NumberOfShards {
			progress.indicesRestored++
			progress.requiredRestored++
		}
	}

	sort.Strings(lifecycleIndices)
	progress.lifecycleExpected = len(lifecycleIndices)

	seenPresent := false
	for _, index := range lifecycleIndices {
		indexHealth, exists := health[index]
		if !exists {
			progress.lifecycleMissing++
			if seenPresent {
				progress.lifecycleGap = true
			}
			continue
		}

		seenPresent = true
		progress.lifecyclePresent++
		progress.primariesActive += indexHealth.ActivePrimaryShards
		if indexHealth.NumberOfShards > 0 && indexHealth.ActivePrimaryShards == indexHealth.NumberOfShards {
			progress.indicesRestored++
		}
	}

	return progress
}

func isDatastreamBackingIndex(index, datastreamPrefix string) bool {
	return strings.HasPrefix(index, datastreamPrefix+"-")
}

func (p restoreProgress) complete() bool {
	if p.requiredExpected == 0 {
		return false
	}

	if p.requiredRestored != p.requiredExpected {
		return false
	}

	// Required indices prove the restore's cluster-state update has applied; target index metadata
	// is created together before shard recovery starts.
	if p.lifecycleExpected == 0 || p.lifecyclePresent == 0 {
		return true
	}

	// With any backing indices left, only an oldest prefix may be absent.
	return !p.lifecycleGap &&
		p.indicesRestored == p.requiredRestored+p.lifecyclePresent
}

type indicesHealthGetter interface {
	GetIndicesHealth() (map[string]es.IndexHealth, error)
}

// newRestoreStatusFn builds the status callback used for both the single check and the wait loop.
// Completion is derived from the restored indices themselves: a restore that has been accepted but
// not yet applied is indistinguishable from a finished one when judged by recovery activity alone.
func newRestoreStatusFn(
	esClient indicesHealthGetter,
	log *logger.Logger,
	expected []string,
	datastreamPrefix string,
	noProgressTimeout time.Duration,
	maxErrors int,
) func() (string, bool, error) {
	lastPrimariesActive := -1
	lastLifecyclePresent := -1
	lastProgressAt := time.Now()
	errCount := 0

	return func() (string, bool, error) {
		health, err := esClient.GetIndicesHealth()
		if err != nil {
			errCount++
			if errCount >= maxErrors {
				return "", false, err
			}

			log.Warningf("Restore status check failed (%d/%d), retrying: %v", errCount, maxErrors, err)

			return es.StatusInProgress, false, nil
		}
		errCount = 0

		progress := measureRestore(expected, datastreamPrefix, health)
		if progress.complete() {
			if progress.lifecycleMissing > 0 {
				indexWord := "indices"
				if progress.lifecycleMissing == 1 {
					indexWord = "index"
				}
				log.Infof(
					"Restore complete; %d oldest data-stream backing %s no longer present, consistent with lifecycle retention",
					progress.lifecycleMissing, indexWord,
				)
			}
			return es.StatusSuccess, true, nil
		}

		lifecycleRemoved := lastLifecyclePresent >= 0 && progress.lifecyclePresent < lastLifecyclePresent
		lastLifecyclePresent = progress.lifecyclePresent

		if lifecycleRemoved && progress.primariesActive < lastPrimariesActive {
			// An ILM deletion makes totals before and after this poll incomparable. Rebaseline
			// without treating the deletion as progress or failing before a later shard increase
			// can be observed.
			lastPrimariesActive = progress.primariesActive
		}

		if progress.primariesActive > lastPrimariesActive {
			lastPrimariesActive = progress.primariesActive
			lastProgressAt = time.Now()
		} else if !lifecycleRemoved && noProgressTimeout > 0 && time.Since(lastProgressAt) > noProgressTimeout {
			// Deliberately not reported as a failed restore: all this establishes is that no progress
			// was observed from here. Restarting a restore deletes every STS index first, so a caller
			// that reads this as "failed" and retries would destroy a restore that is merely slow.
			return "", false, fmt.Errorf(
				"elasticsearch restore stalled: no primary shard finished restoring in %s "+
					"(%d of %d indices present and complete, %d lifecycle-managed indices absent, %d primaries active); "+
					"it may still be running server-side, so check before restarting it",
				noProgressTimeout, progress.indicesRestored, len(expected), progress.lifecycleMissing, progress.primariesActive,
			)
		}

		log.Debugf(
			"Restored %d of %d indices (%d lifecycle-managed indices absent, %d primaries active)",
			progress.indicesRestored, len(expected), progress.lifecycleMissing, progress.primariesActive,
		)

		return es.StatusInProgress, false, nil
	}
}

func checkAndFinalize(
	esClient es.Interface,
	appCtx *app.Context,
	repository, snapshotName string,
	waitForComplete bool,
	noProgressTimeout time.Duration,
) error {
	expected, err := expectedRestoredIndices(esClient, appCtx, repository, snapshotName)
	if err != nil {
		return err
	}

	healthClient := &reconnectingHealthClient{appCtx: appCtx, client: esClient}
	defer healthClient.disconnect()

	// Retrying only pays off while polling. A one-shot check that swallowed the error would report
	// a dead tunnel as a running restore and exit 0.
	maxErrors := restoreStatusMaxErrors
	if !waitForComplete {
		maxErrors = 1
	}

	statusFn := newRestoreStatusFn(
		healthClient,
		appCtx.Logger,
		expected,
		appCtx.Config.Elasticsearch.Restore.DatastreamIndexPrefix,
		noProgressTimeout,
		maxErrors,
	)

	// Get restore status
	appCtx.Logger.Infof("Checking restore status for snapshot: %s (%d indices)", snapshotName, len(expected))
	status, isComplete, err := statusFn()
	if err != nil {
		return fmt.Errorf("failed to get restore status: %w", err)
	}

	appCtx.Logger.Debugf("Restore status: %s (complete: %v)", status, isComplete)

	// Handle different scenarios
	if isComplete {
		switch status {
		case es.StatusSuccess:
			appCtx.Logger.Successf("Restore completed successfully")
			return finalizeRestore(appCtx)
		default:
			return fmt.Errorf("restore completed with unexpected status: %s", status)
		}
	}

	// Restore still running
	appCtx.Logger.Infof("Restore is still in progress (status: %s)", status)

	if waitForComplete {
		appCtx.Logger.Println()
		return waitAndFinalize(statusFn, appCtx, snapshotName)
	}

	// Not waiting - print status and exit
	appCtx.Logger.Println()
	restore.PrintAPIRunningRestoreStatus("elasticsearch", snapshotName, appCtx.Namespace, appCtx.Logger)
	return nil
}

// waitAndFinalize waits for restore to complete and finalizes (scale up)
func waitAndFinalize(statusFn func() (string, bool, error), appCtx *app.Context, snapshotName string) error {
	restore.PrintAPIWaitingMessage("elasticsearch", snapshotName, appCtx.Namespace, appCtx.Logger)

	if err := restore.WaitForAPIRestore(statusFn, 0, appCtx.Logger); err != nil {
		return err
	}

	// Finalize restore (scale up)
	return finalizeRestore(appCtx)
}

// finalizeRestore performs post-restore finalization (scale up deployments and release lock)
func finalizeRestore(appCtx *app.Context) error {
	appCtx.Logger.Println()
	labelSelector := appCtx.Config.Elasticsearch.Restore.ScaleDownLabelSelector
	scaleUpFn := func() error {
		return scale.ScaleUpAndReleaseLock(appCtx.K8sClient, appCtx.Namespace, labelSelector, appCtx.Logger)
	}

	return restore.FinalizeRestore(scaleUpFn, appCtx.Logger)
}
