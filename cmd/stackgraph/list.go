package stackgraph

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	s3client "github.com/stackvista/stackstate-backup-cli/internal/clients/s3"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/output"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
)

func listCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available Stackgraph backups from S3/Minio",
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runList, cmdutils.MinioIsRequired)
		},
	}
}

func runList(appCtx *app.Context) error {
	// Setup port-forward to Minio
	serviceName := appCtx.Config.Minio.Service.Name
	localPort := appCtx.Config.Minio.Service.LocalPortForwardPort
	remotePort := appCtx.Config.Minio.Service.Port

	pf, err := portforward.SetupPortForward(appCtx.K8sClient, appCtx.Namespace, serviceName, localPort, remotePort, appCtx.Logger)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	// List objects in bucket
	bucket := appCtx.Config.Stackgraph.Bucket
	prefix := appCtx.Config.Stackgraph.S3Prefix
	multipartArchive := appCtx.Config.Stackgraph.MultipartArchive

	appCtx.Logger.Infof("Listing Stackgraph backups in bucket '%s'...", bucket)

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	result, err := appCtx.S3Client.ListObjectsV2(context.Background(), input)
	if err != nil {
		return fmt.Errorf("failed to list S3 objects: %w", err)
	}

	// Filter objects based on whether the archive is split or not
	filteredObjects := s3client.FilterBackupObjects(result.Contents, multipartArchive)

	// Sort by LastModified time (most recent first)
	sort.Slice(filteredObjects, func(i, j int) bool {
		return filteredObjects[i].LastModified.After(filteredObjects[j].LastModified)
	})

	if len(filteredObjects) == 0 {
		appCtx.Formatter.PrintMessage("No backups found")
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
			output.FormatBytes(obj.Size),
		}
		table.Rows = append(table.Rows, row)
	}

	return appCtx.Formatter.PrintTable(table)
}
