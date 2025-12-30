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
	jobNameTemplate          = "victoriametrics-restore"
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
		Short: "Restore VictoriaMetrics from a backup archive",
		Long:  `Restore VictoriaMetrics data from a backup archive stored in S3/Minio. Can use --latest or --archive to specify which backup to restore.`,
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runRestore, cmdutils.MinioIsRequired)
		},
	}

	cmd.Flags().StringVar(&archiveName, "archive", "", "Specific archive to restore (e.g., sts-victoria-metrics-backup/victoria-metrics-0-20251030152500)")
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
		appCtx.Logger.Warningf("WARNING: Restoring from backup will PURGE all existing VictoriaMetrics data!")
		appCtx.Logger.Warningf("This operation cannot be undone.")
		appCtx.Logger.Println()
		appCtx.Logger.Infof("Backup to restore: %s", backupFile)
		appCtx.Logger.Infof("Namespace: %s", appCtx.Namespace)
		appCtx.Logger.Println()

		if !restore.PromptForConfirmation() {
			return fmt.Errorf("restore operation cancelled by user")
		}
	}

	// Scale down workload before restore
	appCtx.Logger.Println()
	scaleDownLabelSelector := appCtx.Config.VictoriaMetrics.Restore.ScaleDownLabelSelector
	scaledStatefulSets, err := scale.ScaleDown(appCtx.K8sClient, appCtx.Namespace, scaleDownLabelSelector, appCtx.Logger)
	if err != nil {
		return err
	}

	// Ensure workload are scaled back up on exit (even if restore fails)
	defer func() {
		if len(scaledStatefulSets) > 0 && !background {
			appCtx.Logger.Println()
			if err := scale.ScaleUpFromAnnotations(appCtx.K8sClient, appCtx.Namespace, scaleDownLabelSelector, appCtx.Logger); err != nil {
				appCtx.Logger.Warningf("Failed to scale up workload: %v", err)
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
		restore.PrintRunningJobStatus(appCtx.Logger, "victoria-metrics", jobName, appCtx.Namespace, 0)
		return nil
	}

	return waitAndCleanupRestoreJob(appCtx.K8sClient, appCtx.Namespace, jobName, appCtx.Logger)
}

// waitAndCleanupRestoreJob waits for job completion and cleans up resources
func waitAndCleanupRestoreJob(k8sClient *k8s.Client, namespace, jobName string, log *logger.Logger) error {
	restore.PrintWaitingMessage(log, "victoria-metrics", jobName, namespace)
	return restore.WaitAndCleanup(k8sClient, namespace, jobName, log, false)
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

	var vmBackups []s3client.Object
	// List objects in bucket
	log.Infof("Listing VictoriaMetrics backups in bucket ...")
	for _, s3Location := range config.VictoriaMetrics.S3Locations {
		bucket := s3Location.Bucket
		prefix := s3Location.Prefix

		input := &s3.ListObjectsV2Input{
			Bucket:    aws.String(bucket),
			Prefix:    aws.String(prefix),
			Delimiter: aws.String("/"),
		}

		result, err := s3Client.ListObjectsV2(context.Background(), input)
		if err != nil {
			return "", fmt.Errorf("failed to list S3 objects: %w", err)
		}

		for _, key := range s3client.FilterByCommonPrefix(result.CommonPrefixes) {
			vmBackups = append(vmBackups, s3client.Object{
				Key:          fmt.Sprintf("%s/%s", bucket, key.Key),
				LastModified: getVMBackupTime(s3Client, bucket, key.Key),
			})
		}
	}

	if len(vmBackups) == 0 {
		return "", fmt.Errorf("no backups found")
	}

	sort.Slice(vmBackups, func(i, j int) bool {
		return vmBackups[i].LastModified.After(vmBackups[j].LastModified)
	})

	return vmBackups[0].Key, nil
}

// createRestoreJob creates a Kubernetes Job for restoring from backup
func createRestoreJob(k8sClient *k8s.Client, namespace, jobName, backupFile string, config *config.Config) error {
	defaultMode := int32(configMapDefaultFileMode)

	// Merge common labels with resource-specific labels
	jobLabels := k8s.MergeLabels(config.Kubernetes.CommonLabels, config.VictoriaMetrics.Restore.Job.Labels)

	// Build job spec using configuration
	spec := k8s.JobSpec{
		Name:             jobName,
		Labels:           jobLabels,
		ImagePullSecrets: k8s.ConvertImagePullSecrets(config.VictoriaMetrics.Restore.Job.ImagePullSecrets),
		SecurityContext:  k8s.ConvertPodSecurityContext(&config.VictoriaMetrics.Restore.Job.SecurityContext),
		NodeSelector:     config.VictoriaMetrics.Restore.Job.NodeSelector,
		Tolerations:      k8s.ConvertTolerations(config.VictoriaMetrics.Restore.Job.Tolerations),
		Affinity:         k8s.ConvertAffinity(config.VictoriaMetrics.Restore.Job.Affinity),
		Containers:       buildRestoreContainers(backupFile, config),
		InitContainers:   buildRestoreInitContainers(config),
		Volumes:          buildRestoreVolumes(config, defaultMode),
	}

	// Create job
	_, err := k8sClient.CreateJob(namespace, spec)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	return nil
}

// buildRestoreEnvVars constructs environment variables for the restore job
func buildRestoreEnvVars(config *config.Config) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "MINIO_ENDPOINT", Value: fmt.Sprintf("%s:%d", config.Minio.Service.Name, config.Minio.Service.Port)},
	}
}

