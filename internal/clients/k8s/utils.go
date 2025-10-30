package k8s

// MergeLabels merges commonLabels with resource-specific labels.
// Resource-specific labels take precedence over commonLabels.
// Returns a new map with the merged labels.
func MergeLabels(commonLabels, resourceLabels map[string]string) map[string]string {
	merged := make(map[string]string)

	// Add common labels first
	for k, v := range commonLabels {
		merged[k] = v
	}

	// Override with resource-specific labels
	for k, v := range resourceLabels {
		merged[k] = v
	}

	return merged
}
