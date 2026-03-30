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
	fakeClient := fake.NewClientset()
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
	// Verify legacy mode
	assert.True(t, config.IsLegacyMode())
	assert.True(t, config.GlobalBackupEnabled())
}

func TestLoadConfig_Storage_FromConfigMapOnly(t *testing.T) {
	fakeClient := fake.NewClientset()
	validConfigYAML := loadTestData(t, "validStorageConfigMapOnly.yaml")

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
	// Verify new storage mode (not legacy)
	assert.False(t, config.IsLegacyMode())
	assert.True(t, config.GlobalBackupEnabled())
	// Verify storage accessor methods return storage config values
	assert.Equal(t, "suse-observability-storage", config.GetStorageService().Name)
	assert.Equal(t, 9000, config.GetStorageService().Port)
	assert.Equal(t, "storageadmin", config.GetStorageAccessKey())
	assert.Equal(t, "storageadmin", config.GetStorageSecretKey())
}

func TestGlobalBackupEnabled_StorageMode_Disabled(t *testing.T) {
	config := &Config{
		Storage: StorageConfig{
			GlobalBackupEnabled: false,
			Service: ServiceConfig{
				Name:                 "storage",
				Port:                 9000,

			},
		},
	}

	assert.False(t, config.IsLegacyMode())
	assert.False(t, config.GlobalBackupEnabled())
}

func TestGlobalBackupEnabled_StorageMode_Enabled(t *testing.T) {
	config := &Config{
		Storage: StorageConfig{
			GlobalBackupEnabled: true,
			Service: ServiceConfig{
				Name:                 "storage",
				Port:                 9000,

			},
		},
	}

	assert.False(t, config.IsLegacyMode())
	assert.True(t, config.GlobalBackupEnabled())
}

func TestGlobalBackupEnabled_LegacyMode(t *testing.T) {
	config := &Config{
		Minio: MinioConfig{
			Enabled: false,
			Service: ServiceConfig{
				Name:                 "minio",
				Port:                 9000,

			},
		},
	}

	assert.True(t, config.IsLegacyMode())
	assert.False(t, config.GlobalBackupEnabled())

	config.Minio.Enabled = true
	assert.True(t, config.GlobalBackupEnabled())
}

