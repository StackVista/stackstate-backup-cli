package stackgraph

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	s3client "github.com/stackvista/stackstate-backup-cli/internal/clients/s3"
	cfg "github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/output"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
)

func listCmd(cliCtx *cfg.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available Stackgraph backups from S3/Minio",
		Run: func(_ *cobra.Command, _ []string) {
			if err := runList(cliCtx); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func runList(cliCtx *cfg.Context) error {
	// Create logger
	log := logger.New(cliCtx.Config.Quiet, cliCtx.Config.Debug)

	// Create Kubernetes client
	k8sClient, err := k8s.NewClient(cliCtx.Config.Kubeconfig, cliCtx.Config.Debug)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Load configuration
	config, err := cfg.LoadConfig(k8sClient.Clientset(), cliCtx.Config.Namespace, cliCtx.Config.ConfigMapName, cliCtx.Config.SecretName)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Setup port-forward to Minio
	serviceName := config.Minio.Service.Name
	localPort := config.Minio.Service.LocalPortForwardPort
	remotePort := config.Minio.Service.Port

	pf, err := portforward.SetupPortForward(k8sClient, cliCtx.Config.Namespace, serviceName, localPort, remotePort, log)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	// Create S3 client
	endpoint := fmt.Sprintf("http://localhost:%d", pf.LocalPort)
	s3Client, err := s3client.NewClient(endpoint, config.Minio.AccessKey, config.Minio.SecretKey)
	if err != nil {
		return err
	}

	// List objects in bucket
	bucket := config.Stackgraph.Bucket
	prefix := config.Stackgraph.S3Prefix
	archiveSplitSize := config.Stackgraph.ArchiveSplitSize

	log.Infof("Listing Stackgraph backups in bucket '%s'...", bucket)

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	result, err := s3Client.ListObjectsV2(context.Background(), input)
	if err != nil {
		return fmt.Errorf("failed to list S3 objects: %w", err)
	}

	// Filter objects based on archive split size
	filteredObjects := s3client.FilterBackupObjects(result.Contents, archiveSplitSize)

	// Sort by LastModified time (most recent first)
	sort.Slice(filteredObjects, func(i, j int) bool {
		return filteredObjects[i].LastModified.After(filteredObjects[j].LastModified)
	})

	// Format and print backups
	formatter := output.NewFormatter(cliCtx.Config.OutputFormat)

	if len(filteredObjects) == 0 {
		formatter.PrintMessage("No backups found")
		return nil
	}

	table := output.Table{
		Headers: []string{"NAME", "LAST MODIFIED", "SIZE"},
		Rows:    make([][]string, 0, len(filteredObjects)),
	}

	for _, obj := range filteredObjects {
		row := []string{
			obj.Key,
			obj.LastModified.Format("2006-01-02 15:04:05 MST"),
			formatBytes(obj.Size),
		}
		table.Rows = append(table.Rows, row)
	}

	return formatter.PrintTable(table)
}

// formatBytes formats bytes to human-readable format without spaces (e.g., "624MiB")
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	return fmt.Sprintf("%.0f%s", float64(bytes)/float64(div), units[exp])
}