// buildRestoreVolumeMounts constructs volume mounts for the restore job container
func buildRestoreVolumeMounts(vmPvc string) []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "backup-restore-scripts", MountPath: "/backup-restore-scripts"},
		{Name: "minio-keys", MountPath: "/aws-keys"},
		{Name: vmPvc, MountPath: "/storage"},
	}
}

// buildRestoreInitContainers constructs init containers for the restore job
func buildRestoreInitContainers(config *config.Config) []corev1.Container {
	return []corev1.Container{
		{
			Name:            "wait",
			Image:           config.VictoriaMetrics.Restore.Job.WaitImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{
				"sh",
				"-c",
				fmt.Sprintf("/entrypoint -c %s:%d -t 300", config.Minio.Service.Name, config.Minio.Service.Port),
			},
		},
	}
}

// buildRestoreVolumes constructs volumes for the restore job pod
func buildRestoreVolumes(config *config.Config, defaultMode int32) []corev1.Volume {
	volumes := []corev1.Volume{
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
			Name: vmPvcName(config.VictoriaMetrics.Restore.PersistentVolumeClaimPrefix, 0),
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: vmPvcName(config.VictoriaMetrics.Restore.PersistentVolumeClaimPrefix, 0),
				},
			},
		},
	}

	if config.VictoriaMetrics.Restore.HaMode == vmHaMirrorMode {
		v := corev1.Volume{
			Name: vmPvcName(config.VictoriaMetrics.Restore.PersistentVolumeClaimPrefix, 1),
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: vmPvcName(config.VictoriaMetrics.Restore.PersistentVolumeClaimPrefix, 1),
				},
			},
		}
		volumes = append(volumes, []corev1.Volume{v}...)
	}

	return volumes
}

// buildRestoreContainers constructs containers for the restore job
func buildRestoreContainers(backupFile string, config *config.Config) []corev1.Container {
	containers := []corev1.Container{
		{
			Name:            "restore",
			Image:           config.VictoriaMetrics.Restore.Job.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: k8s.ConvertSecurityContext(config.VictoriaMetrics.Restore.Job.ContainerSecurityContext),
			Command: []string{
				"sh",
				"/backup-restore-scripts/restore-victoria-metrics-backup.sh",
				backupFile,
				"127.0.0.1:8420",
			},
			Env:          buildRestoreEnvVars(config),
			Resources:    k8s.ConvertResources(config.VictoriaMetrics.Restore.Job.Resources),
			VolumeMounts: buildRestoreVolumeMounts(vmPvcName(config.VictoriaMetrics.Restore.PersistentVolumeClaimPrefix, 0)),
		},
	}

	if config.VictoriaMetrics.Restore.HaMode == vmHaMirrorMode {
		containers = append(containers, []corev1.Container{
			{
				Name:            "restore-1",
				Image:           config.VictoriaMetrics.Restore.Job.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				SecurityContext: k8s.ConvertSecurityContext(config.VictoriaMetrics.Restore.Job.ContainerSecurityContext),
				Command: []string{
					"sh",
					"/backup-restore-scripts/restore-victoria-metrics-backup.sh",
					backupFile,
					"127.0.0.1:8421",
				},
				Env:          buildRestoreEnvVars(config),
				Resources:    k8s.ConvertResources(config.VictoriaMetrics.Restore.Job.Resources),
				VolumeMounts: buildRestoreVolumeMounts(vmPvcName(config.VictoriaMetrics.Restore.PersistentVolumeClaimPrefix, 1)),
			},
		}...)
	}
	return containers
}

func vmPvcName(prefix string, instance int) string {
	return fmt.Sprintf("%svictoria-metrics-%d-0", prefix, instance)
}
