package settings

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
	corev1 "k8s.io/api/core/v1"
)

const (
	restoreJobNameTemplate   = "settings-restore"
	listJobNameTemplate      = "settings-list"
	configMapDefaultFileMode = 0755
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
		Short: "Restore Settings from a backup archive",
		Long: "Restore Settings data from a backup archive stored in S3. Automatically also restores " +
			"Stackpacks backup that was made at the same time, it can be skipped with --skip-stackpacks. " +
			"Can use --latest or --archive to specify which backup to restore.",
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runRestore, cmdutils.StorageIsNotRequired)
		},
	}

	cmd.Flags().StringVar(&archiveName, "archive", "", "Specific archive name to restore (e.g., sts-backup-20251117-1404.sty)")
	cmd.Flags().BoolVar(&useLatest, "latest", false, "Restore from the most recent backup")
	cmd.Flags().BoolVar(&background, "background", false, "Run restore job in background without waiting for completion")
	cmd.Flags().BoolVarP(&skipConfirmation, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&fromPVC, "from-old-pvc", false, "Restore backup from legacy PVC instead of S3")
	cmd.Flags().BoolVar(&skipStackpacks, "skip-stackpacks", false, "Skip restoring stackpacks backup")
	cmd.MarkFlagsMutuallyExclusive("archive", "latest")
	cmd.MarkFlagsOneRequired("archive", "latest")

	return cmd
}

