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

// Note: Constants testConfigMapName and testNamespace are shared from list_snapshots_test.go

// mockESClientForIndices is a mock for testing list-indices command
type mockESClientForIndices struct {
	indices       []string
	indicesDetail []elasticsearch.IndexInfo
	err           error
}

func (m *mockESClientForIndices) ListSnapshots(_ string) ([]elasticsearch.Snapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockESClientForIndices) GetSnapshot(_, _ string) (*elasticsearch.Snapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockESClientForIndices) ListIndices(_ string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.indices, nil
}

func (m *mockESClientForIndices) ListIndicesDetailed() ([]elasticsearch.IndexInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.indicesDetail, nil
}

func (m *mockESClientForIndices) DeleteIndex(_ string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClientForIndices) IndexExists(_ string) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

func (m *mockESClientForIndices) RestoreSnapshot(_, _, _ string, _ bool) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClientForIndices) ConfigureSnapshotRepository(_, _, _, _, _, _ string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClientForIndices) ConfigureSLMPolicy(_, _, _, _, _, _ string, _, _ int) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClientForIndices) RolloverDatastream(_ string) error {
	return fmt.Errorf("not implemented")
}

// TestListIndicesCmd_Unit tests the command structure
func TestListIndicesCmd_Unit(t *testing.T) {
	cliCtx := config.NewContext()
	cliCtx.Config.Namespace = testNamespace
	cliCtx.Config.ConfigMapName = testConfigMapName
	cliCtx.Config.OutputFormat = "table"

	cmd := listIndicesCmd(cliCtx)

	// Test command metadata
	assert.Equal(t, "list-indices", cmd.Use)
	assert.Equal(t, "List Elasticsearch indices", cmd.Short)
	assert.NotNil(t, cmd.Run)
}

// TestListIndicesCmd_Integration tests the integration with Kubernetes client
func TestListIndicesCmd_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	// Create ConfigMap with valid config
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMapName,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"config": `
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
`,
		},
	}
	_, err := fakeClient.CoreV1().ConfigMaps(testNamespace).Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Test that config loading works
	cfg, err := config.LoadConfig(fakeClient, testNamespace, testConfigMapName, "")
	require.NoError(t, err)
	assert.Equal(t, "elasticsearch-master", cfg.Elasticsearch.Service.Name)
	assert.Equal(t, 9200, cfg.Elasticsearch.Service.Port)
}

// TestMockESClientForIndices demonstrates mock usage for indices
func TestMockESClientForIndices(t *testing.T) {
	tests := []struct {
		name          string
		mockIndices   []elasticsearch.IndexInfo
		mockErr       error
		expectError   bool
		expectedCount int
	}{
		{
			name: "successful list with multiple indices",
			mockIndices: []elasticsearch.IndexInfo{
				{
					Health:       "green",
					Status:       "open",
					Index:        "sts_logs-2024-01",
					UUID:         "uuid1",
					Pri:          "1",
					Rep:          "1",
					DocsCount:    "1000",
					DocsDeleted:  "0",
					StoreSize:    "1mb",
					PriStoreSize: "500kb",
					DatasetSize:  "1mb",
				},
				{
					Health:       "yellow",
					Status:       "open",
					Index:        "sts_logs-2024-02",
					UUID:         "uuid2",
					Pri:          "1",
					Rep:          "1",
					DocsCount:    "2000",
					DocsDeleted:  "10",
					StoreSize:    "2mb",
					PriStoreSize: "1mb",
					DatasetSize:  "2mb",
				},
			},
			mockErr:       nil,
			expectError:   false,
			expectedCount: 2,
		},
		{
			name:          "empty indices list",
			mockIndices:   []elasticsearch.IndexInfo{},
			mockErr:       nil,
			expectError:   false,
			expectedCount: 0,
		},
		{
			name:          "error case",
			mockIndices:   nil,
			mockErr:       fmt.Errorf("failed to connect to elasticsearch"),
			expectError:   true,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockClient := &mockESClientForIndices{
				indicesDetail: tt.mockIndices,
				err:           tt.mockErr,
			}

			// Call the method
			indices, err := mockClient.ListIndicesDetailed()

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, indices)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedCount, len(indices))
				for i, expected := range tt.mockIndices {
					assert.Equal(t, expected.Index, indices[i].Index)
					assert.Equal(t, expected.Health, indices[i].Health)
					assert.Equal(t, expected.Status, indices[i].Status)
				}
			}
		})
	}
}

// TestMockESClientSimpleList tests the simple ListIndices method
func TestMockESClientSimpleList(t *testing.T) {
	tests := []struct {
		name        string
		mockIndices []string
		mockErr     error
		expectError bool
	}{
		{
			name:        "successful simple list",
			mockIndices: []string{"index-1", "index-2", "index-3"},
			mockErr:     nil,
			expectError: false,
		},
		{
			name:        "error case",
			mockIndices: nil,
			mockErr:     fmt.Errorf("connection timeout"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockESClientForIndices{
				indices: tt.mockIndices,
				err:     tt.mockErr,
			}

			indices, err := mockClient.ListIndices("*")

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.mockIndices, indices)
			}
		})
	}
}
