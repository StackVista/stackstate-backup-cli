package restore

import (
	"fmt"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/scripts"
)

const (
	// MinioKeysSecretName is the name of the secret containing Minio access/secret keys
	MinioKeysSecretName = "suse-observability-backup-cli-minio-keys" //nolint:gosec // This is a Kubernetes secret name, not a credential
	// RestoreScriptsConfigMap is the name of the ConfigMap containing restore scripts
	RestoreScriptsConfigMap = "suse-observability-backup-cli-restore-scripts"
)

// EnsureRestoreResources ensures that required Kubernetes resources exist for the restore job
func EnsureRestoreResources(k8sClient *k8s.Client, namespace string, config *config.Config, log *logger.Logger) error {
	// Ensure backup scripts ConfigMap exists
	log.Infof("Ensuring backup scripts ConfigMap exists...")

	scriptNames, err := scripts.ListScripts()
	if err != nil {
		return fmt.Errorf("failed to list embedded scripts: %w", err)
	}

	scriptsData := make(map[string]string)
	for _, scriptName := range scriptNames {
		scriptContent, err := scripts.GetScript(scriptName)
		if err != nil {
			return fmt.Errorf("failed to get script %s: %w", scriptName, err)
		}
		scriptsData[scriptName] = string(scriptContent)
	}

	configMapLabels := k8s.MergeLabels(config.Kubernetes.CommonLabels, map[string]string{})
	if _, err := k8sClient.EnsureConfigMap(namespace, RestoreScriptsConfigMap, scriptsData, configMapLabels); err != nil {
		return fmt.Errorf("failed to ensure backup scripts ConfigMap: %w", err)
	}
	log.Successf("Backup scripts ConfigMap ready")

	// Ensure Minio keys secret exists
	log.Infof("Ensuring Minio keys secret exists...")

	secretData := map[string][]byte{
		"accesskey": []byte(config.Minio.AccessKey),
		"secretkey": []byte(config.Minio.SecretKey),
	}

	secretLabels := k8s.MergeLabels(config.Kubernetes.CommonLabels, map[string]string{})
	if _, err := k8sClient.EnsureSecret(namespace, MinioKeysSecretName, secretData, secretLabels); err != nil {
		return fmt.Errorf("failed to ensure Minio keys secret: %w", err)
	}
	log.Successf("Minio keys secret ready")

	return nil
}

// CleanupResources cleans up job and optionally PVC resources
// If pvcName is empty, no PVC cleanup is attempted
func CleanupResources(k8sClient *k8s.Client, namespace, jobName, pvcName string, log *logger.Logger, cleanupPVC bool) error {
	log.Infof("Cleaning up resources...")

	// Delete job
	if err := k8sClient.DeleteJob(namespace, jobName); err != nil {
		log.Warningf("Failed to delete job: %v", err)
	} else {
		log.Successf("Job deleted: %s", jobName)
	}

	// Delete PVC if requested
	if cleanupPVC {
		// Use jobName as PVC name if pvcName not specified
		if pvcName == "" {
			pvcName = jobName
		}
		if err := k8sClient.DeletePVC(namespace, pvcName); err != nil {
			log.Warningf("Failed to delete PVC: %v", err)
		} else {
			log.Successf("PVC deleted: %s", pvcName)
		}
	}

	return nil
}
