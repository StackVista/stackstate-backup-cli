package elasticsearch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testConfigMapName = "backup-config"
	testNamespace     = "test-ns"
	testSecretName    = "backup-secret"
)

// minimalESConfig provides the common Elasticsearch configuration for tests
const minimalESConfig = `
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
    accessKey: key
    secretKey: secret
  slm:
    name: daily
    schedule: "0 1 * * *"
    snapshotTemplateName: "<snap-{now/d}>"
    repository: backup-repo
    indices: "sts_*"
    retentionExpireAfter: 30d
    retentionMinCount: 5
    retentionMaxCount: 50
`

// minimalMinioStackgraphConfig provides the required Minio and Stackgraph configuration for tests
const minimalMinioStackgraphConfig = `
minio:
  enabled: true
  service:
    name: minio
    port: 9000
  accessKey: minioadmin
  secretKey: minioadmin
stackgraph:
  bucket: stackgraph-bucket
  multipartArchive: true
  restore:
    scaleDownLabelSelector: "app=stackgraph"
    loggingConfigConfigMap: logging-config
    zookeeperQuorum: "zookeeper:2181"
    job:
      image: backup:latest
      waitImage: wait:latest
      resources:
        limits:
          cpu: "2"
          memory: "4Gi"
        requests:
          cpu: "1"
          memory: "2Gi"
    pvc:
      size: "10Gi"
victoriaMetrics:
  S3Locations:
    - bucket: vm-backup
      prefix: victoria-metrics-0
    - bucket: vm-backup
      prefix: victoria-metrics-1
  restore:
    haMode: "mirror"
    persistentVolumeClaimPrefix: "database-victoria-metrics-"
    scaleDownLabelSelector: "app=victoria-metrics"
    job:
      image: vm-backup:latest
      waitImage: wait:latest
      resources:
        limits:
          cpu: "1"
          memory: "2Gi"
        requests:
          cpu: "500m"
          memory: "1Gi"
settings:
  bucket: sts-settings-backup
  s3Prefix: ""
  restore:
    scaleDownLabelSelector: "app=settings"
    loggingConfigConfigMap: logging-config
    baseUrl: "http://server:7070"
    receiverBaseUrl: "http://receiver:7077"
    platformVersion: "5.2.0"
    zookeeperQuorum: "zookeeper:2181"
    pvc: "suse-observability-settings-backup-data"
    job:
      image: settings-backup:latest
      waitImage: wait:latest
      resources:
        limits:
          cpu: "1"
          memory: "2Gi"
        requests:
          cpu: "500m"
          memory: "1Gi"
clickhouse:
  service:
    name: "clickhouse"
    port: 9000
  backupService:
    name: "clickhouse"
    port: 7171
  database: "default"
  username: "default"
  password: "password"
  restore:
    scaleDownLabelSelector: "app=clickhouse"
`

// minimalStorageStackgraphConfig provides the required Storage and Stackgraph configuration for tests (new mode)
const minimalStorageStackgraphConfig = `
storage:
  globalBackupEnabled: true
  service:
    name: storage
    port: 9000
    localPortForwardPort: 9000
  accessKey: storageadmin
  secretKey: storageadmin
stackgraph:
  bucket: stackgraph-bucket
  multipartArchive: true
  restore:
    scaleDownLabelSelector: "app=stackgraph"
    loggingConfigConfigMap: logging-config
    zookeeperQuorum: "zookeeper:2181"
    job:
      image: backup:latest
      waitImage: wait:latest
      resources:
        limits:
          cpu: "2"
          memory: "4Gi"
        requests:
          cpu: "1"
          memory: "2Gi"
    pvc:
      size: "10Gi"
victoriaMetrics:
  S3Locations:
    - bucket: vm-backup
      prefix: victoria-metrics-0
    - bucket: vm-backup
      prefix: victoria-metrics-1
  restore:
    haMode: "mirror"
    persistentVolumeClaimPrefix: "database-victoria-metrics-"
    scaleDownLabelSelector: "app=victoria-metrics"
    job:
      image: vm-backup:latest
      waitImage: wait:latest
      resources:
        limits:
          cpu: "1"
          memory: "2Gi"
        requests:
          cpu: "500m"
          memory: "1Gi"
settings:
  bucket: sts-settings-backup
  s3Prefix: ""
  localBucket: sts-settings-local-backup
  restore:
    scaleDownLabelSelector: "app=settings"
    loggingConfigConfigMap: logging-config
    baseUrl: "http://server:7070"
    receiverBaseUrl: "http://receiver:7077"
    platformVersion: "5.2.0"
    zookeeperQuorum: "zookeeper:2181"
    job:
      image: settings-backup:latest
      waitImage: wait:latest
      resources:
        limits:
          cpu: "1"
          memory: "2Gi"
        requests:
          cpu: "500m"
          memory: "1Gi"
clickhouse:
  service:
    name: "clickhouse"
    port: 9000
    localPortForwardPort: 9000
  backupService:
    name: "clickhouse"
    port: 7171
    localPortForwardPort: 7171
  database: "default"
  username: "default"
  password: "password"
  restore:
    scaleDownLabelSelector: "app=clickhouse"
`

