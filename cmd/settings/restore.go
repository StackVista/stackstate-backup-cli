package settings

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	s3client "github.com/stackvista/stackstate-backup-cli/internal/clients/s3"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
	corev1 "k8s.io/api/core/v1"
)

const (
	jobNameTemplate          = "settings-restore"
	configMapDefaultFileMode = 0755
)

// Restore command flags
var (
	archiveName      string
	useLatest        bool
	background       bool
	skipConfirmation bool
)

func restoreCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore Settings from a backup archive",
		Long:  `Restore Settings data from a backup archive stored in S3/Minio. Can use --latest or --archive to specify which backup to restore.`,
		Run: func(_ *cobra.Command, _ []string) {
			appCtx, err := app.NewContext(globalFlags)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if err := runRestore(appCtx); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&archiveName, "archive", "", "Specific archive name to restore (e.g., sts-backup-20251117-1404.sty)")
	cmd.Flags().BoolVar(&useLatest, "latest", false, "Restore from the most recent backup")
	cmd.Flags().BoolVar(&background, "background", false, "Run restore job in background without waiting for completion")
	cmd.Flags().BoolVarP(&skipConfirmation, "yes", "y", false, "Skip confirmation prompt")
	cmd.MarkFlagsMutuallyExclusive("archive", "latest")
	cmd.MarkFlagsOneRequired("archive", "latest")

	return cmd
}

func runRestore(appCtx *app.Context) error {
	// Determine which archive to restore
	backupFile := archiveName
	if useLatest {
		appCtx.Logger.Infof("Finding latest backup...")
		latest, err := getLatestBackup(appCtx.K8sClient, appCtx.Namespace, appCtx.Config, appCtx.Logger)
		if err != nil {
			return err
		}
		backupFile = latest
		appCtx.Logger.Infof("Using latest backup: %s", backupFile)
	}

	// Warn user and ask for confirmation
	if !skipConfirmation {
		appCtx.Logger.Println()
		appCtx.Logger.Warningf("WARNING: Restoring from backup will PURGE all existing Stackgraph (Topology) data!")
		appCtx.Logger.Warningf("This operation cannot be undone.")
		appCtx.Logger.Println()
		appCtx.Logger.Infof("Backup to restore: %s", backupFile)
		appCtx.Logger.Infof("Namespace: %s", appCtx.Namespace)
		appCtx.Logger.Println()

		if !restore.PromptForConfirmation() {
			return fmt.Errorf("restore operation cancelled by user")
		}
	}

	// Scale down deployments before restore
	appCtx.Logger.Println()
	scaleDownLabelSelector := appCtx.Config.Settings.Restore.ScaleDownLabelSelector
	scaledDeployments, err := scale.ScaleDown(appCtx.K8sClient, appCtx.Namespace, scaleDownLabelSelector, appCtx.Logger)
	if err != nil {
		return err
	}

	// Ensure deployments are scaled back up on exit (even if restore fails)
	defer func() {
		if len(scaledDeployments) > 0 && !background {
			appCtx.Logger.Println()
			if err := scale.ScaleUpFromAnnotations(appCtx.K8sClient, appCtx.Namespace, scaleDownLabelSelector, appCtx.Logger); err != nil {
				appCtx.Logger.Warningf("Failed to scale up deployments: %v", err)
			}
		}
	}()

	// Setup Kubernetes resources for restore job
	appCtx.Logger.Println()
	if err := restore.EnsureRestoreResources(appCtx.K8sClient, appCtx.Namespace, appCtx.Config, appCtx.Logger); err != nil {
		return err
	}

	// Create restore job
	appCtx.Logger.Println()
	appCtx.Logger.Infof("Creating restore job for backup: %s", backupFile)

	jobName := fmt.Sprintf("%s-%s", jobNameTemplate, time.Now().Format("20060102t150405"))

	if err = createRestoreJob(appCtx.K8sClient, appCtx.Namespace, jobName, backupFile, appCtx.Config); err != nil {
		return fmt.Errorf("failed to create restore job: %w", err)
	}

	appCtx.Logger.Successf("Restore job created: %s", jobName)

	if background {
		restore.PrintRunningJobStatus(appCtx.Logger, "settings", jobName, appCtx.Namespace, 0)
		return nil
	}

	return waitAndCleanupRestoreJob(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger)
}

// waitAndCleanupRestoreJob waits for job completion and cleans up resources
func waitAndCleanupRestoreJob(k8sClient *k8s.Client, namespace, jobName string, log *logger.Logger) error {
	restore.PrintWaitingMessage(log, "settings", jobName, namespace)
	return restore.WaitAndCleanup(k8sClient, namespace, jobName, log, true)
}

