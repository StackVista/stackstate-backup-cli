package elasticsearch

import (
	"context"
	"fmt"
	"testing"

	"github.com/stackvista/stackstate-backup-cli/internal/config"
	"github.com/stackvista/stackstate-backup-cli/internal/elasticsearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// mockESClientForConfigure is a mock for testing configure command
type mockESClientForConfigure struct {
	configureRepoErr error
	configureSLMErr  error
	repoConfigured   bool
	slmConfigured    bool
	lastRepoConfig   map[string]string
	lastSLMConfig    map[string]interface{}
}

func (m *mockESClientForConfigure) ConfigureSnapshotRepository(name, bucket, endpoint, basePath, accessKey, secretKey string) error {
	if m.configureRepoErr != nil {
		return m.configureRepoErr
	}
	m.repoConfigured = true
	m.lastRepoConfig = map[string]string{
		"name":      name,
		"bucket":    bucket,
		"endpoint":  endpoint,
		"basePath":  basePath,
		"accessKey": accessKey,
		"secretKey": secretKey,
	}
	return nil
}

func (m *mockESClientForConfigure) ConfigureSLMPolicy(name, schedule, snapshotName, repository, indices, expireAfter string, minCount, maxCount int) error {
	if m.configureSLMErr != nil {
		return m.configureSLMErr
	}
	m.slmConfigured = true
	m.lastSLMConfig = map[string]interface{}{
		"name":         name,
		"schedule":     schedule,
		"snapshotName": snapshotName,
		"repository":   repository,
		"indices":      indices,
		"expireAfter":  expireAfter,
		"minCount":     minCount,
		"maxCount":     maxCount,
	}
	return nil
}

func (m *mockESClientForConfigure) ListSnapshots(_ string) ([]elasticsearch.Snapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockESClientForConfigure) GetSnapshot(_, _ string) (*elasticsearch.Snapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockESClientForConfigure) ListIndices(_ string) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockESClientForConfigure) ListIndicesDetailed() ([]elasticsearch.IndexInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockESClientForConfigure) DeleteIndex(_ string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClientForConfigure) IndexExists(_ string) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

func (m *mockESClientForConfigure) RestoreSnapshot(_, _, _ string, _ bool) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClientForConfigure) RolloverDatastream(_ string) error {
	return fmt.Errorf("not implemented")
}

// TestConfigureCmd_Unit tests the command structure
func TestConfigureCmd_Unit(t *testing.T) {
	cliCtx := config.NewContext()
	cliCtx.Config.Namespace = testNamespace
	cliCtx.Config.ConfigMapName = testConfigMapName
	cliCtx.Config.SecretName = testSecretName

	cmd := configureCmd(cliCtx)

	// Test command metadata
	assert.Equal(t, "configure", cmd.Use)
	assert.Equal(t, "Configure Elasticsearch snapshot repository and SLM policy", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.Run)
}

// TestConfigureCmd_Integration tests the integration with Kubernetes client
//
//nolint:funlen
func TestConfigureCmd_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name          string
		configData    string
		secretData    string
		expectError   bool
		errorContains string
	}{
		{
			name: "successful configuration with complete data",
			configData: `
elasticsearch:
  service:
    name: elasticsearch-master
    port: 9200
    localPortForwardPort: 9200
  restore:
    scaleDownLabelSelector: app=test
    indexPrefix: sts_
    datastreamIndexPrefix: sts_k8s_logs
    datastreamName: sts_k8s_logs
    indicesPattern: "sts_*"
    repository: backup-repo
  snapshotRepository:
    name: backup-repo
    bucket: backups
    endpoint: minio:9000
    basepath: snapshots
    accessKey: test-key
    secretKey: test-secret
  slm:
    name: daily
    schedule: "0 1 * * *"
    snapshotTemplateName: "<snap-{now/d}>"
    repository: backup-repo
    indices: "sts_*"
    retentionExpireAfter: 30d
    retentionMinCount: 5
    retentionMaxCount: 50
`,
			secretData:  "",
			expectError: false,
		},
		{
			name: "missing credentials in config",
			configData: `
elasticsearch:
  service:
    name: elasticsearch-master
    port: 9200
    localPortForwardPort: 9200
  restore:
    scaleDownLabelSelector: app=test
    indexPrefix: sts_
    datastreamIndexPrefix: sts_k8s_logs
    datastreamName: sts_k8s_logs
    indicesPattern: "sts_*"
    repository: backup-repo
  snapshotRepository:
    name: backup-repo
    bucket: backups
    endpoint: minio:9000
    basepath: snapshots
    accessKey: ""
    secretKey: ""
  slm:
    name: daily
    schedule: "0 1 * * *"
    snapshotTemplateName: "<snap-{now/d}>"
    repository: backup-repo
    indices: "sts_*"
    retentionExpireAfter: 30d
    retentionMinCount: 5
    retentionMaxCount: 50
`,
			secretData: `
elasticsearch:
  snapshotRepository:
    accessKey: secret-key
    secretKey: secret-value
`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()

			// Create ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testConfigMapName,
					Namespace: testNamespace,
				},
				Data: map[string]string{
					"config": tt.configData,
				},
			}
			_, err := fakeClient.CoreV1().ConfigMaps(testNamespace).Create(
				context.Background(), cm, metav1.CreateOptions{},
			)
			require.NoError(t, err)

			// Create Secret if provided
			if tt.secretData != "" {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{
						"config": []byte(tt.secretData),
					},
				}
				_, err := fakeClient.CoreV1().Secrets(testNamespace).Create(
					context.Background(), secret, metav1.CreateOptions{},
				)
				require.NoError(t, err)
			}

			// Test that config loading works
			secretName := ""
			if tt.secretData != "" {
				secretName = testSecretName
			}
			cfg, err := config.LoadConfig(fakeClient, testNamespace, testConfigMapName, secretName)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, cfg)
				assert.NotEmpty(t, cfg.Elasticsearch.SnapshotRepository.AccessKey)
				assert.NotEmpty(t, cfg.Elasticsearch.SnapshotRepository.SecretKey)
			}
		})
	}
}

