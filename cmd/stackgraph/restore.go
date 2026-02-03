package stackgraph

import (
	"context"
	"fmt"
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
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
	corev1 "k8s.io/api/core/v1"
)

const (
	jobNameTemplate          = "stackgraph-restore"
	configMapDefaultFileMode = 0755
	purgeStackgraphDataFlag  = "-force"
)

// Restore command flags
var (
	archiveName      string
	useLatest        bool
	background       bool
	skipConfirmation bool
	skipStackpacks   bool
)

func restoreCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore Stackgraph from a backup archive",
		Long:  `Restore Stackgraph data from a backup archive stored in S3. Automatically also restores Stackpacks backup that was made at the same time, it can be skipped with --skip-stackpacks. Can use --latest or --archive to specify which backup to restore.`,
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runRestore, cmdutils.StorageIsRequired)
		},
	}

	cmd.Flags().StringVar(&archiveName, "archive", "", "Specific archive name to restore (e.g., sts-backup-20210216-0300.graph)")
	cmd.Flags().BoolVar(&useLatest, "latest", false, "Restore from the most recent backup")
	cmd.Flags().BoolVar(&background, "background", false, "Run restore job in background without waiting for completion")
	cmd.Flags().BoolVarP(&skipConfirmation, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&skipStackpacks, "skip-stackpacks", false, "Skip restoring stackpacks backup")
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
		appCtx.Logger.Warningf("WARNING: Restoring from backup will PURGE all existing Stackgraph data!")
		appCtx.Logger.Warningf("This operation cannot be undone.")
		appCtx.Logger.Println()
		appCtx.Logger.Infof("Backup to restore: %s", backupFile)
		appCtx.Logger.Infof("Namespace: %s", appCtx.Namespace)
		appCtx.Logger.Println()

		if !restore.PromptForConfirmation() {
			return fmt.Errorf("restore operation cancelled by user")
		}
	}

	// Scale down deployments before restore (with lock protection)
	appCtx.Logger.Println()
	scaleDownLabelSelector := appCtx.Config.Stackgraph.Restore.ScaleDownLabelSelector
	scaledDeployments, err := scale.ScaleDownWithLock(scale.ScaleDownWithLockParams{
		K8sClient:     appCtx.K8sClient,
		Namespace:     appCtx.Namespace,
		LabelSelector: scaleDownLabelSelector,
		Datastore:     config.DatastoreStackgraph,
		AllSelectors:  appCtx.Config.GetAllScaleDownSelectors(),
		Log:           appCtx.Logger,
	})
	if err != nil {
		return err
	}

	// Ensure deployments are scaled back up and lock released on exit (even if restore fails)
	defer func() {
		if len(scaledDeployments) > 0 && !background {
			appCtx.Logger.Println()
			if err := scale.ScaleUpAndReleaseLock(appCtx.K8sClient, appCtx.Namespace, scaleDownLabelSelector, appCtx.Logger); err != nil {
				appCtx.Logger.Warningf("Failed to scale up deployments: %v", err)
			}
		}
	}()

	// Setup Kubernetes resources for restore job
	appCtx.Logger.Println()
	if err := restore.EnsureResources(appCtx.K8sClient, appCtx.Namespace, appCtx.Config, appCtx.Logger); err != nil {
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
		restore.PrintRunningJobStatus(appCtx.Logger, "stackgraph", jobName, appCtx.Namespace, 0)
		return nil
	}

	return waitAndCleanupRestoreJob(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger)
}

// waitAndCleanupRestoreJob waits for job completion and cleans up resources
func waitAndCleanupRestoreJob(k8sClient *k8s.Client, namespace, jobName string, log *logger.Logger) error {
	restore.PrintWaitingMessage(log, "stackgraph", jobName, namespace)
	return restore.WaitAndCleanup(k8sClient, namespace, jobName, log, true)
}

