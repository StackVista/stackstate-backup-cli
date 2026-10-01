package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLoadConfig_YAMLCompatibility(t *testing.T) {
	client := configClientWithSecret(t, loadTestData(t, "yamlCompatibilitySecret.yaml"))

	cfg, err := LoadConfig(client, "test-ns", "backup-config", "backup-secret")

	require.NoError(t, err)
	assert.Equal(t, "12345", cfg.Elasticsearch.SnapshotRepository.AccessKey)
	assert.Equal(t, "first line\nsecond line", cfg.Elasticsearch.SnapshotRepository.SecretKey)
	assert.Equal(t, "sts-backup", cfg.Elasticsearch.SnapshotRepository.Name)
	assert.Equal(t, "suse-observability-elasticsearch-master-headless", cfg.Elasticsearch.Service.Name)
	assert.Equal(t, 9200, cfg.Elasticsearch.Service.Port)
	assert.True(t, cfg.Storage.GlobalBackupEnabled)
	assert.Equal(t, "on", cfg.Storage.AccessKey)
	assert.Equal(t, "yes", cfg.Storage.SecretKey)
	assert.Equal(t, map[string]string{"enabled": "true", "legacy": "yes", "numeric": "42"}, cfg.Kubernetes.CommonLabels)
}

func TestLoadConfig_SecretYAMLErrors(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		error      string
	}{
		{
			name:       "duplicate keys",
			configYAML: "storage:\n  accessKey: first\n  accessKey: second\n",
			error:      "failed to parse Secret config",
		},
		{
			name:       "invalid integer",
			configYAML: "elasticsearch:\n  service:\n    port: invalid\n",
			error:      "failed to parse Secret config",
		},
		{
			name:       "invalid service port",
			configYAML: "elasticsearch:\n  service:\n    port: 65536\n",
			error:      "configuration validation failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := configClientWithSecret(t, tt.configYAML)

			cfg, err := LoadConfig(client, "test-ns", "backup-config", "backup-secret")

			require.ErrorContains(t, err, tt.error)
			assert.Nil(t, cfg)
		})
	}
}

func configClientWithSecret(t *testing.T, secret string) *fake.Clientset {
	t.Helper()
	return fake.NewClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-config", Namespace: "test-ns"},
			Data:       map[string]string{"config": loadTestData(t, "validStorageConfigMapOnly.yaml")},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "backup-secret", Namespace: "test-ns"},
			Data:       map[string][]byte{"config": []byte(secret)},
		},
	)
}
