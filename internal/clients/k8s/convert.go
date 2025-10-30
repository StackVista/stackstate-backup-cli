package k8s

import (
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConvertResources converts config.ResourceRequirements to k8s native ResourceRequirements
func ConvertResources(r config.ResourceRequirements) corev1.ResourceRequirements {
	result := corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{},
		Requests: corev1.ResourceList{},
	}

	// Parse limits
	if r.Limits.CPU != "" {
		result.Limits[corev1.ResourceCPU] = resource.MustParse(r.Limits.CPU)
	}
	if r.Limits.Memory != "" {
		result.Limits[corev1.ResourceMemory] = resource.MustParse(r.Limits.Memory)
	}
	if r.Limits.EphemeralStorage != "" {
		result.Limits[corev1.ResourceEphemeralStorage] = resource.MustParse(r.Limits.EphemeralStorage)
	}

	// Parse requests
	if r.Requests.CPU != "" {
		result.Requests[corev1.ResourceCPU] = resource.MustParse(r.Requests.CPU)
	}
	if r.Requests.Memory != "" {
		result.Requests[corev1.ResourceMemory] = resource.MustParse(r.Requests.Memory)
	}
	if r.Requests.EphemeralStorage != "" {
		result.Requests[corev1.ResourceEphemeralStorage] = resource.MustParse(r.Requests.EphemeralStorage)
	}

	return result
}

// ConvertImagePullSecrets converts config.LocalObjectRef slice to k8s native LocalObjectReference slice
func ConvertImagePullSecrets(refs []config.LocalObjectRef) []corev1.LocalObjectReference {
	if len(refs) == 0 {
		return nil
	}
	result := make([]corev1.LocalObjectReference, len(refs))
	for i, ref := range refs {
		result[i] = corev1.LocalObjectReference{
			Name: ref.Name,
		}
	}
	return result
}

// ConvertPodSecurityContext converts config.PodSecurityContext to k8s native PodSecurityContext
func ConvertPodSecurityContext(sc *config.PodSecurityContext) *corev1.PodSecurityContext {
	if sc == nil {
		return nil
	}
	return &corev1.PodSecurityContext{
		FSGroup:      sc.FSGroup,
		RunAsGroup:   sc.RunAsGroup,
		RunAsNonRoot: sc.RunAsNonRoot,
		RunAsUser:    sc.RunAsUser,
	}
}

// ConvertSecurityContext converts config.SecurityContext to k8s native SecurityContext
func ConvertSecurityContext(sc *config.SecurityContext) *corev1.SecurityContext {
	if sc == nil {
		return nil
	}
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: sc.AllowPrivilegeEscalation,
		RunAsNonRoot:             sc.RunAsNonRoot,
		RunAsUser:                sc.RunAsUser,
	}
}

// ConvertTolerations converts config.Toleration slice to k8s native Toleration slice
func ConvertTolerations(tolerations []config.Toleration) []corev1.Toleration {
	result := make([]corev1.Toleration, len(tolerations))
	for i, t := range tolerations {
		result[i] = corev1.Toleration{
			Key:      t.Key,
			Operator: corev1.TolerationOperator(t.Operator),
			Value:    t.Value,
			Effect:   corev1.TaintEffect(t.Effect),
		}
	}
	return result
}

// ConvertAffinity converts config.Affinity to k8s native Affinity
func ConvertAffinity(affinity *config.Affinity) *corev1.Affinity {
	if affinity == nil {
		return nil
	}

	result := &corev1.Affinity{}

	if affinity.NodeAffinity != nil {
		result.NodeAffinity = ConvertNodeAffinity(affinity.NodeAffinity)
	}

	if affinity.PodAffinity != nil {
		result.PodAffinity = ConvertPodAffinity(affinity.PodAffinity)
	}

	if affinity.PodAntiAffinity != nil {
		result.PodAntiAffinity = ConvertPodAntiAffinity(affinity.PodAntiAffinity)
	}

	return result
}

// ConvertNodeAffinity converts config.NodeAffinity to k8s native NodeAffinity
func ConvertNodeAffinity(na *config.NodeAffinity) *corev1.NodeAffinity {
	if na == nil {
		return nil
	}

	result := &corev1.NodeAffinity{}

	if na.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		result.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{
			NodeSelectorTerms: make([]corev1.NodeSelectorTerm, len(na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)),
		}
		for i, term := range na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			result.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[i] = ConvertNodeSelectorTerm(term)
		}
	}

	if len(na.PreferredDuringSchedulingIgnoredDuringExecution) > 0 {
		result.PreferredDuringSchedulingIgnoredDuringExecution = make([]corev1.PreferredSchedulingTerm, len(na.PreferredDuringSchedulingIgnoredDuringExecution))
		for i, term := range na.PreferredDuringSchedulingIgnoredDuringExecution {
			result.PreferredDuringSchedulingIgnoredDuringExecution[i] = corev1.PreferredSchedulingTerm{
				Weight:     term.Weight,
				Preference: ConvertNodeSelectorTerm(term.Preference),
			}
		}
	}

	return result
}

