package restore

import (
	"fmt"
	"time"

	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

const (
	defaultAPIRestoreTimeout      = 30 * time.Minute
	defaultAPIStatusCheckInterval = 10 * time.Second
)

// WaitForAPIRestore waits for an API-based restore operation to complete by polling status
// checkStatusFn should return (statusMessage, isComplete, error)
// where statusMessage describes current state, isComplete indicates if operation finished
func WaitForAPIRestore(
	checkStatusFn func() (string, bool, error),
	interval time.Duration,
	timeout time.Duration,
	log *logger.Logger,
) error {
	if interval == 0 {
		interval = defaultAPIStatusCheckInterval
	}
	if timeout == 0 {
		timeout = defaultAPIRestoreTimeout
	}

	timeoutChan := time.After(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("timeout waiting for restore to complete")
		case <-ticker.C:
			statusMsg, isComplete, err := checkStatusFn()
			if err != nil {
				return fmt.Errorf("failed to check restore status: %w", err)
			}

			log.Debugf("Restore status: %s (complete: %v)", statusMsg, isComplete)

			if isComplete {
				if statusMsg == "SUCCESS" || statusMsg == "PARTIAL" {
					log.Debugf("Restore completed successfully")
					return nil
				}
				return fmt.Errorf("restore failed with status: %s", statusMsg)
			}
		}
	}
}

// PrintAPIWaitingMessage prints waiting message with instructions for interruption
// Adapted from job.go:PrintWaitingMessage for API-based restores
func PrintAPIWaitingMessage(serviceName, identifier, namespace string, log *logger.Logger) {
	log.Println()
	log.Infof("Waiting for restore to complete (this may take significant amount of time depending on the data size)...")
	log.Println()
	log.Infof("You can safely interrupt this command with Ctrl+C.")
	log.Infof("To check status and finalize later, run:")
	log.Infof("  sts-backup %s check-and-finalize --operation-id %s -n %s", serviceName, identifier, namespace)
}

// PrintAPIRunningRestoreStatus prints status and instructions for a running restore
// Adapted from job.go:PrintRunningJobStatus for API-based restores
func PrintAPIRunningRestoreStatus(serviceName, identifier, namespace string, log *logger.Logger) {
	log.Println()
	log.Infof("Restore is running for %s: %s", serviceName, identifier)
	log.Println()
	log.Infof("To check status and finalize, run:")
	log.Infof("  sts-backup %s check-and-finalize --operation-id %s --wait -n %s", serviceName, identifier, namespace)
}

// FinalizeRestore executes post-restore finalization steps
func FinalizeRestore(scaleUpFn func() error, log *logger.Logger) error {
	log.Infof("Finalizing restore...")
	if err := scaleUpFn(); err != nil {
		return fmt.Errorf("failed to scale up deployments: %w", err)
	}
	log.Successf("Finalization completed successfully")
	return nil
}
