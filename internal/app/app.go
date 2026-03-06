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

	// Format and print backups
	formatter := output.NewFormatter(os.Stdout, flags.OutputFormat)

	return &Context{
		K8sClient: k8sClient,
		Namespace: flags.Namespace,
		Config:    cfg,
		Logger:    logger.New(flags.Quiet, flags.Debug),
		Formatter: formatter,
		Context:   context.Background(),
	}, nil
}

// NewS3Client creates an S3 client connecting to the given local port-forwarded port
func (c *Context) NewS3Client(localPort int) (s3.Interface, error) {
	endpoint := fmt.Sprintf("http://localhost:%d", localPort)
	return s3.NewClient(endpoint, c.Config.Minio.AccessKey, c.Config.Minio.SecretKey)
}

// NewESClient creates an Elasticsearch client connecting to the given local port-forwarded port
func (c *Context) NewESClient(localPort int) (elasticsearch.Interface, error) {
	return elasticsearch.NewClient(fmt.Sprintf("http://localhost:%d", localPort))
}

// NewCHClient creates a ClickHouse client. Pass backupAPIPort for backup API access,
// and dbPort for SQL access. Use 0 for either if not needed.
func (c *Context) NewCHClient(backupAPIPort, dbPort int) (clickhouse.Interface, error) {
	backupAPIURL := ""
	if backupAPIPort > 0 {
		backupAPIURL = fmt.Sprintf("http://localhost:%d", backupAPIPort)
	}
	dbAddr := ""
	if dbPort > 0 {
		dbAddr = fmt.Sprintf("localhost:%d", dbPort)
	}
	return clickhouse.NewClient(
		backupAPIURL,
		dbAddr,
		c.Config.Clickhouse.Database,
		c.Config.Clickhouse.Username,
		c.Config.Clickhouse.Password,
	)
}
