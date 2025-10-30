// Package config provides configuration management for the backup CLI tool.
// It supports loading configuration from Kubernetes ConfigMaps and Secrets
// with a merge strategy that allows ConfigMap to be overridden by Secret.
package config

import (
	"context"
	"fmt"

	"dario.cat/mergo"
	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Config represents the merged configuration from ConfigMap and Secret
type Config struct {
	Kubernetes    KubernetesConfig    `yaml:"kubernetes"`
	Elasticsearch ElasticsearchConfig `yaml:"elasticsearch" validate:"required"`
	Minio         MinioConfig         `yaml:"minio" validate:"required"`
	Stackgraph    StackgraphConfig    `yaml:"stackgraph" validate:"required"`
}

// KubernetesConfig holds Kubernetes-wide configuration
type KubernetesConfig struct {
	CommonLabels map[string]string `yaml:"commonLabels"`
}

// ElasticsearchConfig holds Elasticsearch-specific configuration
type ElasticsearchConfig struct {
	Service            ServiceConfig            `yaml:"service" validate:"required"`
	Restore            RestoreConfig            `yaml:"restore" validate:"required"`
	SnapshotRepository SnapshotRepositoryConfig `yaml:"snapshotRepository" validate:"required"`
	SLM                SLMConfig                `yaml:"slm" validate:"required"`
}

// RestoreConfig holds restore-specific configuration
type RestoreConfig struct {
	ScaleDownLabelSelector string `yaml:"scaleDownLabelSelector" validate:"required"`
	IndexPrefix            string `yaml:"indexPrefix" validate:"required"`
	DatastreamIndexPrefix  string `yaml:"datastreamIndexPrefix" validate:"required"`
	DatastreamName         string `yaml:"datastreamName" validate:"required"`
	IndicesPattern         string `yaml:"indicesPattern" validate:"required"`
	Repository             string `yaml:"repository" validate:"required"`
}

// SnapshotRepositoryConfig holds snapshot repository configuration
type SnapshotRepositoryConfig struct {
	Name      string `yaml:"name" validate:"required"`
	Bucket    string `yaml:"bucket" validate:"required"`
	Endpoint  string `yaml:"endpoint" validate:"required"`
	BasePath  string `yaml:"basepath"`
	AccessKey string `yaml:"accessKey" validate:"required"` // From secret
	SecretKey string `yaml:"secretKey" validate:"required"` // From secret
}

// SLMConfig holds Snapshot Lifecycle Management configuration
type SLMConfig struct {
	Name                 string `yaml:"name" validate:"required"`
	Schedule             string `yaml:"schedule" validate:"required"`
	SnapshotTemplateName string `yaml:"snapshotTemplateName" validate:"required"`
	Repository           string `yaml:"repository" validate:"required"`
	Indices              string `yaml:"indices" validate:"required"`
	RetentionExpireAfter string `yaml:"retentionExpireAfter" validate:"required"`
	RetentionMinCount    int    `yaml:"retentionMinCount" validate:"required,min=1"`
	RetentionMaxCount    int    `yaml:"retentionMaxCount" validate:"required,min=1"`
}

// ServiceConfig holds service connection details
type ServiceConfig struct {
	Name                 string `yaml:"name" validate:"required"`
	Port                 int    `yaml:"port" validate:"required,min=1,max=65535"`
	LocalPortForwardPort int    `yaml:"localPortForwardPort" validate:"required,min=1,max=65535"`
}

// MinioConfig holds Minio-specific configuration
type MinioConfig struct {
	Service   ServiceConfig `yaml:"service" validate:"required"`
	AccessKey string        `yaml:"accessKey" validate:"required"` // From secret
	SecretKey string        `yaml:"secretKey" validate:"required"` // From secret
}

// StackgraphConfig holds Stackgraph backup-specific configuration
type StackgraphConfig struct {
	Bucket           string                  `yaml:"bucket" validate:"required"`
	S3Prefix         string                  `yaml:"s3Prefix"`
	MultipartArchive bool                    `yaml:"multipartArchive" validate:"boolean"`
	Restore          StackgraphRestoreConfig `yaml:"restore" validate:"required"`
}

// StackgraphRestoreConfig holds Stackgraph restore-specific configuration
type StackgraphRestoreConfig struct {
	ScaleDownLabelSelector     string    `yaml:"scaleDownLabelSelector" validate:"required"`
	LoggingConfigConfigMapName string    `yaml:"loggingConfigConfigMap" validate:"required"`
	ZookeeperQuorum            string    `yaml:"zookeeperQuorum" validate:"required"`
	Job                        JobConfig `yaml:"job" validate:"required"`
	PVC                        PVCConfig `yaml:"pvc" validate:"required"`
}

// PVCConfig holds PersistentVolumeClaim configuration
type PVCConfig struct {
	Size             string   `yaml:"size" validate:"required"`
	AccessModes      []string `yaml:"accessModes"`
	StorageClassName string   `yaml:"storageClassName"`
}

// JobConfig holds Kubernetes Job configuration that can be applied to backup/restore jobs
type JobConfig struct {
	Labels                   map[string]string    `yaml:"labels"`
	ImagePullSecrets         []LocalObjectRef     `yaml:"imagePullSecrets"`
	SecurityContext          PodSecurityContext   `yaml:"securityContext"`
	NodeSelector             map[string]string    `yaml:"nodeSelector"`
	Tolerations              []Toleration         `yaml:"tolerations"`
	Affinity                 *Affinity            `yaml:"affinity"`
	Resources                ResourceRequirements `yaml:"resources" validate:"required"`
	ContainerSecurityContext *SecurityContext     `yaml:"containerSecurityContext"`
	Image                    string               `yaml:"image" validate:"required"`
	WaitImage                string               `yaml:"waitImage" validate:"required"`
}

// LocalObjectRef represents a reference to a local object by name
type LocalObjectRef struct {
	Name string `yaml:"name" validate:"required"`
}

// PodSecurityContext holds pod-level security context settings
type PodSecurityContext struct {
	FSGroup      *int64 `yaml:"fsGroup"`
	RunAsGroup   *int64 `yaml:"runAsGroup"`
	RunAsNonRoot *bool  `yaml:"runAsNonRoot"`
	RunAsUser    *int64 `yaml:"runAsUser"`
}

// SecurityContext holds container-level security context settings
type SecurityContext struct {
	AllowPrivilegeEscalation *bool  `yaml:"allowPrivilegeEscalation"`
	RunAsNonRoot             *bool  `yaml:"runAsNonRoot"`
	RunAsUser                *int64 `yaml:"runAsUser"`
}

// Toleration represents a pod toleration
type Toleration struct {
	Key      string `yaml:"key"`
	Operator string `yaml:"operator"`
	Value    string `yaml:"value"`
	Effect   string `yaml:"effect"`
}

// Affinity represents pod affinity and anti-affinity settings
type Affinity struct {
	NodeAffinity    *NodeAffinity    `yaml:"nodeAffinity"`
	PodAffinity     *PodAffinity     `yaml:"podAffinity"`
	PodAntiAffinity *PodAntiAffinity `yaml:"podAntiAffinity"`
}

// NodeAffinity represents node affinity scheduling rules
type NodeAffinity struct {
	RequiredDuringSchedulingIgnoredDuringExecution  *NodeSelector             `yaml:"requiredDuringSchedulingIgnoredDuringExecution"`
	PreferredDuringSchedulingIgnoredDuringExecution []PreferredSchedulingTerm `yaml:"preferredDuringSchedulingIgnoredDuringExecution"`
}

// NodeSelector represents node selector requirements
type NodeSelector struct {
	NodeSelectorTerms []NodeSelectorTerm `yaml:"nodeSelectorTerms"`
}

// NodeSelectorTerm represents node selector term
type NodeSelectorTerm struct {
	MatchExpressions []NodeSelectorRequirement `yaml:"matchExpressions"`
	MatchFields      []NodeSelectorRequirement `yaml:"matchFields"`
}

// NodeSelectorRequirement represents a node selector requirement
type NodeSelectorRequirement struct {
	Key      string   `yaml:"key"`
	Operator string   `yaml:"operator"`
	Values   []string `yaml:"values"`
}

// PreferredSchedulingTerm represents a preferred scheduling term
type PreferredSchedulingTerm struct {
	Weight     int32            `yaml:"weight"`
	Preference NodeSelectorTerm `yaml:"preference"`
}

// PodAffinity represents pod affinity scheduling rules
type PodAffinity struct {
	RequiredDuringSchedulingIgnoredDuringExecution  []PodAffinityTerm         `yaml:"requiredDuringSchedulingIgnoredDuringExecution"`
	PreferredDuringSchedulingIgnoredDuringExecution []WeightedPodAffinityTerm `yaml:"preferredDuringSchedulingIgnoredDuringExecution"`
}

// PodAntiAffinity represents pod anti-affinity scheduling rules
type PodAntiAffinity struct {
	RequiredDuringSchedulingIgnoredDuringExecution  []PodAffinityTerm         `yaml:"requiredDuringSchedulingIgnoredDuringExecution"`
	PreferredDuringSchedulingIgnoredDuringExecution []WeightedPodAffinityTerm `yaml:"preferredDuringSchedulingIgnoredDuringExecution"`
}

// PodAffinityTerm represents pod affinity term
type PodAffinityTerm struct {
	LabelSelector *LabelSelector `yaml:"labelSelector"`
	Namespaces    []string       `yaml:"namespaces"`
	TopologyKey   string         `yaml:"topologyKey"`
}

// WeightedPodAffinityTerm represents weighted pod affinity term
type WeightedPodAffinityTerm struct {
	Weight          int32           `yaml:"weight"`
	PodAffinityTerm PodAffinityTerm `yaml:"podAffinityTerm"`
}

// LabelSelector represents a label selector
type LabelSelector struct {
	MatchLabels      map[string]string          `yaml:"matchLabels"`
	MatchExpressions []LabelSelectorRequirement `yaml:"matchExpressions"`
}

// LabelSelectorRequirement represents a label selector requirement
type LabelSelectorRequirement struct {
	Key      string   `yaml:"key"`
	Operator string   `yaml:"operator"`
	Values   []string `yaml:"values"`
}

// ResourceRequirements holds resource limits and requests
type ResourceRequirements struct {
	Limits   ResourceList `yaml:"limits" validate:"required"`
	Requests ResourceList `yaml:"requests" validate:"required"`
}

// ResourceList holds resource quantities
type ResourceList struct {
	CPU              string `yaml:"cpu" validate:"required"`
	Memory           string `yaml:"memory" validate:"required"`
	EphemeralStorage string `yaml:"ephemeralStorage"`
}

// LoadConfig loads and merges configuration from ConfigMap and Secret
// ConfigMap provides base configuration, Secret overrides it
// All required fields must be present after merging, validated with validator
func LoadConfig(clientset kubernetes.Interface, namespace, configMapName, secretName string) (*Config, error) {
	ctx := context.Background()
	config := &Config{}

	// Load ConfigMap if it exists
	if configMapName != "" {
		cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get ConfigMap '%s': %w", configMapName, err)
		}

		if configData, ok := cm.Data["config"]; ok {
			if err := yaml.Unmarshal([]byte(configData), config); err != nil {
				return nil, fmt.Errorf("failed to parse ConfigMap config: %w", err)
			}
		} else {
			return nil, fmt.Errorf("ConfigMap '%s' does not contain 'config' key", configMapName)
		}
	}

	// Load Secret if it exists (overrides ConfigMap)
	if secretName != "" {
		secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			// Secret is optional - only used for overrides
			fmt.Printf("Warningf: Secret '%s' not found, using ConfigMap only\n", secretName)
		} else {
			if configData, ok := secret.Data["config"]; ok {
				var secretConfig Config
				if err := yaml.Unmarshal(configData, &secretConfig); err != nil {
					return nil, fmt.Errorf("failed to parse Secret config: %w", err)
				}
				// Merge Secret config into base config (non-zero values override)
				if err := mergo.Merge(config, secretConfig, mergo.WithOverride); err != nil {
					return nil, fmt.Errorf("failed to merge Secret config: %w", err)
				}
			}
		}
	}

	// Validate the merged configuration
	validate := validator.New()
	if err := validate.Struct(config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

type CLIGlobalFlags struct {
	Namespace     string
	Kubeconfig    string
	Debug         bool
	Quiet         bool
	ConfigMapName string
	SecretName    string
	OutputFormat  string // table, json
}

func NewCLIGlobalFlags() *CLIGlobalFlags {
	return &CLIGlobalFlags{}
}
