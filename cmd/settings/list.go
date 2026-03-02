package settings

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	s3client "github.com/stackvista/stackstate-backup-cli/internal/clients/s3"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/output"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
	corev1 "k8s.io/api/core/v1"
)

const (
	isMultiPartArchive            = false
	expectedListJobPodCount       = 1
	expectedListJobContainerCount = 1
)

func listCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available Settings backups from S3/Minio",
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runList, cmdutils.MinioIsNotRequired)
		},
	}
}

func runList(appCtx *app.Context) error {
	backups, err := getAllBackups(appCtx)
	if err != nil {
		return err
	}

	if len(backups) == 0 {
		appCtx.Formatter.PrintMessage("No backups found")
		return nil
	}
	table := output.Table{
		Headers: []string{"NAME", "LAST MODIFIED", "SIZE"},
		Rows:    make([][]string, 0, len(backups)),
	}

	for _, obj := range backups {
		row := []string{
			obj.Filename,
			obj.LastModified.Format("2006-01-02 15:04:05 MST"),
			output.FormatBytes(obj.Size),
		}
		table.Rows = append(table.Rows, row)
	}

	return appCtx.Formatter.PrintTable(table)
}

// getAllBackups retrieves backups from all sources (S3 and PVC), deduplicates and sorts them by LastModified time (most recent first)
func getAllBackups(appCtx *app.Context) ([]BackupFileInfo, error) {
	var backups []BackupFileInfo
	var err error

	// Get backups from S3 if enabled
	if appCtx.Config.Minio.Enabled {
		if backups, err = getBackupListFromS3(appCtx); err != nil {
			return nil, fmt.Errorf("failed to get list of backups from Minio: %v", err)
		}
	}

	// Get backups from PVC
	backupsFromPvc, err := getBackupListFromPVC(appCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of backups from PVC: %v", err)
	}
	backups = append(backups, backupsFromPvc...)

	if len(backups) == 0 {
		return []BackupFileInfo{}, nil
	}

	// Sort by name for deduplication
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Filename < backups[j].Filename
	})

	// Deduplicate by filename
	backups = slices.CompactFunc(backups, func(i, j BackupFileInfo) bool {
		return i.Filename == j.Filename
	})

	// Sort by LastModified time (most recent first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].LastModified.After(backups[j].LastModified)
	})

	return backups, nil
}

// BackupFileInfo represents metadata for a backup file
type BackupFileInfo struct {
	LastModified time.Time // Unix timestamp with nanoseconds
	Filename     string    // Name of the backup file
	Size         int64     // File size in bytes
}

func getBackupListFromS3(appCtx *app.Context) ([]BackupFileInfo, error) {
	// Setup port-forward to Minio
	serviceName := appCtx.Config.Minio.Service.Name
	localPort := appCtx.Config.Minio.Service.LocalPortForwardPort
	remotePort := appCtx.Config.Minio.Service.Port

	pf, err := portforward.SetupPortForward(appCtx.K8sClient, appCtx.Namespace, serviceName, localPort, remotePort, appCtx.Logger)
	if err != nil {
		return nil, err
	}
	defer close(pf.StopChan)

	// List objects in bucket
	bucket := appCtx.Config.Settings.Bucket
	prefix := appCtx.Config.Settings.S3Prefix

	appCtx.Logger.Infof("Listing Settings backups in bucket '%s'...", bucket)

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	result, err := appCtx.S3Client.ListObjectsV2(context.Background(), input)
	if err != nil {
		return nil, fmt.Errorf("failed to list S3 objects: %w", err)
	}

	// Filter objects based on whether the archive is split or not
	filteredObjects := s3client.FilterBackupObjects(result.Contents, isMultiPartArchive)

	var backups []BackupFileInfo
	for _, obj := range filteredObjects {
		row := BackupFileInfo{
			Filename:     strings.TrimPrefix(obj.Key, prefix),
			LastModified: obj.LastModified,
			Size:         obj.Size,
		}
		backups = append(backups, row)
	}
	return backups, nil
}