// getLatestBackup retrieves the most recent backup from S3
func getLatestBackup(k8sClient *k8s.Client, namespace string, config *config.Config, log *logger.Logger) (string, error) {
	// Setup port-forward to S3-compatible storage
	storageService := config.GetStorageService()
	serviceName := storageService.Name
	remotePort := storageService.Port

	pf, err := portforward.SetupPortForward(k8sClient, namespace, serviceName, remotePort, log)
	if err != nil {
		return "", err
	}
	defer close(pf.StopChan)

	// Create S3 client
	endpoint := fmt.Sprintf("http://localhost:%d", pf.LocalPort)
	s3Client, err := s3client.NewClient(endpoint, config.GetStorageAccessKey(), config.GetStorageSecretKey())
	if err != nil {
		return "", err
	}

	// List objects in bucket
	bucket := config.Stackgraph.Bucket
	prefix := config.Stackgraph.S3Prefix
	multipartArchive := config.Stackgraph.MultipartArchive

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	result, err := s3Client.ListObjectsV2(context.Background(), input)
	if err != nil {
		return "", fmt.Errorf("failed to list S3 objects: %w", err)
	}

	// Filter objects based on whether the archive is split or not
	filteredObjects := s3client.FilterBackupObjects(result.Contents, multipartArchive)

	if len(filteredObjects) == 0 {
		return "", fmt.Errorf("no backups found in bucket %s", bucket)
	}

	// Sort by LastModified time (most recent first)
	sort.Slice(filteredObjects, func(i, j int) bool {
		return filteredObjects[i].LastModified.After(filteredObjects[j].LastModified)
	})
	latestBackup := strings.TrimPrefix(filteredObjects[0].Key, prefix)
	return latestBackup, nil
}

// buildPVCSpec builds a PVCSpec from configuration
func buildPVCSpec(name string, config *config.Config, labels map[string]string) k8s.PVCSpec {
	pvcConfig := config.Stackgraph.Restore.PVC

	// Convert string access modes to k8s types
	accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce} // default
	if len(pvcConfig.AccessModes) > 0 {
		accessModes = make([]corev1.PersistentVolumeAccessMode, 0, len(pvcConfig.AccessModes))
		for _, mode := range pvcConfig.AccessModes {
			accessModes = append(accessModes, corev1.PersistentVolumeAccessMode(mode))
		}
	}

	// Handle storage class (nil if not set)
	var storageClass *string
	if pvcConfig.StorageClassName != "" {
		storageClass = &pvcConfig.StorageClassName
	}

	return k8s.PVCSpec{
		Name:         name,
		Labels:       labels,
		StorageSize:  pvcConfig.Size,
		AccessModes:  accessModes,
		StorageClass: storageClass,
	}
}

// createRestoreJob creates a Kubernetes Job and PVC for restoring from backup
func createRestoreJob(k8sClient *k8s.Client, namespace, jobName, backupFile string, config *config.Config) error {
	defaultMode := int32(configMapDefaultFileMode)

	// Merge common labels with resource-specific labels
	pvcLabels := k8s.MergeLabels(config.Kubernetes.CommonLabels, map[string]string{})
	jobLabels := k8s.MergeLabels(config.Kubernetes.CommonLabels, config.Stackgraph.Restore.Job.Labels)

	// Create PVC first
	pvcSpec := buildPVCSpec(jobName, config, pvcLabels)
	pvc, err := k8sClient.CreatePVC(namespace, pvcSpec)
	if err != nil {
		return fmt.Errorf("failed to create PVC: %w", err)
	}

	// Build job spec using configuration
	spec := k8s.JobSpec{
		Name:             jobName,
		Labels:           jobLabels,
		ImagePullSecrets: k8s.ConvertImagePullSecrets(config.Stackgraph.Restore.Job.ImagePullSecrets),
		SecurityContext:  k8s.ConvertPodSecurityContext(&config.Stackgraph.Restore.Job.SecurityContext),
		NodeSelector:     config.Stackgraph.Restore.Job.NodeSelector,
		Tolerations:      k8s.ConvertTolerations(config.Stackgraph.Restore.Job.Tolerations),
		Affinity:         k8s.ConvertAffinity(config.Stackgraph.Restore.Job.Affinity),
		Containers:       buildRestoreContainers(backupFile, config),
		InitContainers:   buildRestoreInitContainers(config),
		Volumes:          buildRestoreVolumes(jobName, config, defaultMode),
	}

	// Create job
	_, err = k8sClient.CreateJob(namespace, spec)
	if err != nil {
		// Cleanup PVC if job creation fails
		_ = k8sClient.DeletePVC(namespace, pvc.Name)
		return fmt.Errorf("failed to create job: %w", err)
	}

	return nil
}

