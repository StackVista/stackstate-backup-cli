package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const invalidConfigYAML = `
elasticsearch:
  service:
    name: ""
    port: 0
`

// loadTestData loads test configuration from testdata files
func loadTestData(t *testing.T, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filename))
	require.NoError(t, err, "failed to read test data file: %s", filename)
	return string(data)
}

func TestLoadConfig_FromConfigMapOnly(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	validConfigYAML := loadTestData(t, "validConfigMapOnly.yaml")

	// Create ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-config",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"config": validConfigYAML,
		},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Load config
	config, err := LoadConfig(fakeClient, "test-ns", "backup-config", "")

	// Assertions
	require.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "suse-observability-elasticsearch-master-headless", config.Elasticsearch.Service.Name)
	assert.Equal(t, 9200, config.Elasticsearch.Service.Port)
	assert.Equal(t, "sts-backup", config.Elasticsearch.SnapshotRepository.Name)
	assert.Equal(t, "configmap-access-key", config.Elasticsearch.SnapshotRepository.AccessKey)
	assert.Equal(t, "configmap-secret-key", config.Elasticsearch.SnapshotRepository.SecretKey)
}

func TestLoadConfig_CompleteConfiguration(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	validConfigYAML := loadTestData(t, "validConfigMapConfig.yaml")
	secretOverrideYAML := loadTestData(t, "validSecretConfig.yaml")

	// Create ConfigMap with non-sensitive configuration
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-config",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"config": validConfigYAML,
		},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Create Secret with sensitive credentials
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"config": []byte(secretOverrideYAML),
		},
	}
	_, err = fakeClient.CoreV1().Secrets("test-ns").Create(
		context.Background(), secret, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Load config - production pattern: ConfigMap + Secret
	config, err := LoadConfig(fakeClient, "test-ns", "backup-config", "backup-secret")

	// Comprehensive assertions
	require.NoError(t, err)
	assert.NotNil(t, config)

	// Service config
	assert.Equal(t, "suse-observability-elasticsearch-master-headless", config.Elasticsearch.Service.Name)
	assert.Equal(t, 9200, config.Elasticsearch.Service.Port)
	assert.Equal(t, 9200, config.Elasticsearch.Service.LocalPortForwardPort)

	// Restore config
	assert.Equal(t, "observability.suse.com/scalable-during-es-restore=true", config.Elasticsearch.Restore.ScaleDownLabelSelector)
	assert.Equal(t, "sts", config.Elasticsearch.Restore.IndexPrefix)
	assert.Equal(t, ".ds-sts_k8s_logs", config.Elasticsearch.Restore.DatastreamIndexPrefix)
	assert.Equal(t, "sts_k8s_logs", config.Elasticsearch.Restore.DatastreamName)
	assert.Equal(t, "sts*,.ds-sts_k8s_logs*", config.Elasticsearch.Restore.IndicesPattern)
	assert.Equal(t, "sts-backup", config.Elasticsearch.Restore.Repository)

	// Snapshot repository config
	assert.Equal(t, "sts-backup", config.Elasticsearch.SnapshotRepository.Name)
	assert.Equal(t, "sts-elasticsearch-backup", config.Elasticsearch.SnapshotRepository.Bucket)
	assert.Equal(t, "suse-observability-minio:9000", config.Elasticsearch.SnapshotRepository.Endpoint)
	assert.Equal(t, "", config.Elasticsearch.SnapshotRepository.BasePath)
	// Credentials come from Secret
	assert.Equal(t, "secret-access-key", config.Elasticsearch.SnapshotRepository.AccessKey)
	assert.Equal(t, "secret-secret-key", config.Elasticsearch.SnapshotRepository.SecretKey)

	// SLM config
	assert.Equal(t, "auto-sts-backup", config.Elasticsearch.SLM.Name)
	assert.Equal(t, "0 0 3 * * ?", config.Elasticsearch.SLM.Schedule)
	assert.Equal(t, "<sts-backup-{now{yyyyMMdd-HHmm}}>", config.Elasticsearch.SLM.SnapshotTemplateName)
	assert.Equal(t, "sts-backup", config.Elasticsearch.SLM.Repository)
	assert.Equal(t, "sts*", config.Elasticsearch.SLM.Indices)
	assert.Equal(t, "30d", config.Elasticsearch.SLM.RetentionExpireAfter)
	assert.Equal(t, 5, config.Elasticsearch.SLM.RetentionMinCount)
	assert.Equal(t, 30, config.Elasticsearch.SLM.RetentionMaxCount)
}

