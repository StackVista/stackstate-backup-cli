package k8s

import (
	"testing"

	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConvertResources tests conversion of resource requirements
func TestConvertResources(t *testing.T) {
	tests := []struct {
		name     string
		input    config.ResourceRequirements
		validate func(*testing.T, corev1.ResourceRequirements)
	}{
		{
			name: "empty resources",
			input: config.ResourceRequirements{
				Limits:   config.ResourceList{},
				Requests: config.ResourceList{},
			},
			validate: func(t *testing.T, result corev1.ResourceRequirements) {
				assert.Empty(t, result.Limits)
				assert.Empty(t, result.Requests)
			},
		},
		{
			name: "only CPU and memory limits",
			input: config.ResourceRequirements{
				Limits: config.ResourceList{
					CPU:    "1",
					Memory: "2Gi",
				},
			},
			validate: func(t *testing.T, result corev1.ResourceRequirements) {
				assert.Len(t, result.Limits, 2)
				assert.Contains(t, result.Limits, corev1.ResourceCPU)
				assert.Contains(t, result.Limits, corev1.ResourceMemory)
				assert.Empty(t, result.Requests)
			},
		},
		{
			name: "only requests",
			input: config.ResourceRequirements{
				Requests: config.ResourceList{
					CPU:    "500m",
					Memory: "1Gi",
				},
			},
			validate: func(t *testing.T, result corev1.ResourceRequirements) {
				assert.Empty(t, result.Limits)
				assert.Len(t, result.Requests, 2)
				assert.Contains(t, result.Requests, corev1.ResourceCPU)
				assert.Contains(t, result.Requests, corev1.ResourceMemory)
			},
		},
		{
			name: "full resources with ephemeral storage",
			input: config.ResourceRequirements{
				Limits: config.ResourceList{
					CPU:              "2",
					Memory:           "4Gi",
					EphemeralStorage: "10Gi",
				},
				Requests: config.ResourceList{
					CPU:              "1",
					Memory:           "2Gi",
					EphemeralStorage: "5Gi",
				},
			},
			validate: func(t *testing.T, result corev1.ResourceRequirements) {
				assert.Len(t, result.Limits, 3)
				assert.Len(t, result.Requests, 3)
				assert.Contains(t, result.Limits, corev1.ResourceCPU)
				assert.Contains(t, result.Limits, corev1.ResourceMemory)
				assert.Contains(t, result.Limits, corev1.ResourceEphemeralStorage)
				assert.Contains(t, result.Requests, corev1.ResourceCPU)
				assert.Contains(t, result.Requests, corev1.ResourceMemory)
				assert.Contains(t, result.Requests, corev1.ResourceEphemeralStorage)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertResources(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestConvertImagePullSecrets tests conversion of image pull secrets
func TestConvertImagePullSecrets(t *testing.T) {
	tests := []struct {
		name     string
		input    []config.LocalObjectRef
		expected []corev1.LocalObjectReference
	}{
		{
			name:     "empty slice",
			input:    []config.LocalObjectRef{},
			expected: nil,
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: nil,
		},
		{
			name: "single secret",
			input: []config.LocalObjectRef{
				{Name: "registry-secret"},
			},
			expected: []corev1.LocalObjectReference{
				{Name: "registry-secret"},
			},
		},
		{
			name: "multiple secrets",
			input: []config.LocalObjectRef{
				{Name: "docker-secret"},
				{Name: "gcr-secret"},
				{Name: "ecr-secret"},
			},
			expected: []corev1.LocalObjectReference{
				{Name: "docker-secret"},
				{Name: "gcr-secret"},
				{Name: "ecr-secret"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertImagePullSecrets(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertPodSecurityContext tests conversion of pod security context
func TestConvertPodSecurityContext(t *testing.T) {
	tests := []struct {
		name     string
		input    *config.PodSecurityContext
		expected *corev1.PodSecurityContext
	}{
		{
			name:     "nil context",
			input:    nil,
			expected: nil,
		},
		{
			name:  "empty context",
			input: &config.PodSecurityContext{},
			expected: &corev1.PodSecurityContext{
				FSGroup:      nil,
				RunAsGroup:   nil,
				RunAsNonRoot: nil,
				RunAsUser:    nil,
			},
		},
		{
			name: "full context",
			input: &config.PodSecurityContext{
				FSGroup:      int64Ptr(3000),
				RunAsGroup:   int64Ptr(2000),
				RunAsNonRoot: boolPtr(true),
				RunAsUser:    int64Ptr(1000),
			},
			expected: &corev1.PodSecurityContext{
				FSGroup:      int64Ptr(3000),
				RunAsGroup:   int64Ptr(2000),
				RunAsNonRoot: boolPtr(true),
				RunAsUser:    int64Ptr(1000),
			},
		},
		{
			name: "partial context",
			input: &config.PodSecurityContext{
				RunAsUser:    int64Ptr(1000),
				RunAsNonRoot: boolPtr(true),
			},
			expected: &corev1.PodSecurityContext{
				RunAsUser:    int64Ptr(1000),
				RunAsNonRoot: boolPtr(true),
				FSGroup:      nil,
				RunAsGroup:   nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertPodSecurityContext(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertSecurityContext tests conversion of container security context
func TestConvertSecurityContext(t *testing.T) {
	tests := []struct {
		name     string
		input    *config.SecurityContext
		expected *corev1.SecurityContext
	}{
		{
			name:     "nil context",
			input:    nil,
			expected: nil,
		},
		{
			name:  "empty context",
			input: &config.SecurityContext{},
			expected: &corev1.SecurityContext{
				AllowPrivilegeEscalation: nil,
				RunAsNonRoot:             nil,
				RunAsUser:                nil,
			},
		},
		{
			name: "full context",
			input: &config.SecurityContext{
				AllowPrivilegeEscalation: boolPtr(false),
				RunAsNonRoot:             boolPtr(true),
				RunAsUser:                int64Ptr(1000),
			},
			expected: &corev1.SecurityContext{
				AllowPrivilegeEscalation: boolPtr(false),
				RunAsNonRoot:             boolPtr(true),
				RunAsUser:                int64Ptr(1000),
			},
		},
		{
			name: "only privilege escalation",
			input: &config.SecurityContext{
				AllowPrivilegeEscalation: boolPtr(false),
			},
			expected: &corev1.SecurityContext{
				AllowPrivilegeEscalation: boolPtr(false),
				RunAsNonRoot:             nil,
				RunAsUser:                nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertSecurityContext(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertTolerations tests conversion of tolerations
func TestConvertTolerations(t *testing.T) {
	tests := []struct {
		name     string
		input    []config.Toleration
		expected []corev1.Toleration
	}{
		{
			name:     "empty slice",
			input:    []config.Toleration{},
			expected: []corev1.Toleration{},
		},
		{
			name: "single toleration",
			input: []config.Toleration{
				{
					Key:      "key1",
					Operator: "Equal",
					Value:    "value1",
					Effect:   "NoSchedule",
				},
			},
			expected: []corev1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},
		{
			name: "multiple tolerations",
			input: []config.Toleration{
				{
					Key:      "node.kubernetes.io/not-ready",
					Operator: "Exists",
					Effect:   "NoExecute",
				},
				{
					Key:      "node.kubernetes.io/unreachable",
					Operator: "Exists",
					Effect:   "NoExecute",
				},
				{
					Key:      "disktype",
					Operator: "Equal",
					Value:    "ssd",
					Effect:   "PreferNoSchedule",
				},
			},
			expected: []corev1.Toleration{
				{
					Key:      "node.kubernetes.io/not-ready",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoExecute,
				},
				{
					Key:      "node.kubernetes.io/unreachable",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoExecute,
				},
				{
					Key:      "disktype",
					Operator: corev1.TolerationOpEqual,
					Value:    "ssd",
					Effect:   corev1.TaintEffectPreferNoSchedule,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertTolerations(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertAffinity tests conversion of affinity
func TestConvertAffinity(t *testing.T) {
	tests := []struct {
		name     string
		input    *config.Affinity
		validate func(*testing.T, *corev1.Affinity)
	}{
		{
			name:  "nil affinity",
			input: nil,
			validate: func(t *testing.T, result *corev1.Affinity) {
				assert.Nil(t, result)
			},
		},
		{
			name:  "empty affinity",
			input: &config.Affinity{},
			validate: func(t *testing.T, result *corev1.Affinity) {
				assert.NotNil(t, result)
				assert.Nil(t, result.NodeAffinity)
				assert.Nil(t, result.PodAffinity)
				assert.Nil(t, result.PodAntiAffinity)
			},
		},
		{
			name: "only node affinity",
			input: &config.Affinity{
				NodeAffinity: &config.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &config.NodeSelector{
						NodeSelectorTerms: []config.NodeSelectorTerm{
							{
								MatchExpressions: []config.NodeSelectorRequirement{
									{
										Key:      "disktype",
										Operator: "In",
										Values:   []string{"ssd"},
									},
								},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, result *corev1.Affinity) {
				assert.NotNil(t, result)
				assert.NotNil(t, result.NodeAffinity)
				assert.Nil(t, result.PodAffinity)
				assert.Nil(t, result.PodAntiAffinity)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertAffinity(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestConvertNodeAffinity tests conversion of node affinity
func TestConvertNodeAffinity(t *testing.T) {
	tests := []struct {
		name     string
		input    *config.NodeAffinity
		validate func(*testing.T, *corev1.NodeAffinity)
	}{
		{
			name:  "nil node affinity",
			input: nil,
			validate: func(t *testing.T, result *corev1.NodeAffinity) {
				assert.Nil(t, result)
			},
		},
		{
			name:  "empty node affinity",
			input: &config.NodeAffinity{},
			validate: func(t *testing.T, result *corev1.NodeAffinity) {
				assert.NotNil(t, result)
				assert.Nil(t, result.RequiredDuringSchedulingIgnoredDuringExecution)
				assert.Nil(t, result.PreferredDuringSchedulingIgnoredDuringExecution)
			},
		},
		{
			name: "required node selector",
			input: &config.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &config.NodeSelector{
					NodeSelectorTerms: []config.NodeSelectorTerm{
						{
							MatchExpressions: []config.NodeSelectorRequirement{
								{
									Key:      "kubernetes.io/os",
									Operator: "In",
									Values:   []string{"linux"},
								},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, result *corev1.NodeAffinity) {
				assert.NotNil(t, result)
				assert.NotNil(t, result.RequiredDuringSchedulingIgnoredDuringExecution)
				assert.Len(t, result.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, 1)
				assert.Len(t, result.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions, 1)
				assert.Equal(t, "kubernetes.io/os", result.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key)
			},
		},
		{
			name: "preferred node selector",
			input: &config.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []config.PreferredSchedulingTerm{
					{
						Weight: 1,
						Preference: config.NodeSelectorTerm{
							MatchExpressions: []config.NodeSelectorRequirement{
								{
									Key:      "zone",
									Operator: "In",
									Values:   []string{"us-west-1a"},
								},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, result *corev1.NodeAffinity) {
				assert.NotNil(t, result)
				assert.Len(t, result.PreferredDuringSchedulingIgnoredDuringExecution, 1)
				assert.Equal(t, int32(1), result.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertNodeAffinity(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestConvertNodeSelectorTerm tests conversion of node selector term
func TestConvertNodeSelectorTerm(t *testing.T) {
	tests := []struct {
		name     string
		input    config.NodeSelectorTerm
		validate func(*testing.T, corev1.NodeSelectorTerm)
	}{
		{
			name:  "empty term",
			input: config.NodeSelectorTerm{},
			validate: func(t *testing.T, result corev1.NodeSelectorTerm) {
				assert.Empty(t, result.MatchExpressions)
				assert.Empty(t, result.MatchFields)
			},
		},
		{
			name: "match expressions",
			input: config.NodeSelectorTerm{
				MatchExpressions: []config.NodeSelectorRequirement{
					{
						Key:      "disktype",
						Operator: "In",
						Values:   []string{"ssd", "nvme"},
					},
					{
						Key:      "kubernetes.io/arch",
						Operator: "NotIn",
						Values:   []string{"arm"},
					},
				},
			},
			validate: func(t *testing.T, result corev1.NodeSelectorTerm) {
				assert.Len(t, result.MatchExpressions, 2)
				assert.Equal(t, "disktype", result.MatchExpressions[0].Key)
				assert.Equal(t, corev1.NodeSelectorOpIn, result.MatchExpressions[0].Operator)
				assert.Equal(t, []string{"ssd", "nvme"}, result.MatchExpressions[0].Values)
			},
		},
		{
			name: "match fields",
			input: config.NodeSelectorTerm{
				MatchFields: []config.NodeSelectorRequirement{
					{
						Key:      "metadata.name",
						Operator: "In",
						Values:   []string{"node-1", "node-2"},
					},
				},
			},
			validate: func(t *testing.T, result corev1.NodeSelectorTerm) {
				assert.Len(t, result.MatchFields, 1)
				assert.Equal(t, "metadata.name", result.MatchFields[0].Key)
				assert.Equal(t, corev1.NodeSelectorOpIn, result.MatchFields[0].Operator)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertNodeSelectorTerm(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestConvertPodAffinity tests conversion of pod affinity
func TestConvertPodAffinity(t *testing.T) {
	tests := []struct {
		name     string
		input    *config.PodAffinity
		validate func(*testing.T, *corev1.PodAffinity)
	}{
		{
			name:  "nil pod affinity",
			input: nil,
			validate: func(t *testing.T, result *corev1.PodAffinity) {
				assert.Nil(t, result)
			},
		},
		{
			name:  "empty pod affinity",
			input: &config.PodAffinity{},
			validate: func(t *testing.T, result *corev1.PodAffinity) {
				assert.NotNil(t, result)
				assert.Empty(t, result.RequiredDuringSchedulingIgnoredDuringExecution)
				assert.Empty(t, result.PreferredDuringSchedulingIgnoredDuringExecution)
			},
		},
		{
			name: "required pod affinity",
			input: &config.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []config.PodAffinityTerm{
					{
						TopologyKey: "kubernetes.io/hostname",
						LabelSelector: &config.LabelSelector{
							MatchLabels: map[string]string{"app": "cache"},
						},
					},
				},
			},
			validate: func(t *testing.T, result *corev1.PodAffinity) {
				assert.NotNil(t, result)
				assert.Len(t, result.RequiredDuringSchedulingIgnoredDuringExecution, 1)
				assert.Equal(t, "kubernetes.io/hostname", result.RequiredDuringSchedulingIgnoredDuringExecution[0].TopologyKey)
			},
		},
		{
			name: "preferred pod affinity",
			input: &config.PodAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []config.WeightedPodAffinityTerm{
					{
						Weight: 100,
						PodAffinityTerm: config.PodAffinityTerm{
							TopologyKey: "zone",
							LabelSelector: &config.LabelSelector{
								MatchLabels: map[string]string{"app": "web"},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, result *corev1.PodAffinity) {
				assert.NotNil(t, result)
				assert.Len(t, result.PreferredDuringSchedulingIgnoredDuringExecution, 1)
				assert.Equal(t, int32(100), result.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertPodAffinity(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestConvertPodAntiAffinity tests conversion of pod anti-affinity
func TestConvertPodAntiAffinity(t *testing.T) {
	tests := []struct {
		name     string
		input    *config.PodAntiAffinity
		validate func(*testing.T, *corev1.PodAntiAffinity)
	}{
		{
			name:  "nil pod anti-affinity",
			input: nil,
			validate: func(t *testing.T, result *corev1.PodAntiAffinity) {
				assert.Nil(t, result)
			},
		},
		{
			name:  "empty pod anti-affinity",
			input: &config.PodAntiAffinity{},
			validate: func(t *testing.T, result *corev1.PodAntiAffinity) {
				assert.NotNil(t, result)
				assert.Empty(t, result.RequiredDuringSchedulingIgnoredDuringExecution)
				assert.Empty(t, result.PreferredDuringSchedulingIgnoredDuringExecution)
			},
		},
		{
			name: "required pod anti-affinity",
			input: &config.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []config.PodAffinityTerm{
					{
						TopologyKey: "kubernetes.io/hostname",
						LabelSelector: &config.LabelSelector{
							MatchLabels: map[string]string{"app": "backup"},
						},
					},
				},
			},
			validate: func(t *testing.T, result *corev1.PodAntiAffinity) {
				assert.NotNil(t, result)
				assert.Len(t, result.RequiredDuringSchedulingIgnoredDuringExecution, 1)
				assert.Equal(t, "kubernetes.io/hostname", result.RequiredDuringSchedulingIgnoredDuringExecution[0].TopologyKey)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertPodAntiAffinity(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestConvertPodAffinityTerm tests conversion of pod affinity term
func TestConvertPodAffinityTerm(t *testing.T) {
	tests := []struct {
		name     string
		input    config.PodAffinityTerm
		validate func(*testing.T, corev1.PodAffinityTerm)
	}{
		{
			name: "minimal term",
			input: config.PodAffinityTerm{
				TopologyKey: "kubernetes.io/hostname",
			},
			validate: func(t *testing.T, result corev1.PodAffinityTerm) {
				assert.Equal(t, "kubernetes.io/hostname", result.TopologyKey)
				assert.Nil(t, result.LabelSelector)
				assert.Empty(t, result.Namespaces)
			},
		},
		{
			name: "with label selector",
			input: config.PodAffinityTerm{
				TopologyKey: "zone",
				LabelSelector: &config.LabelSelector{
					MatchLabels: map[string]string{
						"app": "database",
						"env": "prod",
					},
				},
			},
			validate: func(t *testing.T, result corev1.PodAffinityTerm) {
				assert.Equal(t, "zone", result.TopologyKey)
				assert.NotNil(t, result.LabelSelector)
				assert.Equal(t, map[string]string{"app": "database", "env": "prod"}, result.LabelSelector.MatchLabels)
			},
		},
		{
			name: "with label selector and match expressions",
			input: config.PodAffinityTerm{
				TopologyKey: "region",
				LabelSelector: &config.LabelSelector{
					MatchLabels: map[string]string{"app": "web"},
					MatchExpressions: []config.LabelSelectorRequirement{
						{
							Key:      "tier",
							Operator: "In",
							Values:   []string{"frontend", "backend"},
						},
					},
				},
			},
			validate: func(t *testing.T, result corev1.PodAffinityTerm) {
				assert.Equal(t, "region", result.TopologyKey)
				assert.NotNil(t, result.LabelSelector)
				assert.Len(t, result.LabelSelector.MatchExpressions, 1)
				assert.Equal(t, "tier", result.LabelSelector.MatchExpressions[0].Key)
				assert.Equal(t, metav1.LabelSelectorOpIn, result.LabelSelector.MatchExpressions[0].Operator)
				assert.Equal(t, []string{"frontend", "backend"}, result.LabelSelector.MatchExpressions[0].Values)
			},
		},
		{
			name: "with namespaces",
			input: config.PodAffinityTerm{
				TopologyKey: "kubernetes.io/hostname",
				Namespaces:  []string{"default", "kube-system"},
				LabelSelector: &config.LabelSelector{
					MatchLabels: map[string]string{"app": "monitoring"},
				},
			},
			validate: func(t *testing.T, result corev1.PodAffinityTerm) {
				assert.Equal(t, "kubernetes.io/hostname", result.TopologyKey)
				assert.Equal(t, []string{"default", "kube-system"}, result.Namespaces)
				assert.NotNil(t, result.LabelSelector)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertPodAffinityTerm(tt.input)
			tt.validate(t, result)
		})
	}
}

// Helper functions for test pointers
func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}
