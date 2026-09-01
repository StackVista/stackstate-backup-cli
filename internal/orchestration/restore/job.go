package restore

import (
	"fmt"
	"time"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

const (
	defaultJobStatusCheckInterval = 10 * time.Second
)

// WaitForJobCompletion waits for a Kubernetes job to complete
func WaitForJobCompletion(k8sClient *k8s.Client, namespace, jobName string, log *logger.Logger) error {
	ticker := time.NewTicker(defaultJobStatusCheckInterval)
	defer ticker.Stop()

	for {
		<-ticker.C
		job, err := k8sClient.GetJob(namespace, jobName)
		if err != nil {
			return fmt.Errorf("failed to get job status: %w", err)
		}

		if job.Status.Succeeded > 0 {
			return nil
		}

		if job.Status.Failed > 0 {
			// Get and print logs from failed job
			log.Println()
			log.Errorf("Job failed. Fetching logs...")
			log.Println()
			if err := PrintJobLogs(k8sClient, namespace, jobName, log); err != nil {
				log.Warningf("Failed to fetch job logs: %v", err)
			}
			return fmt.Errorf("job failed")
		}

		log.Debugf("Job status: Active=%d, Succeeded=%d, Failed=%d",
			job.Status.Active, job.Status.Succeeded, job.Status.Failed)
	}
}

// PrintJobLogs retrieves and prints logs from all containers in a job's pods
func PrintJobLogs(k8sClient *k8s.Client, namespace, jobName string, log *logger.Logger) error {
	// Get logs from all pods in the job
	allPodLogs, err := k8sClient.GetJobLogs(namespace, jobName)
	if err != nil {
		return err
	}

	// Print logs from each pod
	for _, podLogs := range allPodLogs {
		log.Infof("=== Logs from pod: %s ===", podLogs.PodName)
		log.Println()

		// Print logs from each container
		for _, containerLog := range podLogs.ContainerLogs {
			containerType := "container"
			if containerLog.IsInit {
				containerType = "init container"
			}

			log.Infof("--- Logs from %s: %s ---", containerType, containerLog.Name)

			// Print the actual logs
			if containerLog.Logs != "" {
				fmt.Println(containerLog.Logs)
			} else {
				log.Infof("(no logs)")
			}
			log.Println()
		}
	}

	return nil
}

// PrintWaitingMessage prints waiting message with instructions for interruption
func PrintWaitingMessage(log *logger.Logger, serviceName, jobName, namespace string) {
	log.Println()
	log.Infof("Waiting for restore job to complete (this may take significant amount of time depending on the archive size)...")
	log.Println()
	log.Infof("Monitoring commands:")
	log.Infof("  kubectl logs --follow job/%s -n %s", jobName, namespace)
	log.Println()
	log.Infof("You can safely interrupt this command with Ctrl+C.")
	log.Infof("To check status, scale up the required deployments and cleanup later, run:")
	log.Infof("  sts-backup %s check-and-finalize --job %s --wait -n %s", serviceName, jobName, namespace)
}

// PrintRunningJobStatus prints status and instructions for a running job
func PrintRunningJobStatus(log *logger.Logger, serviceName, jobName, namespace string, activePods int32) {
	log.Println()
	log.Infof("Job is running in background: %s", jobName)
	if activePods > 0 {
		log.Infof("  Active pods: %d", activePods)
	}
	log.Println()
	log.Infof("Monitoring commands:")
	log.Infof("  kubectl logs --follow job/%s -n %s", jobName, namespace)
	log.Infof("  kubectl get job %s -n %s", jobName, namespace)
	log.Println()
	log.Infof("To wait for completion, scaling up the necessary deployments and cleanup, run:")
	log.Infof("  sts-backup %s check-and-finalize --job %s --wait -n %s", serviceName, jobName, namespace)
}

// WaitAndCleanup waits for job completion and cleans up resources
func WaitAndCleanup(k8sClient *k8s.Client, namespace, jobName string, log *logger.Logger, cleanupPVC bool) error {
	if err := WaitForJobCompletion(k8sClient, namespace, jobName, log); err != nil {
		log.Errorf("Job failed: %v", err)
		log.Println()
		log.Infof("Cleanup commands:")
		if cleanupPVC {
			log.Infof("  kubectl delete job,pvc %s -n %s", jobName, namespace)
		} else {
			log.Infof("  kubectl delete job %s -n %s", jobName, namespace)
		}
		return err
	}

	log.Println()
	log.Successf("Restore completed successfully")

	// Cleanup resources
	log.Println()
	return CleanupResources(k8sClient, namespace, jobName, "", log, cleanupPVC)
}