func TestLoadConfig_WithSecretOverride(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	validConfigYAML := loadTestData(t, "validConfigMapOnly.yaml")
	secretOverrideYAML := loadTestData(t, "validSecretConfig.yaml")

	// Create ConfigMap with credentials
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-config",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"config": validConfigYAML,
		},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Create Secret with different credentials
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"config": []byte(secretOverrideYAML),
		},
	}
	_, err = fakeClient.CoreV1().Secrets("test-ns").Create(
		context.Background(), secret, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Load config
	config, err := LoadConfig(fakeClient, "test-ns", "backup-config", "backup-secret")

	// Assertions - Secret should override ConfigMap credentials
	require.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "suse-observability-elasticsearch-master-headless", config.Elasticsearch.Service.Name)
	// Verify Secret overrides ConfigMap: secret-access-key overrides configmap-access-key
	assert.Equal(t, "secret-access-key", config.Elasticsearch.SnapshotRepository.AccessKey)
	assert.Equal(t, "secret-secret-key", config.Elasticsearch.SnapshotRepository.SecretKey)
}

func TestLoadConfig_ConfigMapNotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	// Try to load non-existent ConfigMap
	config, err := LoadConfig(fakeClient, "test-ns", "nonexistent", "")

	// Assertions
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to get ConfigMap")
}

func TestLoadConfig_ConfigMapMissingConfigKey(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	validConfigYAML := loadTestData(t, "validConfigMapOnly.yaml")

	// Create ConfigMap without 'config' key
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-config",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"wrong-key": validConfigYAML,
		},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Load config
	config, err := LoadConfig(fakeClient, "test-ns", "backup-config", "")

	// Assertions
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "does not contain 'config' key")
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	// Create ConfigMap with invalid YAML
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-config",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"config": "invalid: yaml: content: [unclosed",
		},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Load config
	config, err := LoadConfig(fakeClient, "test-ns", "backup-config", "")

	// Assertions
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to parse ConfigMap config")
}

func TestLoadConfig_ValidationFails(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	// Create ConfigMap with invalid config (missing required fields)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-config",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"config": invalidConfigYAML,
		},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Load config
	config, err := LoadConfig(fakeClient, "test-ns", "backup-config", "")

	// Assertions
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "configuration validation failed")
}

func TestLoadConfig_SecretNotFoundWarning(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	validConfigYAML := loadTestData(t, "validConfigMapOnly.yaml")

	// Create only ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backup-config",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"config": validConfigYAML,
		},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Load config with non-existent secret (should succeed with warning)
	config, err := LoadConfig(fakeClient, "test-ns", "backup-config", "nonexistent-secret")

	// Assertions - should succeed as secret is optional
	require.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "suse-observability-elasticsearch-master-headless", config.Elasticsearch.Service.Name)
}

func TestLoadConfig_EmptyConfigMapName(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	// Try to load with empty ConfigMap name
	config, err := LoadConfig(fakeClient, "test-ns", "", "")

	// Should fail - ConfigMap is required
	assert.Error(t, err)
	assert.Nil(t, config)
}

func TestNewContext(t *testing.T) {
	ctx := NewContext()

	assert.NotNil(t, ctx)
	assert.NotNil(t, ctx.Config)
	assert.Equal(t, "", ctx.Config.Namespace)
	assert.Equal(t, "", ctx.Config.Kubeconfig)
	assert.False(t, ctx.Config.Debug)
	assert.False(t, ctx.Config.Quiet)
	assert.Equal(t, "", ctx.Config.ConfigMapName)
	assert.Equal(t, "", ctx.Config.SecretName)
	assert.Equal(t, "", ctx.Config.OutputFormat)
}

