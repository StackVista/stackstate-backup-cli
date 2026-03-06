package elasticsearch

import (
	"fmt"
	"testing"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListIndicesCmd_Unit tests the command structure
func TestListIndicesCmd_Unit(t *testing.T) {
	flags := config.NewCLIGlobalFlags()
	flags.Namespace = testNamespace
	flags.ConfigMapName = testConfigMapName
	flags.OutputFormat = "table"

	cmd := listIndicesCmd(flags)

	assert.Equal(t, "list-indices", cmd.Use)
	assert.Equal(t, "List Elasticsearch indices", cmd.Short)
	assert.NotNil(t, cmd.Run)
}

// TestListIndicesCmd_Integration tests the integration with Kubernetes client
func TestListIndicesCmd_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fakeClient := createFakeClientWithConfig(t, minimalMinioStackgraphConfig)
	cfg, err := config.LoadConfig(fakeClient, testNamespace, testConfigMapName, "")
	require.NoError(t, err)
	assert.Equal(t, "elasticsearch-master", cfg.Elasticsearch.Service.Name)
	assert.Equal(t, 9200, cfg.Elasticsearch.Service.Port)
}

// TestListIndicesCmd_StorageIntegration tests the integration with Kubernetes client using StorageConfig
func TestListIndicesCmd_StorageIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fakeClient := createFakeClientWithConfig(t, minimalStorageStackgraphConfig)
	cfg, err := config.LoadConfig(fakeClient, testNamespace, testConfigMapName, "")
	require.NoError(t, err)
	assert.Equal(t, "elasticsearch-master", cfg.Elasticsearch.Service.Name)
	assert.Equal(t, 9200, cfg.Elasticsearch.Service.Port)
	assert.False(t, cfg.IsLegacyMode())
	assert.True(t, cfg.StorageEnabled())
	assert.Equal(t, "storage", cfg.GetStorageService().Name)
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
			mockClient := &mockESClient{
				indicesDetail: tt.mockIndices,
				err:           tt.mockErr,
			}

			indices, err := mockClient.ListIndicesDetailed()

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
			mockClient := &mockESClient{
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
