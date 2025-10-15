package elasticsearch

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/config"
	"github.com/stackvista/stackstate-backup-cli/internal/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/logger"
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

func restoreCmd(cliCtx *config.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore-snapshot",
		Short: "Restore Elasticsearch from a snapshot",
		Long:  `Restore Elasticsearch indices from a snapshot. Can optionally delete existing indices before restore.`,
		Run: func(_ *cobra.Command, _ []string) {
			if err := runRestore(cliCtx); err != nil {
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

func runRestore(cliCtx *config.Context) error {
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

	// Scale down deployments before restore
	scaledDeployments, err := scaleDownDeployments(k8sClient, cliCtx.Config.Namespace, cfg.Elasticsearch.Restore.ScaleDownLabelSelector, log)
	if err != nil {
		return err
	}

	// Ensure deployments are scaled back up on exit (even if restore fails)
	defer func() {
		if len(scaledDeployments) > 0 {
			log.Println()
			log.Infof("Scaling up deployments back to original replica counts...")
			if err := k8sClient.ScaleUpDeployments(cliCtx.Config.Namespace, scaledDeployments); err != nil {
				log.Warningf("Failed to scale up deployments: %v", err)
			} else {
				log.Successf("Scaled up %d deployment(s) successfully:", len(scaledDeployments))
				for _, dep := range scaledDeployments {
					log.Infof("  - %s (replicas: 0 -> %d)", dep.Name, dep.Replicas)
				}
			}
		}
	}()

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

	repository := cfg.Elasticsearch.Restore.Repository

	// Get all indices and filter for STS indices
	log.Infof("Fetching current Elasticsearch indices...")
	allIndices, err := esClient.ListIndices("*")
	if err != nil {
		return fmt.Errorf("failed to list indices: %w", err)
	}

	stsIndices := filterSTSIndices(allIndices, cfg.Elasticsearch.Restore.IndexPrefix, cfg.Elasticsearch.Restore.DatastreamIndexPrefix)

	if dropAllIndices {
		log.Println()
		if err := deleteIndices(esClient, stsIndices, cfg, log, skipConfirmation); err != nil {
			return err
		}
	}

	// Restore snapshot
	log.Println()
	log.Infof("Restoring snapshot '%s' from repository '%s'", snapshotName, repository)

	// Get snapshot details to show indices
	snapshot, err := esClient.GetSnapshot(repository, snapshotName)
	if err != nil {
		return fmt.Errorf("failed to get snapshot details: %w", err)
	}

	log.Debugf("Indices pattern: %s", cfg.Elasticsearch.Restore.IndicesPattern)

	if len(snapshot.Indices) == 0 {
		log.Warningf("Snapshot contains no indices")
	} else {
		log.Infof("Snapshot contains %d index(es)", len(snapshot.Indices))
		for _, index := range snapshot.Indices {
			log.Debugf("  - %s", index)
		}
	}

	log.Infof("Starting restore - this may take several minutes...")

	if err := esClient.RestoreSnapshot(repository, snapshotName, cfg.Elasticsearch.Restore.IndicesPattern, true); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	log.Println()
	log.Successf("Restore completed successfully")
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
func deleteIndexWithVerification(esClient *elasticsearch.Client, index string, log *logger.Logger) error {
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

// scaleDownDeployments scales down deployments matching the label selector
func scaleDownDeployments(k8sClient *k8s.Client, namespace, labelSelector string, log *logger.Logger) ([]k8s.DeploymentScale, error) {
	log.Infof("Scaling down deployments (selector: %s)...", labelSelector)

	scaledDeployments, err := k8sClient.ScaleDownDeployments(namespace, labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to scale down deployments: %w", err)
	}

	if len(scaledDeployments) == 0 {
		log.Infof("No deployments found to scale down")
	} else {
		log.Successf("Scaled down %d deployment(s):", len(scaledDeployments))
		for _, dep := range scaledDeployments {
			log.Infof("  - %s (replicas: %d -> 0)", dep.Name, dep.Replicas)
		}
	}

	return scaledDeployments, nil
}

// deleteIndices handles the deletion of all STS indices including datastream rollover
func deleteIndices(esClient *elasticsearch.Client, stsIndices []string, cfg *config.Config, log *logger.Logger, skipConfirm bool) error {
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
