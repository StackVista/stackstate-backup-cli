package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMergeLabels_BothEmpty tests merging with both maps empty
func TestMergeLabels_BothEmpty(t *testing.T) {
	result := MergeLabels(map[string]string{}, map[string]string{})
	assert.Empty(t, result)
}

// TestMergeLabels_BothNil tests merging with both maps nil
func TestMergeLabels_BothNil(t *testing.T) {
	result := MergeLabels(nil, nil)
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

// TestMergeLabels_OnlyCommonLabels tests merging with only common labels
func TestMergeLabels_OnlyCommonLabels(t *testing.T) {
	common := map[string]string{
		"app":     "backup",
		"version": "1.0",
		"env":     "prod",
	}

	result := MergeLabels(common, nil)

	assert.Len(t, result, 3)
	assert.Equal(t, "backup", result["app"])
	assert.Equal(t, "1.0", result["version"])
	assert.Equal(t, "prod", result["env"])
}

// TestMergeLabels_OnlyResourceLabels tests merging with only resource labels
func TestMergeLabels_OnlyResourceLabels(t *testing.T) {
	resource := map[string]string{
		"component": "elasticsearch",
		"tier":      "backend",
	}

	result := MergeLabels(nil, resource)

	assert.Len(t, result, 2)
	assert.Equal(t, "elasticsearch", result["component"])
	assert.Equal(t, "backend", result["tier"])
}

// TestMergeLabels_NoOverlap tests merging with no overlapping keys
func TestMergeLabels_NoOverlap(t *testing.T) {
	common := map[string]string{
		"app":     "backup",
		"version": "1.0",
	}
	resource := map[string]string{
		"component": "elasticsearch",
		"tier":      "backend",
	}

	result := MergeLabels(common, resource)

	assert.Len(t, result, 4)
	assert.Equal(t, "backup", result["app"])
	assert.Equal(t, "1.0", result["version"])
	assert.Equal(t, "elasticsearch", result["component"])
	assert.Equal(t, "backend", result["tier"])
}

// TestMergeLabels_WithOverlap tests that resource labels override common labels
func TestMergeLabels_WithOverlap(t *testing.T) {
	common := map[string]string{
		"app":     "backup",
		"version": "1.0",
		"env":     "prod",
	}
	resource := map[string]string{
		"version": "2.0",     // Override
		"env":     "staging", // Override
		"tier":    "backend", // New label
	}

	result := MergeLabels(common, resource)

	assert.Len(t, result, 4)
	assert.Equal(t, "backup", result["app"])   // From common
	assert.Equal(t, "2.0", result["version"])  // Overridden by resource
	assert.Equal(t, "staging", result["env"])  // Overridden by resource
	assert.Equal(t, "backend", result["tier"]) // From resource
}

// TestMergeLabels_OverrideAllCommon tests resource labels completely override common labels
func TestMergeLabels_OverrideAllCommon(t *testing.T) {
	common := map[string]string{
		"app": "backup",
		"env": "prod",
	}
	resource := map[string]string{
		"app": "restore",
		"env": "dev",
	}

	result := MergeLabels(common, resource)

	assert.Len(t, result, 2)
	assert.Equal(t, "restore", result["app"]) // Overridden
	assert.Equal(t, "dev", result["env"])     // Overridden
}

// TestMergeLabels_EmptyStrings tests handling of empty string values
func TestMergeLabels_EmptyStrings(t *testing.T) {
	common := map[string]string{
		"app":     "backup",
		"version": "",
	}
	resource := map[string]string{
		"env": "",
	}

	result := MergeLabels(common, resource)

	assert.Len(t, result, 3)
	assert.Equal(t, "backup", result["app"])
	assert.Equal(t, "", result["version"])
	assert.Equal(t, "", result["env"])
}

// TestMergeLabels_SpecialCharacters tests handling of special characters in keys and values
func TestMergeLabels_SpecialCharacters(t *testing.T) {
	common := map[string]string{
		"app.kubernetes.io/name":       "backup",
		"app.kubernetes.io/version":    "1.0",
		"app.kubernetes.io/managed-by": "helm",
	}
	resource := map[string]string{
		"app.kubernetes.io/version":   "2.0", // Override
		"app.kubernetes.io/component": "elasticsearch",
	}

	result := MergeLabels(common, resource)

	assert.Len(t, result, 4)
	assert.Equal(t, "backup", result["app.kubernetes.io/name"])
	assert.Equal(t, "2.0", result["app.kubernetes.io/version"]) // Overridden
	assert.Equal(t, "helm", result["app.kubernetes.io/managed-by"])
	assert.Equal(t, "elasticsearch", result["app.kubernetes.io/component"])
}

// TestMergeLabels_DoesNotModifyInput tests that input maps are not modified
func TestMergeLabels_DoesNotModifyInput(t *testing.T) {
	common := map[string]string{
		"app": "backup",
		"env": "prod",
	}
	resource := map[string]string{
		"version": "1.0",
	}

	// Keep copies for comparison
	commonCopy := make(map[string]string)
	for k, v := range common {
		commonCopy[k] = v
	}
	resourceCopy := make(map[string]string)
	for k, v := range resource {
		resourceCopy[k] = v
	}

	result := MergeLabels(common, resource)

	// Verify input maps weren't modified
	assert.Equal(t, commonCopy, common)
	assert.Equal(t, resourceCopy, resource)

	// Verify result is independent
	result["new-key"] = "new-value"
	assert.NotContains(t, common, "new-key")
	assert.NotContains(t, resource, "new-key")
}

// TestMergeLabels_KubernetesLabels tests realistic Kubernetes label scenarios
func TestMergeLabels_KubernetesLabels(t *testing.T) {
	tests := []struct {
		name           string
		commonLabels   map[string]string
		resourceLabels map[string]string
		expected       map[string]string
	}{
		{
			name: "backup job labels",
			commonLabels: map[string]string{
				"app.kubernetes.io/name":       "suse-observability",
				"app.kubernetes.io/managed-by": "helm",
				"helm.sh/chart":                "suse-observability-1.0.0",
			},
			resourceLabels: map[string]string{
				"app.kubernetes.io/component": "backup",
				"backup.stackstate.com/type":  "elasticsearch",
			},
			expected: map[string]string{
				"app.kubernetes.io/name":       "suse-observability",
				"app.kubernetes.io/managed-by": "helm",
				"helm.sh/chart":                "suse-observability-1.0.0",
				"app.kubernetes.io/component":  "backup",
				"backup.stackstate.com/type":   "elasticsearch",
			},
		},
		{
			name: "restore job labels with override",
			commonLabels: map[string]string{
				"app":                          "backup-tool",
				"app.kubernetes.io/version":    "1.0",
				"app.kubernetes.io/managed-by": "operator",
			},
			resourceLabels: map[string]string{
				"app.kubernetes.io/version":   "2.0", // Override version
				"app.kubernetes.io/component": "restore",
			},
			expected: map[string]string{
				"app":                          "backup-tool",
				"app.kubernetes.io/version":    "2.0", // Overridden
				"app.kubernetes.io/managed-by": "operator",
				"app.kubernetes.io/component":  "restore",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeLabels(tt.commonLabels, tt.resourceLabels)
			assert.Equal(t, tt.expected, result)
		})
	}
}
