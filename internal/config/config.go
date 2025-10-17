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
	Elasticsearch ElasticsearchConfig `yaml:"elasticsearch" validate:"required"`
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

type Context struct {
	Config *CLIConfig
}

type CLIConfig struct {
	Namespace     string
	Kubeconfig    string
	Debug         bool
	Quiet         bool
	ConfigMapName string
	SecretName    string
	OutputFormat  string // table, json
}

func NewContext() *Context {
	return &Context{
		Config: &CLIConfig{},
	}
}
