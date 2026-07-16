package stackgraphv2

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

const (
	backupFileNameRegex = `^sts-backup-.*\.graph.v2$`
)

func listCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available Stackgraph backups (v2) from S3",
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runList, cmdutils.StorageIsRequired)
		},
	}
}

func runList(appCtx *app.Context) error {
	// Setup port-forward to S3-compatible storage
	storageService := appCtx.Config.GetStorageService()
	serviceName := storageService.Name
	remotePort := storageService.Port

	pf, err := portforward.SetupPortForward(appCtx.K8sClient, appCtx.Namespace, serviceName, remotePort, appCtx.Logger)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	// Create S3 client with actual port
	s3Client, err := appCtx.NewS3Client(pf.LocalPort)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	// List objects in bucket
	bucket := appCtx.Config.Stackgraph.Bucket
	prefix := appCtx.Config.Stackgraph.S3Prefix + "v2/"

	appCtx.Logger.Infof("Listing Stackgraph backups in bucket '%s' with prefix '%s'...", bucket, prefix)

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	}

	result, err := s3Client.ListObjectsV2(context.Background(), input)
	if err != nil {
		return fmt.Errorf("failed to list S3 objects: %w", err)
	}

	filteredObjects := s3client.ConvertBackupObjects(result.Contents)

	// Filter to only include direct children of the prefix that match the backup filename pattern,
	// and strip the prefix from the key
	filteredObjects, err = s3client.FilterByPrefixAndRegex(filteredObjects, prefix, backupFileNameRegex)
	if err != nil {
		return fmt.Errorf("failed to filter objects: %w", err)
	}

	// Sort by LastModified time (most recent first)
	sort.Slice(filteredObjects, func(i, j int) bool {
		return filteredObjects[i].LastModified.After(filteredObjects[j].LastModified)
	})

	if len(filteredObjects) == 0 {
		appCtx.Formatter.PrintMessage("No backups found")
		return nil
	}

	table := output.Table{
		Headers: []string{"NAME", "LAST MODIFIED"},
		Rows:    make([][]string, 0, len(filteredObjects)),
	}

	for _, obj := range filteredObjects {
		row := []string{
			strings.TrimPrefix(obj.Key, prefix),
			obj.LastModified.Format("2006-01-02 15:04:05 MST"),
		}
		table.Rows = append(table.Rows, row)
	}

	return appCtx.Formatter.PrintTable(table)
}