// buildRestoreEnvVars constructs environment variables for the restore job
func buildRestoreEnvVars(backupFile string, config *config.Config) []corev1.EnvVar {
	storageService := config.GetStorageService()
	return []corev1.EnvVar{
		{Name: "BACKUP_FILE", Value: backupFile},
		{Name: "FORCE_DELETE", Value: purgeStackgraphDataFlag},
		{Name: "BACKUP_STACKGRAPH_BUCKET_NAME", Value: config.Stackgraph.Bucket},
		{Name: "BACKUP_STACKGRAPH_S3_PREFIX", Value: config.Stackgraph.S3Prefix},
		{Name: "BACKUP_STACKGRAPH_STACKPACKS_S3_PREFIX", Value: config.Stackgraph.StackpacksS3Prefix},
		{Name: "BACKUP_STACKGRAPH_MULTIPART_ARCHIVE", Value: strconv.FormatBool(config.Stackgraph.MultipartArchive)},
		{Name: "MINIO_ENDPOINT", Value: fmt.Sprintf("%s:%d", storageService.Name, storageService.Port)},
		{Name: "ZOOKEEPER_QUORUM", Value: config.Stackgraph.Restore.ZookeeperQuorum},
		{Name: "SKIP_STACKPACKS", Value: strconv.FormatBool(skipStackpacks)},
	}
}

// buildRestoreVolumeMounts constructs volume mounts for the restore job container
func buildRestoreVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "backup-log", MountPath: "/opt/docker/etc_log"},
		{Name: "backup-restore-scripts", MountPath: "/backup-restore-scripts"},
		{Name: "minio-keys", MountPath: "/aws-keys"},
		{Name: "tmp-data", MountPath: "/tmp-data"},
	}
}

// buildRestoreInitContainers constructs init containers for the restore job
func buildRestoreInitContainers(config *config.Config) []corev1.Container {
	storageService := config.GetStorageService()
	return []corev1.Container{
		{
			Name:            "wait",
			Image:           config.Stackgraph.Restore.Job.WaitImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{
				"sh",
				"-c",
				fmt.Sprintf("/entrypoint -c %s:%d -t 300", storageService.Name, storageService.Port),
			},
			SecurityContext: k8s.ConvertSecurityContext(config.Stackgraph.Restore.Job.ContainerSecurityContext),
		},
	}
}

// buildRestoreVolumes constructs volumes for the restore job pod
func buildRestoreVolumes(jobName string, config *config.Config, defaultMode int32) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "backup-log",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: config.Stackgraph.Restore.LoggingConfigConfigMapName,
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
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: jobName,
				},
			},
		},
	}
}

// buildRestoreContainers constructs containers for the restore job
func buildRestoreContainers(backupFile string, config *config.Config) []corev1.Container {
	return []corev1.Container{
		{
			Name:            "restore",
			Image:           config.Stackgraph.Restore.Job.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: k8s.ConvertSecurityContext(config.Stackgraph.Restore.Job.ContainerSecurityContext),
			Command:         []string{"/backup-restore-scripts/restore-stackgraph-backup.sh"},
			Env:             buildRestoreEnvVars(backupFile, config),
			Resources:       k8s.ConvertResources(config.Stackgraph.Restore.Job.Resources),
			VolumeMounts:    buildRestoreVolumeMounts(),
		},
	}
}