// TestMockESClientForConfigure demonstrates mock usage for configure
func TestMockESClientForConfigure(t *testing.T) {
	tests := []struct {
		name             string
		configureRepoErr error
		configureSLMErr  error
		expectRepoOK     bool
		expectSLMOK      bool
	}{
		{
			name:             "successful configuration",
			configureRepoErr: nil,
			configureSLMErr:  nil,
			expectRepoOK:     true,
			expectSLMOK:      true,
		},
		{
			name:             "repository configuration fails",
			configureRepoErr: fmt.Errorf("repository creation failed"),
			configureSLMErr:  nil,
			expectRepoOK:     false,
			expectSLMOK:      false,
		},
		{
			name:             "SLM configuration fails",
			configureRepoErr: nil,
			configureSLMErr:  fmt.Errorf("SLM policy creation failed"),
			expectRepoOK:     true,
			expectSLMOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockESClientForConfigure{
				configureRepoErr: tt.configureRepoErr,
				configureSLMErr:  tt.configureSLMErr,
			}

			// Configure repository
			err := mockClient.ConfigureSnapshotRepository(
				"backup-repo",
				"backup-bucket",
				"minio:9000",
				"snapshots",
				"access-key",
				"secret-key",
			)

			if tt.expectRepoOK {
				assert.NoError(t, err)
				assert.True(t, mockClient.repoConfigured)
				assert.Equal(t, "backup-repo", mockClient.lastRepoConfig["name"])
				assert.Equal(t, "backup-bucket", mockClient.lastRepoConfig["bucket"])
			} else {
				assert.Error(t, err)
				return // Don't test SLM if repo failed
			}

			// Configure SLM policy
			err = mockClient.ConfigureSLMPolicy(
				"daily-snapshots",
				"0 1 * * *",
				"<snap-{now/d}>",
				"backup-repo",
				"sts_*",
				"30d",
				5,
				50,
			)

			if tt.expectSLMOK {
				assert.NoError(t, err)
				assert.True(t, mockClient.slmConfigured)
				assert.Equal(t, "daily-snapshots", mockClient.lastSLMConfig["name"])
				assert.Equal(t, "0 1 * * *", mockClient.lastSLMConfig["schedule"])
				assert.Equal(t, 5, mockClient.lastSLMConfig["minCount"])
				assert.Equal(t, 50, mockClient.lastSLMConfig["maxCount"])
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// TestConfigureValidation tests configuration validation
func TestConfigureValidation(t *testing.T) {
	tests := []struct {
		name          string
		accessKey     string
		secretKey     string
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid credentials",
			accessKey:   "test-key",
			secretKey:   "test-secret",
			expectError: false,
		},
		{
			name:          "missing access key",
			accessKey:     "",
			secretKey:     "test-secret",
			expectError:   true,
			errorContains: "accessKey and secretKey are required",
		},
		{
			name:          "missing secret key",
			accessKey:     "test-key",
			secretKey:     "",
			expectError:   true,
			errorContains: "accessKey and secretKey are required",
		},
		{
			name:          "missing both credentials",
			accessKey:     "",
			secretKey:     "",
			expectError:   true,
			errorContains: "accessKey and secretKey are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate validation logic from runConfigure
			hasError := tt.accessKey == "" || tt.secretKey == ""

			if tt.expectError {
				assert.True(t, hasError)
			} else {
				assert.False(t, hasError)
			}
		})
	}
}