// ConvertNodeSelectorTerm converts config.NodeSelectorTerm to k8s native NodeSelectorTerm
func ConvertNodeSelectorTerm(term config.NodeSelectorTerm) corev1.NodeSelectorTerm {
	result := corev1.NodeSelectorTerm{}

	if len(term.MatchExpressions) > 0 {
		result.MatchExpressions = make([]corev1.NodeSelectorRequirement, len(term.MatchExpressions))
		for i, expr := range term.MatchExpressions {
			result.MatchExpressions[i] = corev1.NodeSelectorRequirement{
				Key:      expr.Key,
				Operator: corev1.NodeSelectorOperator(expr.Operator),
				Values:   expr.Values,
			}
		}
	}

	if len(term.MatchFields) > 0 {
		result.MatchFields = make([]corev1.NodeSelectorRequirement, len(term.MatchFields))
		for i, field := range term.MatchFields {
			result.MatchFields[i] = corev1.NodeSelectorRequirement{
				Key:      field.Key,
				Operator: corev1.NodeSelectorOperator(field.Operator),
				Values:   field.Values,
			}
		}
	}

	return result
}

// ConvertPodAffinity converts config.PodAffinity to k8s native PodAffinity
func ConvertPodAffinity(pa *config.PodAffinity) *corev1.PodAffinity {
	if pa == nil {
		return nil
	}

	result := &corev1.PodAffinity{}

	if len(pa.RequiredDuringSchedulingIgnoredDuringExecution) > 0 {
		result.RequiredDuringSchedulingIgnoredDuringExecution = make([]corev1.PodAffinityTerm, len(pa.RequiredDuringSchedulingIgnoredDuringExecution))
		for i, term := range pa.RequiredDuringSchedulingIgnoredDuringExecution {
			result.RequiredDuringSchedulingIgnoredDuringExecution[i] = ConvertPodAffinityTerm(term)
		}
	}

	if len(pa.PreferredDuringSchedulingIgnoredDuringExecution) > 0 {
		result.PreferredDuringSchedulingIgnoredDuringExecution = make([]corev1.WeightedPodAffinityTerm, len(pa.PreferredDuringSchedulingIgnoredDuringExecution))
		for i, term := range pa.PreferredDuringSchedulingIgnoredDuringExecution {
			result.PreferredDuringSchedulingIgnoredDuringExecution[i] = corev1.WeightedPodAffinityTerm{
				Weight:          term.Weight,
				PodAffinityTerm: ConvertPodAffinityTerm(term.PodAffinityTerm),
			}
		}
	}

	return result
}

// ConvertPodAntiAffinity converts config.PodAntiAffinity to k8s native PodAntiAffinity
func ConvertPodAntiAffinity(paa *config.PodAntiAffinity) *corev1.PodAntiAffinity {
	if paa == nil {
		return nil
	}

	result := &corev1.PodAntiAffinity{}

	if len(paa.RequiredDuringSchedulingIgnoredDuringExecution) > 0 {
		result.RequiredDuringSchedulingIgnoredDuringExecution = make([]corev1.PodAffinityTerm, len(paa.RequiredDuringSchedulingIgnoredDuringExecution))
		for i, term := range paa.RequiredDuringSchedulingIgnoredDuringExecution {
			result.RequiredDuringSchedulingIgnoredDuringExecution[i] = ConvertPodAffinityTerm(term)
		}
	}

	if len(paa.PreferredDuringSchedulingIgnoredDuringExecution) > 0 {
		result.PreferredDuringSchedulingIgnoredDuringExecution = make([]corev1.WeightedPodAffinityTerm, len(paa.PreferredDuringSchedulingIgnoredDuringExecution))
		for i, term := range paa.PreferredDuringSchedulingIgnoredDuringExecution {
			result.PreferredDuringSchedulingIgnoredDuringExecution[i] = corev1.WeightedPodAffinityTerm{
				Weight:          term.Weight,
				PodAffinityTerm: ConvertPodAffinityTerm(term.PodAffinityTerm),
			}
		}
	}

	return result
}

// ConvertPodAffinityTerm converts config.PodAffinityTerm to k8s native PodAffinityTerm
func ConvertPodAffinityTerm(term config.PodAffinityTerm) corev1.PodAffinityTerm {
	result := corev1.PodAffinityTerm{
		Namespaces:  term.Namespaces,
		TopologyKey: term.TopologyKey,
	}

	if term.LabelSelector != nil {
		result.LabelSelector = &metav1.LabelSelector{
			MatchLabels: term.LabelSelector.MatchLabels,
		}
		if len(term.LabelSelector.MatchExpressions) > 0 {
			result.LabelSelector.MatchExpressions = make([]metav1.LabelSelectorRequirement, len(term.LabelSelector.MatchExpressions))
			for i, expr := range term.LabelSelector.MatchExpressions {
				result.LabelSelector.MatchExpressions[i] = metav1.LabelSelectorRequirement{
					Key:      expr.Key,
					Operator: metav1.LabelSelectorOperator(expr.Operator),
					Values:   expr.Values,
				}
			}
		}
	}

	return result
}
