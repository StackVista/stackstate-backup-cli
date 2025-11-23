package app

import (
	"context"
	"fmt"
	"os"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/clickhouse"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/s3"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/output"
)

// Context holds all dependencies for cli commands
type Context struct {
	K8sClient *k8s.Client
	Namespace string
	S3Client  s3.Interface
	ESClient  elasticsearch.Interface
	CHClient  clickhouse.Interface
	Config    *config.Config
	Logger    *logger.Logger
	Formatter *output.Formatter
	Context   context.Context
}

// NewContext creates production dependencies
func NewContext(flags *config.CLIGlobalFlags) (*Context, error) {
	k8sClient, err := k8s.NewClient(flags.Kubeconfig, flags.Debug)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Load configuration
	cfg, err := config.LoadConfig(k8sClient.Clientset(), flags.Namespace, flags.ConfigMapName, flags.SecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Create S3 client
	endpoint := fmt.Sprintf("http://localhost:%d", cfg.Minio.Service.LocalPortForwardPort)
	s3Client, err := s3.NewClient(endpoint, cfg.Minio.AccessKey, cfg.Minio.SecretKey)
	if err != nil {
		return nil, err
	}

	// Create Elasticsearch client
	esClient, err := elasticsearch.NewClient(fmt.Sprintf("http://localhost:%d", cfg.Elasticsearch.Service.LocalPortForwardPort))
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// Create ClickHouse client
	chClient, err := clickhouse.NewClient(
		fmt.Sprintf("http://localhost:%d", cfg.Clickhouse.BackupService.LocalPortForwardPort),
		fmt.Sprintf("localhost:%d", cfg.Clickhouse.Service.LocalPortForwardPort),
		cfg.Clickhouse.Database,
		cfg.Clickhouse.Username,
		cfg.Clickhouse.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to create ClickHouse client: %w", err)
	}

	// Format and print backups
	formatter := output.NewFormatter(os.Stdout, flags.OutputFormat)

	return &Context{
		K8sClient: k8sClient,
		Namespace: flags.Namespace,
		Config:    cfg,
		S3Client:  s3Client,
		ESClient:  esClient,
		CHClient:  chClient,
		Logger:    logger.New(flags.Quiet, flags.Debug),
		Formatter: formatter,
		Context:   context.Background(),
	}, nil
}
