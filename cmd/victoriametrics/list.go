package victoriametrics

import (
	"context"
	"fmt"
	"sort"
	"time"

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
	vmBackupSuccessFile = "backup_complete.ignore"
	vmHaMirrorMode      = "mirror"
)

func listCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available VictoriaMetrics backups from S3/Minio",
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

	var vmBackups []s3client.Object
	// List objects in bucket
	appCtx.Logger.Infof("Listing VictoriaMetrics backups in bucket ...")
	if appCtx.Config.VictoriaMetrics.Restore.HaMode == vmHaMirrorMode {
		appCtx.Logger.Println()
		appCtx.Logger.Infof("NOTE: In HA mode, backups from both instances (victoria-metrics-0 and victoria-metrics-1) are listed.")
		appCtx.Logger.Infof("      The restore command accepts either backup to restore both instances.")
	}
	appCtx.Logger.Println()
	for _, s3Location := range appCtx.Config.VictoriaMetrics.S3Locations {
		bucket := s3Location.Bucket
		prefix := s3Location.Prefix

		input := &s3.ListObjectsV2Input{
			Bucket:    aws.String(bucket),
			Prefix:    aws.String(prefix),
			Delimiter: aws.String("/"),
		}

		result, err := s3Client.ListObjectsV2(context.Background(), input)
		if err != nil {
			return fmt.Errorf("failed to list S3 objects: %w", err)
		}

		for _, key := range s3client.FilterByCommonPrefix(result.CommonPrefixes) {
			vmBackups = append(vmBackups, s3client.Object{
				Key:          fmt.Sprintf("%s/%s", bucket, key.Key),
				LastModified: getVMBackupTime(s3Client, bucket, key.Key),
			})
		}
	}

	if len(vmBackups) == 0 {
		appCtx.Formatter.PrintMessage("No backups found")
		return nil
	}

	sort.Slice(vmBackups, func(i, j int) bool {
		return vmBackups[i].LastModified.After(vmBackups[j].LastModified)
	})

	table := output.Table{
		Headers: []string{"NAME ({bucket}/{instance}-{created})", "UPDATED"},
		Rows:    make([][]string, 0, len(vmBackups)),
	}

	for _, obj := range vmBackups {
		row := []string{
			obj.Key,
			obj.LastModified.Format("2006-01-02 15:04:05 MST"),
		}
		table.Rows = append(table.Rows, row)
	}

	return appCtx.Formatter.PrintTable(table)
}

// getVMBackupTime extracts timestamp from the VM backup name
// The expected format is: victoria-metrics-(0|1)-20251030143500
func getVMBackupTime(s3client s3client.Interface, bucket, key string) time.Time {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(key + "/" + vmBackupSuccessFile),
	}

	result, err := s3client.ListObjectsV2(context.Background(), input)
	if err != nil || len(result.Contents) != 1 {
		return time.Time{}
	}
	vmbackup := result.Contents[0]

	return *vmbackup.LastModified
}
