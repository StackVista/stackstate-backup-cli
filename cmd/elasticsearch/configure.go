package elasticsearch

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/config"
	"github.com/stackvista/stackstate-backup-cli/internal/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/logger"
)

func configureCmd(cliCtx *config.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Configure Elasticsearch snapshot repository and SLM policy",
		Long:  `Configure Elasticsearch snapshot repository and Snapshot Lifecycle Management (SLM) policy for automated backups.`,
		Run: func(_ *cobra.Command, _ []string) {
			if err := runConfigure(cliCtx); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func runConfigure(cliCtx *config.Context) error {
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

	// Validate required configuration
	if cfg.Elasticsearch.SnapshotRepository.AccessKey == "" || cfg.Elasticsearch.SnapshotRepository.SecretKey == "" {
		return fmt.Errorf("accessKey and secretKey are required in the secret configuration")
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

	// Configure snapshot repository
	repo := cfg.Elasticsearch.SnapshotRepository
	log.Infof("Configuring snapshot repository '%s' (bucket: %s)...", repo.Name, repo.Bucket)

	err = esClient.ConfigureSnapshotRepository(
		repo.Name,
		repo.Bucket,
		repo.Endpoint,
		repo.BasePath,
		repo.AccessKey,
		repo.SecretKey,
	)
	if err != nil {
		return fmt.Errorf("failed to configure snapshot repository: %w", err)
	}

	log.Successf("Snapshot repository configured successfully")

	// Configure SLM policy
	slm := cfg.Elasticsearch.SLM
	log.Infof("Configuring SLM policy '%s'...", slm.Name)

	err = esClient.ConfigureSLMPolicy(
		slm.Name,
		slm.Schedule,
		slm.SnapshotTemplateName,
		slm.Repository,
		slm.Indices,
		slm.RetentionExpireAfter,
		slm.RetentionMinCount,
		slm.RetentionMaxCount,
	)
	if err != nil {
		return fmt.Errorf("failed to configure SLM policy: %w", err)
	}

	log.Successf("SLM policy configured successfully")
	log.Println()
	log.Successf("Configuration completed successfully")

	return nil
}