func runRestore(appCtx *app.Context) error {
	// Validate --from-old-pvc: PVC must be configured
	if fromPVC && appCtx.Config.Settings.Restore.PVC == "" {
		return fmt.Errorf("--from-old-pvc requires settings.restore.pvc to be configured")
	}

	// Determine which archive to restore
	backupFile := archiveName
	if useLatest {
		appCtx.Logger.Infof("Finding latest backup...")
		latest, err := getLatestBackup(appCtx)
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

	// Scale down deployments before restore (with lock protection)
	appCtx.Logger.Println()
	scaleDownLabelSelector := appCtx.Config.Settings.Restore.ScaleDownLabelSelector
	scaledDeployments, err := scale.ScaleDownWithLock(scale.ScaleDownWithLockParams{
		K8sClient:     appCtx.K8sClient,
		Namespace:     appCtx.Namespace,
		LabelSelector: scaleDownLabelSelector,
		Datastore:     config.DatastoreSettings,
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

	jobName := fmt.Sprintf("%s-%s", restoreJobNameTemplate, time.Now().Format("20060102t150405"))

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
	return restore.WaitAndCleanup(k8sClient, namespace, jobName, log, false)
}

// getLatestBackup retrieves the most recent backup from all sources (S3 and PVC)
func getLatestBackup(appCtx *app.Context) (string, error) {
	backups, err := getAllBackups(appCtx)
	if err != nil {
		return "", err
	}

	if len(backups) == 0 {
		return "", fmt.Errorf("no backups found")
	}

	// getAllBackups returns backups sorted by LastModified (most recent first)
	return backups[0].Filename, nil
}

// createRestoreJob creates a Kubernetes Job and PVC for restoring from backup
func createRestoreJob(k8sClient *k8s.Client, namespace, jobName, backupFile string, config *config.Config) error {
	defaultMode := int32(configMapDefaultFileMode)

	// Merge common labels with resource-specific labels
	jobLabels := k8s.MergeLabels(config.Kubernetes.CommonLabels, config.Settings.Restore.Job.Labels)

	restoreEnvVar := buildEnvVar([]corev1.EnvVar{{Name: "BACKUP_FILE", Value: backupFile}}, config)

	// Build job spec using configuration
	spec := k8s.JobSpec{
		Name:             jobName,
		Labels:           jobLabels,
		ImagePullSecrets: k8s.ConvertImagePullSecrets(config.Settings.Restore.Job.ImagePullSecrets),
		SecurityContext:  k8s.ConvertPodSecurityContext(&config.Settings.Restore.Job.SecurityContext),
		NodeSelector:     config.Settings.Restore.Job.NodeSelector,
		Tolerations:      k8s.ConvertTolerations(config.Settings.Restore.Job.Tolerations),
		Affinity:         k8s.ConvertAffinity(config.Settings.Restore.Job.Affinity),
		Containers:       []corev1.Container{buildContainer(restoreEnvVar, []string{"/backup-restore-scripts/restore-settings-backup.sh"}, config)},
		Volumes:          buildVolumes(config, defaultMode),
	}

	// Create job
	if _, err := k8sClient.CreateJob(namespace, spec); err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	return nil
}

// buildEnvVar constructs environment variables for the container spec
func buildEnvVar(extraEnvVar []corev1.EnvVar, config *config.Config) []corev1.EnvVar {
	storageService := config.GetStorageService()
	commonVar := []corev1.EnvVar{
		{Name: "BACKUP_CONFIGURATION_BUCKET_NAME", Value: config.Settings.Bucket},
		{Name: "BACKUP_CONFIGURATION_S3_PREFIX", Value: config.Settings.S3Prefix},
		{Name: "BACKUP_CONFIGURATION_STACKPACKS_S3_PREFIX", Value: config.Settings.StackpacksS3Prefix},
		{Name: "MINIO_ENDPOINT", Value: fmt.Sprintf("%s:%d", storageService.Name, storageService.Port)},
		{Name: "STACKSTATE_BASE_URL", Value: config.GetBaseURL()},
		{Name: "RECEIVER_BASE_URL", Value: config.GetReceiverBaseURL()},
		{Name: "PLATFORM_VERSION", Value: config.GetPlatformVersion()},
		{Name: "ZOOKEEPER_QUORUM", Value: config.Settings.Restore.ZookeeperQuorum},
		{Name: "BACKUP_CONFIGURATION_UPLOAD_REMOTE", Value: strconv.FormatBool(config.GlobalBackupEnabled())},
		{Name: "SKIP_STACKPACKS", Value: strconv.FormatBool(skipStackpacks)},
	}
	if fromPVC {
		// Force PVC mode in the shell script, suppress local bucket
		commonVar = append(commonVar, corev1.EnvVar{Name: "BACKUP_RESTORE_FROM_PVC", Value: "true"})
	} else if config.Settings.LocalBucket != "" {
		commonVar = append(commonVar, corev1.EnvVar{Name: "BACKUP_CONFIGURATION_LOCAL_BUCKET", Value: config.Settings.LocalBucket})
	}
	if config.Stackpacks != nil {
		commonVar = append(commonVar, corev1.EnvVar{Name: "CONFIG_FORCE_stackstate_stackPacks_localStackPacksUri", Value: config.Stackpacks.LocalStackPacksURI})
	}
	commonVar = append(commonVar, extraEnvVar...)
	return commonVar
}

// buildVolumeMounts constructs volume mounts for the restore job container
func buildVolumeMounts(config *config.Config) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{Name: "backup-log", MountPath: "/opt/docker/etc_log"},
		{Name: "backup-restore-scripts", MountPath: "/backup-restore-scripts"},
		{Name: "minio-keys", MountPath: "/aws-keys"},
		{Name: "tmp-data", MountPath: "/tmp-data"},
	}
	// Mount PVC in legacy mode or when --from-old-pvc is set
	if config.IsLegacyMode() || fromPVC {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{Name: "settings-backup-data", MountPath: "/settings-backup-data"})
	}

	if config.Stackpacks != nil && config.Stackpacks.PVC != "" && strings.HasPrefix(config.Stackpacks.LocalStackPacksURI, "file://") {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "stackpacks-local",
			MountPath: strings.TrimPrefix(config.Stackpacks.LocalStackPacksURI, "file://"),
		})
	}

	return volumeMounts
}

// buildVolumes constructs volumes for the restore job pod
func buildVolumes(config *config.Config, defaultMode int32) []corev1.Volume {
	volumes := []corev1.Volume{
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
					SecretName: restore.StorageKeysSecretName,
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
	// Include PVC volume in legacy mode or when --from-old-pvc is set
	if config.IsLegacyMode() || fromPVC {
		volumes = append(volumes, corev1.Volume{
			Name: "settings-backup-data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: config.Settings.Restore.PVC,
				},
			},
		})
	}
	if config.Stackpacks != nil && config.Stackpacks.PVC != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "stackpacks-local",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: config.Stackpacks.PVC,
				},
			},
		})
	}

	return volumes
}

// buildContainers constructs containers for the restore job
func buildContainer(envVar []corev1.EnvVar, command []string, config *config.Config) corev1.Container {
	return corev1.Container{
		Name:            "settings",
		Image:           config.Settings.Restore.Job.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: k8s.ConvertSecurityContext(config.Settings.Restore.Job.ContainerSecurityContext),
		Command:         command,
		Env:             envVar,
		Resources:       k8s.ConvertResources(config.Settings.Restore.Job.Resources),
		VolumeMounts:    buildVolumeMounts(config),
	}
}