func TestCLIConfig_Defaults(t *testing.T) {
	config := &CLIConfig{}

	// Verify zero values
	assert.Equal(t, "", config.Namespace)
	assert.Equal(t, "", config.Kubeconfig)
	assert.False(t, config.Debug)
	assert.False(t, config.Quiet)
	assert.Equal(t, "", config.ConfigMapName)
	assert.Equal(t, "", config.SecretName)
	assert.Equal(t, "", config.OutputFormat)
}

//nolint:funlen
func TestConfig_StructValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name: "valid config",
			config: &Config{
				Elasticsearch: ElasticsearchConfig{
					Service: ServiceConfig{
						Name:                 "es-master",
						Port:                 9200,
						LocalPortForwardPort: 9200,
					},
					Restore: RestoreConfig{
						ScaleDownLabelSelector: "app=test",
						IndexPrefix:            "sts_",
						DatastreamIndexPrefix:  "sts_k8s",
						DatastreamName:         "sts_k8s",
						IndicesPattern:         "*",
						Repository:             "repo",
					},
					SnapshotRepository: SnapshotRepositoryConfig{
						Name:      "repo",
						Bucket:    "bucket",
						Endpoint:  "endpoint",
						AccessKey: "key",
						SecretKey: "secret",
					},
					SLM: SLMConfig{
						Name:                 "slm",
						Schedule:             "0 0 * * *",
						SnapshotTemplateName: "snap",
						Repository:           "repo",
						Indices:              "*",
						RetentionExpireAfter: "30d",
						RetentionMinCount:    1,
						RetentionMaxCount:    10,
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid port number",
			config: &Config{
				Elasticsearch: ElasticsearchConfig{
					Service: ServiceConfig{
						Name:                 "es-master",
						Port:                 0, // Invalid
						LocalPortForwardPort: 9200,
					},
					Restore: RestoreConfig{
						ScaleDownLabelSelector: "app=test",
						IndexPrefix:            "sts_",
						DatastreamIndexPrefix:  "sts_k8s",
						DatastreamName:         "sts_k8s",
						IndicesPattern:         "*",
						Repository:             "repo",
					},
					SnapshotRepository: SnapshotRepositoryConfig{
						Name:      "repo",
						Bucket:    "bucket",
						Endpoint:  "endpoint",
						AccessKey: "key",
						SecretKey: "secret",
					},
					SLM: SLMConfig{
						Name:                 "slm",
						Schedule:             "0 0 * * *",
						SnapshotTemplateName: "snap",
						Repository:           "repo",
						Indices:              "*",
						RetentionExpireAfter: "30d",
						RetentionMinCount:    1,
						RetentionMaxCount:    10,
					},
				},
			},
			expectError: true,
		},
		{
			name: "invalid retention count",
			config: &Config{
				Elasticsearch: ElasticsearchConfig{
					Service: ServiceConfig{
						Name:                 "es-master",
						Port:                 9200,
						LocalPortForwardPort: 9200,
					},
					Restore: RestoreConfig{
						ScaleDownLabelSelector: "app=test",
						IndexPrefix:            "sts_",
						DatastreamIndexPrefix:  "sts_k8s",
						DatastreamName:         "sts_k8s",
						IndicesPattern:         "*",
						Repository:             "repo",
					},
					SnapshotRepository: SnapshotRepositoryConfig{
						Name:      "repo",
						Bucket:    "bucket",
						Endpoint:  "endpoint",
						AccessKey: "key",
						SecretKey: "secret",
					},
					SLM: SLMConfig{
						Name:                 "slm",
						Schedule:             "0 0 * * *",
						SnapshotTemplateName: "snap",
						Repository:           "repo",
						Indices:              "*",
						RetentionExpireAfter: "30d",
						RetentionMinCount:    0, // Invalid - must be >= 1
						RetentionMaxCount:    10,
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use validator directly to test struct validation
			validate := validator.New()
			err := validate.Struct(tt.config)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
