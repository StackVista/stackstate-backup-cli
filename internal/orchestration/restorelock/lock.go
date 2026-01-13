package restorelock

import (
	"fmt"
	"time"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

// LabelSelectors maps datastore names to their scale-down label selectors.
// This is used to check for conflicts across all related datastores.
type LabelSelectors map[string]string

// formatUnlockHint returns the kubectl command to manually remove a stuck restore lock
func formatUnlockHint(namespace, labelSelector string) string {
	return fmt.Sprintf(
		"To manually remove a stuck restore lock, run:\n"+
			"  kubectl annotate deployment,statefulset -l %s %s- %s- -n %s",
		labelSelector,
		k8s.RestoreInProgressAnnotation,
		k8s.RestoreStartedAtAnnotation,
		namespace,
	)
}

// formatConflictError creates an error message for restore conflicts including the unlock hint
func formatConflictError(
	currentDatastore, conflictingDatastore, resourceKind, resourceName, startedAt string,
	isMutualExclusion bool,
	namespace, labelSelector string,
) error {
	var msg string
	if isMutualExclusion {
		msg = fmt.Sprintf(
			"cannot start %s restore: %s restore is in progress (started at %s on %s/%s). "+
				"Note: %s and %s restores are mutually exclusive",
			currentDatastore, conflictingDatastore, startedAt,
			resourceKind, resourceName,
			currentDatastore, conflictingDatastore,
		)
	} else {
		msg = fmt.Sprintf(
			"cannot start %s restore: another %s restore is already in progress (started at %s on %s/%s)",
			currentDatastore, conflictingDatastore, startedAt,
			resourceKind, resourceName,
		)
	}

	hint := formatUnlockHint(namespace, labelSelector)
	return fmt.Errorf("%s\n\n%s", msg, hint)
}

// CheckForConflicts checks if any restore operation is in progress that would conflict
// with starting a new restore for the given datastore.
//
// It checks for two types of conflicts:
//  1. Same datastore: Another restore for the same datastore is in progress
//  2. Mutual exclusion: A restore for a mutually exclusive datastore is in progress
//     (e.g., stackgraph and settings cannot run concurrently)
func CheckForConflicts(
	k8sClient *k8s.Client,
	namespace string,
	datastore string,
	allSelectors LabelSelectors,
	log *logger.Logger,
) error {
	log.Debugf("Checking for restore conflicts for datastore: %s", datastore)

	// Get current datastore's selector
	currentSelector, ok := allSelectors[datastore]
	if !ok {
		return fmt.Errorf("no label selector configured for datastore: %s", datastore)
	}

	// 1. Check for same-datastore conflict
	locks, err := k8sClient.GetRestoreLocks(namespace, currentSelector)
	if err != nil {
		return fmt.Errorf("failed to check for restore locks: %w", err)
	}

	for _, lock := range locks {
		return formatConflictError(
			datastore, lock.Datastore, lock.ResourceKind, lock.ResourceName, lock.StartedAt,
			lock.Datastore != datastore,
			namespace, currentSelector,
		)
	}

	// 2. Check for mutual exclusion conflicts
	currentGroup := GetMutualExclusionGroup(datastore)
	if currentGroup != "" {
		relatedDatastores := GetDatastoresInGroup(currentGroup)
		for _, relatedDS := range relatedDatastores {
			if relatedDS == datastore {
				continue // Skip self (already checked above)
			}

			relatedSelector, ok := allSelectors[relatedDS]
			if !ok {
				log.Debugf("No selector for related datastore %s, skipping", relatedDS)
				continue
			}

			locks, err := k8sClient.GetRestoreLocks(namespace, relatedSelector)
			if err != nil {
				return fmt.Errorf("failed to check for restore locks on %s: %w", relatedDS, err)
			}

			for _, lock := range locks {
				return formatConflictError(
					datastore, lock.Datastore, lock.ResourceKind, lock.ResourceName, lock.StartedAt,
					true,
					namespace, relatedSelector,
				)
			}
		}
	}

	log.Debugf("No restore conflicts found for datastore: %s", datastore)
	return nil
}

// AcquireLock sets restore lock annotations on all resources matching the label selector.
// This should be called before starting a restore operation.
func AcquireLock(
	k8sClient *k8s.Client,
	namespace string,
	labelSelector string,
	datastore string,
	log *logger.Logger,
) error {
	startedAt := time.Now().UTC().Format(time.RFC3339)
	log.Debugf("Acquiring restore lock for datastore %s (started at %s)", datastore, startedAt)

	if err := k8sClient.SetRestoreLock(namespace, labelSelector, datastore, startedAt); err != nil {
		return fmt.Errorf("failed to acquire restore lock: %w", err)
	}

	log.Debugf("Restore lock acquired for datastore: %s", datastore)
	return nil
}

// ReleaseLock removes restore lock annotations from all resources matching the label selector.
// This should be called after a restore operation completes (success or failure).
func ReleaseLock(
	k8sClient *k8s.Client,
	namespace string,
	labelSelector string,
	log *logger.Logger,
) error {
	log.Debugf("Releasing restore lock (selector: %s)", labelSelector)

	if err := k8sClient.ClearRestoreLock(namespace, labelSelector); err != nil {
		return fmt.Errorf("failed to release restore lock: %w", err)
	}

	log.Debugf("Restore lock released")
	return nil
}