func TestLoadConfig_CompleteConfiguration(t *testing.T) {
	fakeClient := fake.NewClientset()
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

func TestLoadConfig_Storage_CompleteConfiguration(t *testing.T) {
	fakeClient := fake.NewClientset()
	validConfigYAML := loadTestData(t, "validStorageConfigMapConfig.yaml")
	secretOverrideYAML := loadTestData(t, "validStorageSecretConfig.yaml")

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

	// Verify new storage mode (not legacy)
	assert.False(t, config.IsLegacyMode())
	assert.True(t, config.GlobalBackupEnabled())

	// Service config
	assert.Equal(t, "suse-observability-elasticsearch-master-headless", config.Elasticsearch.Service.Name)
	assert.Equal(t, 9200, config.Elasticsearch.Service.Port)


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
	assert.Equal(t, "suse-observability-storage:9000", config.Elasticsearch.SnapshotRepository.Endpoint)
	assert.Equal(t, "", config.Elasticsearch.SnapshotRepository.BasePath)
	// Credentials come from Secret
	assert.Equal(t, "secret-access-key", config.Elasticsearch.SnapshotRepository.AccessKey)
	assert.Equal(t, "secret-secret-key", config.Elasticsearch.SnapshotRepository.SecretKey)

	// Storage accessor methods should return secret-overridden values
	assert.Equal(t, "suse-observability-storage", config.GetStorageService().Name)
	assert.Equal(t, 9000, config.GetStorageService().Port)
	assert.Equal(t, "secret-storage-access-key", config.GetStorageAccessKey())
	assert.Equal(t, "secret-storage-secret-key", config.GetStorageSecretKey())

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
	fakeClient := fake.NewClientset()
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

func TestLoadConfig_Storage_WithSecretOverride(t *testing.T) {
	fakeClient := fake.NewClientset()
	validConfigYAML := loadTestData(t, "validStorageConfigMapOnly.yaml")
	secretOverrideYAML := loadTestData(t, "validStorageSecretConfig.yaml")

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
	assert.False(t, config.IsLegacyMode())
	assert.Equal(t, "suse-observability-elasticsearch-master-headless", config.Elasticsearch.Service.Name)
	// Verify Secret overrides ConfigMap: secret-access-key overrides configmap-access-key
	assert.Equal(t, "secret-access-key", config.Elasticsearch.SnapshotRepository.AccessKey)
	assert.Equal(t, "secret-secret-key", config.Elasticsearch.SnapshotRepository.SecretKey)
	// Verify Secret overrides storage credentials
	assert.Equal(t, "secret-storage-access-key", config.GetStorageAccessKey())
	assert.Equal(t, "secret-storage-secret-key", config.GetStorageSecretKey())
}

func TestLoadConfig_ConfigMapNotFound(t *testing.T) {
	fakeClient := fake.NewClientset()

	// Try to load non-existent ConfigMap
	config, err := LoadConfig(fakeClient, "test-ns", "nonexistent", "")

	// Assertions
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to get ConfigMap")
}

func TestLoadConfig_ConfigMapMissingConfigKey(t *testing.T) {
	fakeClient := fake.NewClientset()
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
	fakeClient := fake.NewClientset()

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
	fakeClient := fake.NewClientset()

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
	fakeClient := fake.NewClientset()
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

func TestLoadConfig_Storage_SecretNotFoundWarning(t *testing.T) {
	fakeClient := fake.NewClientset()
	validConfigYAML := loadTestData(t, "validStorageConfigMapOnly.yaml")

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
	assert.False(t, config.IsLegacyMode())
	assert.Equal(t, "suse-observability-elasticsearch-master-headless", config.Elasticsearch.Service.Name)
	assert.Equal(t, "suse-observability-storage", config.GetStorageService().Name)
}

func TestLoadConfig_EmptyConfigMapName(t *testing.T) {
	fakeClient := fake.NewClientset()

	// Try to load with empty ConfigMap name
	config, err := LoadConfig(fakeClient, "test-ns", "", "")

	// Should fail - ConfigMap is required
	assert.Error(t, err)
	assert.Nil(t, config)
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
						Name: "es-master",
						Port: 9200,
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
				Minio: MinioConfig{
					Enabled: true,
					Service: ServiceConfig{
						Name: "minio",
						Port: 9000,
					},
					AccessKey: "minioadmin",
					SecretKey: "minioadmin",
				},
				Stackgraph: StackgraphConfig{
					Bucket:           "stackgraph-bucket",
					S3Prefix:         "",
					MultipartArchive: true,
					Restore: StackgraphRestoreConfig{
						ScaleDownLabelSelector:     "app=stackgraph",
						LoggingConfigConfigMapName: "logging-config",
						ZookeeperQuorum:            "zookeeper:2181",
						Job: JobConfig{
							Image:     "backup:latest",
							WaitImage: "wait:latest",
							Resources: ResourceRequirements{
								Limits: ResourceList{
									CPU:    "2",
									Memory: "4Gi",
								},
								Requests: ResourceList{
									CPU:    "1",
									Memory: "2Gi",
								},
							},
						},
						PVC: PVCConfig{
							Size: "10Gi",
						},
					},
				},
				VictoriaMetrics: VictoriaMetricsConfig{
					S3Locations: []S3Location{
						{
							Bucket: "vm-backup",
							Prefix: "victoria-metrics-0",
						},
						{
							Bucket: "vm-backup",
							Prefix: "victoria-metrics-1",
						},
					},
					Restore: VictoriaMetricsRestoreConfig{
						HaMode:                      "mirror",
						PersistentVolumeClaimPrefix: "database-victoria-metrics-",
						ScaleDownLabelSelector:      "app=victoria-metrics",
						Job: JobConfig{
							Image:     "vm-backup:latest",
							WaitImage: "wait:latest",
							Resources: ResourceRequirements{
								Limits: ResourceList{
									CPU:    "1",
									Memory: "2Gi",
								},
								Requests: ResourceList{
									CPU:    "500m",
									Memory: "1Gi",
								},
							},
						},
					},
				},
				Settings: SettingsConfig{
					Bucket:   "settings-backup",
					S3Prefix: "",
					Restore: SettingsRestoreConfig{
						ScaleDownLabelSelector:     "app=settings",
						LoggingConfigConfigMapName: "logging-config",
						BaseURL:                    "http://server:7070",
						ReceiverBaseURL:            "http://receiver:7077",
						PlatformVersion:            "5.2.0",
						ZookeeperQuorum:            "zookeeper:2181",
						PVC:                        "suse-observability-settings-backup-data",
						Job: JobConfig{
							Image:     "settings-backup:latest",
							WaitImage: "wait:latest",
							Resources: ResourceRequirements{
								Limits: ResourceList{
									CPU:    "1",
									Memory: "2Gi",
								},
								Requests: ResourceList{
									CPU:    "500m",
									Memory: "1Gi",
								},
							},
						},
					},
				},
				Clickhouse: ClickhouseConfig{
					Service: ServiceConfig{
						Name: "clickhouse",
						Port: 9000,
					},
					BackupService: ServiceConfig{
						Name: "clickhouse",
						Port: 7171,
					},
					Database: "default",
					Username: "default",
					Password: "password",
					Restore: ClickhouseRestoreConfig{
						ScaleDownLabelSelector: "app=clickhouse",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid config with storage",
			config: &Config{
				Elasticsearch: ElasticsearchConfig{
					Service: ServiceConfig{
						Name:                 "es-master",
						Port:                 9200,

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
				Storage: StorageConfig{
					GlobalBackupEnabled: true,
					Service: ServiceConfig{
						Name:                 "storage",
						Port:                 9000,
		
					},
					AccessKey: "storageadmin",
					SecretKey: "storageadmin",
				},
				Stackgraph: StackgraphConfig{
					Bucket:           "stackgraph-bucket",
					S3Prefix:         "",
					MultipartArchive: true,
					Restore: StackgraphRestoreConfig{
						ScaleDownLabelSelector:     "app=stackgraph",
						LoggingConfigConfigMapName: "logging-config",
						ZookeeperQuorum:            "zookeeper:2181",
						Job: JobConfig{
							Image:     "backup:latest",
							WaitImage: "wait:latest",
							Resources: ResourceRequirements{
								Limits: ResourceList{
									CPU:    "2",
									Memory: "4Gi",
								},
								Requests: ResourceList{
									CPU:    "1",
									Memory: "2Gi",
								},
							},
						},
						PVC: PVCConfig{
							Size: "10Gi",
						},
					},
				},
				VictoriaMetrics: VictoriaMetricsConfig{
					S3Locations: []S3Location{
						{
							Bucket: "vm-backup",
							Prefix: "victoria-metrics-0",
						},
						{
							Bucket: "vm-backup",
							Prefix: "victoria-metrics-1",
						},
					},
					Restore: VictoriaMetricsRestoreConfig{
						HaMode:                      "mirror",
						PersistentVolumeClaimPrefix: "database-victoria-metrics-",
						ScaleDownLabelSelector:      "app=victoria-metrics",
						Job: JobConfig{
							Image:     "vm-backup:latest",
							WaitImage: "wait:latest",
							Resources: ResourceRequirements{
								Limits: ResourceList{
									CPU:    "1",
									Memory: "2Gi",
								},
								Requests: ResourceList{
									CPU:    "500m",
									Memory: "1Gi",
								},
							},
						},
					},
				},
				Settings: SettingsConfig{
					Bucket:   "settings-backup",
					S3Prefix: "",
					Restore: SettingsRestoreConfig{
						ScaleDownLabelSelector:     "app=settings",
						LoggingConfigConfigMapName: "logging-config",
						BaseURL:                    "http://server:7070",
						ReceiverBaseURL:            "http://receiver:7077",
						PlatformVersion:            "5.2.0",
						ZookeeperQuorum:            "zookeeper:2181",
						Job: JobConfig{
							Image:     "settings-backup:latest",
							WaitImage: "wait:latest",
							Resources: ResourceRequirements{
								Limits: ResourceList{
									CPU:    "1",
									Memory: "2Gi",
								},
								Requests: ResourceList{
									CPU:    "500m",
									Memory: "1Gi",
								},
							},
						},
					},
				},
				Clickhouse: ClickhouseConfig{
					Service: ServiceConfig{
						Name:                 "clickhouse",
						Port:                 9000,
		
					},
					BackupService: ServiceConfig{
						Name:                 "clickhouse",
						Port:                 7171,

					},
					Database: "default",
					Username: "default",
					Password: "password",
					Restore: ClickhouseRestoreConfig{
						ScaleDownLabelSelector: "app=clickhouse",
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
						Name: "es-master",
						Port: 0, // Invalid
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
						Name: "es-master",
						Port: 9200,
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