func getBackupListFromPVC(appCtx *app.Context) ([]BackupFileInfo, error) {
	// Setup Kubernetes resources for list job
	appCtx.Logger.Println()
	if err := restore.EnsureResources(appCtx.K8sClient, appCtx.Namespace, appCtx.Config, appCtx.Logger); err != nil {
		return nil, err
	}

	// Create list job
	appCtx.Logger.Println()
	appCtx.Logger.Infof("Creating job to list Settings backups stored on PVC...")

	jobName := fmt.Sprintf("%s-%s", listJobNameTemplate, time.Now().Format("20060102t150405"))

	if err := createListJob(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Config); err != nil {
		return nil, fmt.Errorf("failed to create list job: %w", err)
	}

	appCtx.Logger.Successf("List job created: %s", jobName)

	defer func() {
		err := restore.CleanupResources(appCtx.K8sClient, appCtx.Namespace, jobName, "", appCtx.Logger, false)
		if err != nil {
			appCtx.Logger.Errorf("failed to clean up resources: %s", err)
		}
	}()

	if err := restore.WaitForJobCompletion(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger, appCtx.JobTimeout); err != nil {
		return nil, err
	}

	appCtx.Logger.Println()
	appCtx.Logger.Successf("List job completed successfully")

	jobPodLogs, err := appCtx.K8sClient.GetJobLogs(appCtx.Namespace, jobName)
	if err != nil {
		return nil, err
	}

	podLogsCount := len(jobPodLogs)
	if podLogsCount != expectedListJobPodCount {
		appCtx.Logger.Errorf("Expected exactly 1 pod log from the list job, got %d", podLogsCount)
		return nil, errors.New("fail to get backups from the list job")
	}

	containerLogsCount := len(jobPodLogs[0].ContainerLogs)
	if containerLogsCount != expectedListJobContainerCount {
		appCtx.Logger.Errorf("Expected exactly 2 container log from the list job, got %d", containerLogsCount)
		return nil, errors.New("fail to get backups from the list job")
	}

	files, err := ParseListJobOutput(jobPodLogs[0].ContainerLogs[0].Logs)
	if err != nil {
		fmt.Printf("Error parsing files: %v\n", err)
		return nil, fmt.Errorf("failed to parse list job output: %w", err)
	}

	return files, nil
}

// createListJob creates a Kubernetes Job and PVC for listing Settings backups from PVC
func createListJob(k8sClient *k8s.Client, namespace string, jobName string, config *config.Config) error {
	defaultMode := int32(configMapDefaultFileMode)

	// Merge common labels with resource-specific labels
	jobLabels := k8s.MergeLabels(config.Kubernetes.CommonLabels, config.Settings.Restore.Job.Labels)

	listEnvVar := buildEnvVar([]corev1.EnvVar{}, config)

	// Build job spec using configuration
	spec := k8s.JobSpec{
		Name:             jobName,
		Labels:           jobLabels,
		ImagePullSecrets: k8s.ConvertImagePullSecrets(config.Settings.Restore.Job.ImagePullSecrets),
		SecurityContext:  k8s.ConvertPodSecurityContext(&config.Settings.Restore.Job.SecurityContext),
		NodeSelector:     config.Settings.Restore.Job.NodeSelector,
		Tolerations:      k8s.ConvertTolerations(config.Settings.Restore.Job.Tolerations),
		Affinity:         k8s.ConvertAffinity(config.Settings.Restore.Job.Affinity),
		Containers:       []corev1.Container{buildContainer(listEnvVar, []string{"bash", "-c", "find /settings-backup-data/ -maxdepth 1 -type f -printf '%T@ %f %s\n'"}, config)},
		Volumes:          buildVolumes(config, defaultMode),
	}

	// Create job
	if _, err := k8sClient.CreateJob(namespace, spec); err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	return nil
}

// ParseListJobOutput parses a multiline string containing backup file information
func ParseListJobOutput(input string) ([]BackupFileInfo, error) {
	var files []BackupFileInfo

	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Split the line into parts
		parts := strings.Fields(line)
		if len(parts) != 3 { //nolint:mnd
			return nil, fmt.Errorf("invalid line format: expected 3 fields, got %d", len(parts))
		}

		// Parse Unix timestamp (ignore fractional part)
		timestampFloat, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}

		// Convert to time.Time (truncate to seconds)
		timestamp := time.Unix(int64(timestampFloat), 0).UTC()

		// Parse file size
		size, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file size: %w", err)
		}

		files = append(files, BackupFileInfo{
			LastModified: timestamp,
			Filename:     parts[1],
			Size:         size,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading input: %w", err)
	}

	return files, nil
}