// mockESClient is a mock for testing Elasticsearch commands
type mockESClient struct {
	snapshots     []elasticsearch.Snapshot
	indices       []string
	indicesDetail []elasticsearch.IndexInfo
	err           error
}

func (m *mockESClient) ListSnapshots(_ string) ([]elasticsearch.Snapshot, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.snapshots, nil
}

func (m *mockESClient) GetSnapshot(_, _ string) (*elasticsearch.Snapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockESClient) ListIndices(_ string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.indices, nil
}

func (m *mockESClient) ListIndicesDetailed() ([]elasticsearch.IndexInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.indicesDetail, nil
}

func (m *mockESClient) DeleteIndex(_ string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClient) IndexExists(_ string) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

func (m *mockESClient) RestoreSnapshot(_, _, _ string, _ bool) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClient) ConfigureSnapshotRepository(_, _, _, _, _, _ string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClient) ConfigureSLMPolicy(_, _, _, _, _, _ string, _, _ int) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClient) RolloverDatastream(_ string) error {
	return fmt.Errorf("not implemented")
}

// createFakeClientWithConfig creates a fake Kubernetes client with a ConfigMap containing the given config
func createFakeClientWithConfig(t *testing.T, storageConfig string) *fake.Clientset {
	t.Helper()
	fakeClient := fake.NewClientset()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMapName,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"config": minimalESConfig + storageConfig,
		},
	}
	_, err := fakeClient.CoreV1().ConfigMaps(testNamespace).Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)
	return fakeClient
}

// TestListCmd_Integration demonstrates an integration-style test
func TestListCmd_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fakeClient := createFakeClientWithConfig(t, minimalMinioStackgraphConfig)
	cfg, err := config.LoadConfig(fakeClient, testNamespace, testConfigMapName, "")
	require.NoError(t, err)
	assert.Equal(t, "backup-repo", cfg.Elasticsearch.Restore.Repository)
	assert.Equal(t, "elasticsearch-master", cfg.Elasticsearch.Service.Name)
}

// TestListCmd_StorageIntegration tests the full command flow with new StorageConfig
func TestListCmd_StorageIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fakeClient := createFakeClientWithConfig(t, minimalStorageStackgraphConfig)
	cfg, err := config.LoadConfig(fakeClient, testNamespace, testConfigMapName, "")
	require.NoError(t, err)
	assert.Equal(t, "backup-repo", cfg.Elasticsearch.Restore.Repository)
	assert.Equal(t, "elasticsearch-master", cfg.Elasticsearch.Service.Name)
	assert.False(t, cfg.IsLegacyMode())
	assert.True(t, cfg.GlobalBackupEnabled())
	assert.Equal(t, "storage", cfg.GetStorageService().Name)
}

// TestListCmd_Unit demonstrates a unit-style test
func TestListCmd_Unit(t *testing.T) {
	flags := config.NewCLIGlobalFlags()
	flags.Namespace = testNamespace
	flags.ConfigMapName = testConfigMapName
	flags.OutputFormat = "table"

	cmd := listCmd(flags)

	assert.Equal(t, "list", cmd.Use)
	assert.Equal(t, "List available Elasticsearch snapshots", cmd.Short)
	assert.NotNil(t, cmd.Run)
}

// TestMockESClient demonstrates how to use the mock client
func TestMockESClient(t *testing.T) {
	tests := []struct {
		name          string
		mockSnapshots []elasticsearch.Snapshot
		mockErr       error
		expectError   bool
	}{
		{
			name: "successful list",
			mockSnapshots: []elasticsearch.Snapshot{
				{
					Snapshot:         "snapshot-1",
					UUID:             "uuid-1",
					State:            "SUCCESS",
					StartTime:        time.Now().Format(time.RFC3339),
					DurationInMillis: 1000,
				},
				{
					Snapshot:         "snapshot-2",
					UUID:             "uuid-2",
					State:            "SUCCESS",
					StartTime:        time.Now().Format(time.RFC3339),
					DurationInMillis: 2000,
				},
			},
			mockErr:     nil,
			expectError: false,
		},
		{
			name:          "error case",
			mockSnapshots: nil,
			mockErr:       fmt.Errorf("connection failed"),
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockESClient{
				snapshots: tt.mockSnapshots,
				err:       tt.mockErr,
			}

			snapshots, err := mockClient.ListSnapshots("backup-repo")

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, snapshots)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.mockSnapshots), len(snapshots))
				for i, expected := range tt.mockSnapshots {
					assert.Equal(t, expected.Snapshot, snapshots[i].Snapshot)
					assert.Equal(t, expected.State, snapshots[i].State)
				}
			}
		})
	}
}
