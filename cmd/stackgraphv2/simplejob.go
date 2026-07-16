package stackgraphv2

import (
	"fmt"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/restore"
	corev1 "k8s.io/api/core/v1"
)

// createSimpleJob creates a Kubernetes Job for a "simple" stackgraph restore step
// (abort or backfill). These jobs share the same spec and only differ in their
// container name and command.
func createSimpleJob(k8sClient *k8s.Client, namespace, jobName, containerName, command string, config *config.Config) error {
	defaultMode := int32(configMapDefaultFileMode)

	// Merge common labels with resource-specific labels
	jobLabels := k8s.MergeLabels(config.Kubernetes.CommonLabels, config.Stackgraph.Restore.Job.Labels)

	// Build job spec using configuration
	spec := k8s.JobSpec{
		Name:             jobName,
		Labels:           jobLabels,
		ImagePullSecrets: k8s.ConvertImagePullSecrets(config.Stackgraph.Restore.Job.ImagePullSecrets),
		SecurityContext:  k8s.ConvertPodSecurityContext(&config.Stackgraph.Restore.Job.SecurityContext),
		NodeSelector:     config.Stackgraph.Restore.Job.NodeSelector,
		Tolerations:      k8s.ConvertTolerations(config.Stackgraph.Restore.Job.Tolerations),
		Affinity:         k8s.ConvertAffinity(config.Stackgraph.Restore.Job.Affinity),
		Containers:       buildSimpleJobContainers(config, containerName, command),
		InitContainers:   buildSimpleJobInitContainers(config),
		Volumes:          buildSimpleJobVolumes(config, defaultMode),
	}

	// Create job
	_, err := k8sClient.CreateJob(namespace, spec)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	return nil
}

// buildSimpleJobEnvVars constructs environment variables shared by the abort and backfill jobs
func buildSimpleJobEnvVars(config *config.Config) []corev1.EnvVar {
	storageService := config.GetStorageService()
	return []corev1.EnvVar{
		{Name: "BACKUP_STACKGRAPH_BUCKET_NAME", Value: config.Stackgraph.Bucket},
		{Name: "BACKUP_STACKGRAPH_S3_PREFIX", Value: config.Stackgraph.S3Prefix},
		{Name: "S3_ENDPOINT", Value: fmt.Sprintf("%s:%d", storageService.Name, storageService.Port)},
		{Name: "STACKSTATE_BASE_URL", Value: config.GetBaseURL()},
		{Name: "RECEIVER_BASE_URL", Value: config.GetReceiverBaseURL()},
		{Name: "PLATFORM_VERSION", Value: config.GetPlatformVersion()},
		{Name: "ZOOKEEPER_QUORUM", Value: config.Stackgraph.Restore.ZookeeperQuorum},
	}
}

// buildSimpleJobVolumeMounts constructs volume mounts shared by the abort and backfill job containers
func buildSimpleJobVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "backup-log", MountPath: "/opt/docker/etc_log"},
		{Name: "backup-restore-scripts", MountPath: "/backup-restore-scripts"},
		{Name: "minio-keys", MountPath: "/aws-keys"},
	}
}

// buildSimpleJobInitContainers constructs init containers shared by the abort and backfill jobs
func buildSimpleJobInitContainers(config *config.Config) []corev1.Container {
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

// buildSimpleJobVolumes constructs volumes shared by the abort and backfill job pods
func buildSimpleJobVolumes(config *config.Config, defaultMode int32) []corev1.Volume {
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
	}
}

// buildSimpleJobContainers constructs the main container for a simple (abort/backfill) job
func buildSimpleJobContainers(config *config.Config, name, command string) []corev1.Container {
	return []corev1.Container{
		{
			Name:            name,
			Image:           config.Stackgraph.Restore.Job.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: k8s.ConvertSecurityContext(config.Stackgraph.Restore.Job.ContainerSecurityContext),
			Command:         []string{command},
			Env:             buildSimpleJobEnvVars(config),
			Resources:       k8s.ConvertResources(config.Stackgraph.Restore.Job.Resources),
			VolumeMounts:    buildSimpleJobVolumeMounts(),
		},
	}
}