// getLatestBackup retrieves the most recent backup from S3
func getLatestBackup(k8sClient *k8s.Client, namespace string, config *config.Config, log *logger.Logger) (string, error) {
	// Setup port-forward to Minio
	serviceName := config.Minio.Service.Name
	localPort := config.Minio.Service.LocalPortForwardPort
	remotePort := config.Minio.Service.Port

	pf, err := portforward.SetupPortForward(k8sClient, namespace, serviceName, localPort, remotePort, log)
	if err != nil {
		return "", err
	}
	defer close(pf.StopChan)

	// Create S3 client
	endpoint := fmt.Sprintf("http://localhost:%d", pf.LocalPort)
	s3Client, err := s3client.NewClient(endpoint, config.Minio.AccessKey, config.Minio.SecretKey)
	if err != nil {
		return "", err
	}

	// List objects in bucket
	bucket := config.Settings.Bucket
	prefix := config.Settings.S3Prefix

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	result, err := s3Client.ListObjectsV2(context.Background(), input)
	if err != nil {
		return "", fmt.Errorf("failed to list S3 objects: %w", err)
	}

	// Filter objects based on whether the archive is split or not
	filteredObjects := s3client.FilterBackupObjects(result.Contents, isMultiPartArchive)

	if len(filteredObjects) == 0 {
		return "", fmt.Errorf("no backups found in bucket %s", bucket)
	}

	// Sort by LastModified time (most recent first)
	sort.Slice(filteredObjects, func(i, j int) bool {
		return filteredObjects[i].LastModified.After(filteredObjects[j].LastModified)
	})

	return filteredObjects[0].Key, nil
}

// createRestoreJob creates a Kubernetes Job and PVC for restoring from backup
func createRestoreJob(k8sClient *k8s.Client, namespace, jobName, backupFile string, config *config.Config) error {
	defaultMode := int32(configMapDefaultFileMode)

	// Merge common labels with resource-specific labels
	jobLabels := k8s.MergeLabels(config.Kubernetes.CommonLabels, config.Settings.Restore.Job.Labels)

	// Build job spec using configuration
	spec := k8s.JobSpec{
		Name:             jobName,
		Labels:           jobLabels,
		ImagePullSecrets: k8s.ConvertImagePullSecrets(config.Settings.Restore.Job.ImagePullSecrets),
		SecurityContext:  k8s.ConvertPodSecurityContext(&config.Settings.Restore.Job.SecurityContext),
		NodeSelector:     config.Settings.Restore.Job.NodeSelector,
		Tolerations:      k8s.ConvertTolerations(config.Settings.Restore.Job.Tolerations),
		Affinity:         k8s.ConvertAffinity(config.Settings.Restore.Job.Affinity),
		Containers:       buildContainers(backupFile, []string{"/backup-restore-scripts/restore-settings-backup.sh"}, config),
		InitContainers:   buildInitContainers(config),
		Volumes:          buildVolumes(config, defaultMode),
	}

	// Create job
	if _, err := k8sClient.CreateJob(namespace, spec); err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	return nil
}

// buildEnvVars constructs environment variables for the restore job
func buildEnvVars(backupFile string, config *config.Config) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "BACKUP_FILE", Value: backupFile},
		{Name: "BACKUP_CONFIGURATION_BUCKET_NAME", Value: config.Settings.Bucket},
		{Name: "BACKUP_CONFIGURATION_S3_PREFIX", Value: config.Settings.S3Prefix},
		{Name: "MINIO_ENDPOINT", Value: fmt.Sprintf("%s:%d", config.Minio.Service.Name, config.Minio.Service.Port)},
		{Name: "STACKSTATE_BASE_URL", Value: config.Settings.Restore.BaseURL},
		{Name: "RECEIVER_BASE_URL", Value: config.Settings.Restore.ReceiverBaseURL},
		{Name: "PLATFORM_VERSION", Value: config.Settings.Restore.PlatformVersion},
		{Name: "ZOOKEEPER_QUORUM", Value: config.Settings.Restore.ZookeeperQuorum},
	}
}

// buildVolumeMounts constructs volume mounts for the restore job container
func buildVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "backup-log", MountPath: "/opt/docker/etc_log"},
		{Name: "backup-restore-scripts", MountPath: "/backup-restore-scripts"},
		{Name: "minio-keys", MountPath: "/aws-keys"},
		{Name: "tmp-data", MountPath: "/tmp-data"},
	}
}

// buildInitContainers constructs init containers for the restore job
func buildInitContainers(config *config.Config) []corev1.Container {
	return []corev1.Container{
		{
			Name:            "wait",
			Image:           config.Settings.Restore.Job.WaitImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{
				"sh",
				"-c",
				fmt.Sprintf("/entrypoint -c %s:%d -t 300", config.Minio.Service.Name, config.Minio.Service.Port),
			},
			SecurityContext: k8s.ConvertSecurityContext(config.Settings.Restore.Job.ContainerSecurityContext),
		},
	}
}

// buildVolumes constructs volumes for the restore job pod
func buildVolumes(config *config.Config, defaultMode int32) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "backup-log",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: config.Settings.Restore.LoggingConfigConfigMapName,
					},
				},
			},
		},
		{
			Name: "backup-restore-scripts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: restore.RestoreScriptsConfigMap,
					},
					DefaultMode: &defaultMode,
				},
			},
		},
		{
			Name: "minio-keys",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: restore.MinioKeysSecretName,
				},
			},
		},
		{
			Name: "tmp-data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
}

// buildContainers constructs containers for the restore job
func buildContainers(backupFile string, command []string, config *config.Config) []corev1.Container {
	return []corev1.Container{
		{
			Name:            "restore",
			Image:           config.Settings.Restore.Job.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: k8s.ConvertSecurityContext(config.Settings.Restore.Job.ContainerSecurityContext),
			Command:         command,
			Env:             buildEnvVars(backupFile, config),
			Resources:       k8s.ConvertResources(config.Settings.Restore.Job.Resources),
			VolumeMounts:    buildVolumeMounts(),
		},
	}
}
