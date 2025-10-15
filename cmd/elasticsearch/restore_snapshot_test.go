package elasticsearch

import (
	"fmt"
	"testing"
	"time"

	"github.com/stackvista/stackstate-backup-cli/internal/config"
	"github.com/stackvista/stackstate-backup-cli/internal/elasticsearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockESClientForRestore is a mock for testing restore command
type mockESClientForRestore struct {
	indices          []string
	snapshot         *elasticsearch.Snapshot
	deleteErr        error
	indexExistsMap   map[string]bool
	restoreErr       error
	getSnapshotErr   error
	rolloverErr      error
	deletedIndices   []string
	restoredSnapshot string
	rolledOverDS     string
}

func (m *mockESClientForRestore) ListIndices(_ string) ([]string, error) {
	return m.indices, nil
}

func (m *mockESClientForRestore) GetSnapshot(_, _ string) (*elasticsearch.Snapshot, error) {
	if m.getSnapshotErr != nil {
		return nil, m.getSnapshotErr
	}
	return m.snapshot, nil
}

func (m *mockESClientForRestore) DeleteIndex(index string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deletedIndices = append(m.deletedIndices, index)
	// Update exists map
	if m.indexExistsMap == nil {
		m.indexExistsMap = make(map[string]bool)
	}
	m.indexExistsMap[index] = false
	return nil
}

func (m *mockESClientForRestore) IndexExists(index string) (bool, error) {
	if m.indexExistsMap == nil {
		return false, nil
	}
	exists, ok := m.indexExistsMap[index]
	if !ok {
		return false, nil
	}
	return exists, nil
}

func (m *mockESClientForRestore) RestoreSnapshot(_, snapshotName, _ string, _ bool) error {
	if m.restoreErr != nil {
		return m.restoreErr
	}
	m.restoredSnapshot = snapshotName
	return nil
}

func (m *mockESClientForRestore) RolloverDatastream(datastreamName string) error {
	if m.rolloverErr != nil {
		return m.rolloverErr
	}
	m.rolledOverDS = datastreamName
	return nil
}

func (m *mockESClientForRestore) ListSnapshots(_ string) ([]elasticsearch.Snapshot, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockESClientForRestore) ListIndicesDetailed() ([]elasticsearch.IndexInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockESClientForRestore) ConfigureSnapshotRepository(_, _, _, _, _, _ string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClientForRestore) ConfigureSLMPolicy(_, _, _, _, _, _ string, _, _ int) error {
	return fmt.Errorf("not implemented")
}

// TestRestoreCmd_Unit tests the command structure
func TestRestoreCmd_Unit(t *testing.T) {
	cliCtx := config.NewContext()
	cmd := restoreCmd(cliCtx)

	// Test command metadata
	assert.Equal(t, "restore-snapshot", cmd.Use)
	assert.Equal(t, "Restore Elasticsearch from a snapshot", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.Run)

	// Test flags
	snapshotFlag := cmd.Flags().Lookup("snapshot-name")
	require.NotNil(t, snapshotFlag)
	assert.Equal(t, "s", snapshotFlag.Shorthand)

	dropFlag := cmd.Flags().Lookup("drop-all-indices")
	require.NotNil(t, dropFlag)
	assert.Equal(t, "r", dropFlag.Shorthand)

	yesFlag := cmd.Flags().Lookup("yes")
	require.NotNil(t, yesFlag)
}

// TestFilterSTSIndices tests the index filtering logic
func TestFilterSTSIndices(t *testing.T) {
	tests := []struct {
		name             string
		allIndices       []string
		indexPrefix      string
		datastreamPrefix string
		expectedCount    int
		expectedIndices  []string
	}{
		{
			name: "filter STS indices only",
			allIndices: []string{
				"sts_topology",
				"sts_metrics",
				"sts_k8s_logs-000001",
				"other_index",
				".kibana",
			},
			indexPrefix:      "sts_",
			datastreamPrefix: "sts_k8s_logs",
			expectedCount:    3,
			expectedIndices:  []string{"sts_topology", "sts_metrics", "sts_k8s_logs-000001"},
		},
		{
			name: "no STS indices",
			allIndices: []string{
				"other_index",
				".kibana",
				"system_logs",
			},
			indexPrefix:      "sts_",
			datastreamPrefix: "sts_k8s_logs",
			expectedCount:    0,
			expectedIndices:  []string{},
		},
		{
			name:             "empty index list",
			allIndices:       []string{},
			indexPrefix:      "sts_",
			datastreamPrefix: "sts_k8s_logs",
			expectedCount:    0,
			expectedIndices:  []string{},
		},
		{
			name: "only datastream indices",
			allIndices: []string{
				"sts_k8s_logs-000001",
				"sts_k8s_logs-000002",
				"other_index",
			},
			indexPrefix:      "sts_",
			datastreamPrefix: "sts_k8s_logs",
			expectedCount:    2,
			expectedIndices:  []string{"sts_k8s_logs-000001", "sts_k8s_logs-000002"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterSTSIndices(tt.allIndices, tt.indexPrefix, tt.datastreamPrefix)
			assert.Equal(t, tt.expectedCount, len(result))

			if tt.expectedCount > 0 {
				for _, expected := range tt.expectedIndices {
					assert.Contains(t, result, expected)
				}
			}
		})
	}
}

// TestHasDatastreamIndices tests datastream detection
func TestHasDatastreamIndices(t *testing.T) {
	tests := []struct {
		name             string
		indices          []string
		datastreamPrefix string
		expected         bool
	}{
		{
			name: "has datastream indices",
			indices: []string{
				"sts_topology",
				"sts_k8s_logs-000001",
				"sts_metrics",
			},
			datastreamPrefix: "sts_k8s_logs",
			expected:         true,
		},
		{
			name: "no datastream indices",
			indices: []string{
				"sts_topology",
				"sts_metrics",
			},
			datastreamPrefix: "sts_k8s_logs",
			expected:         false,
		},
		{
			name:             "empty indices list",
			indices:          []string{},
			datastreamPrefix: "sts_k8s_logs",
			expected:         false,
		},
		{
			name: "datastream prefix without dash",
			indices: []string{
				"sts_k8s_logs",
				"sts_topology",
			},
			datastreamPrefix: "sts_k8s_logs",
			expected:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasDatastreamIndices(tt.indices, tt.datastreamPrefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMockESClientForRestore demonstrates mock usage for restore
//
//nolint:funlen
func TestMockESClientForRestore(t *testing.T) {
	tests := []struct {
		name               string
		initialIndices     []string
		indicesToDelete    []string
		deleteErr          error
		restoreErr         error
		rolloverErr        error
		expectDeletedCount int
		expectRestoreOK    bool
		expectRolloverOK   bool
	}{
		{
			name:               "successful restore with index deletion",
			initialIndices:     []string{"sts_topology", "sts_metrics"},
			indicesToDelete:    []string{"sts_topology", "sts_metrics"},
			deleteErr:          nil,
			restoreErr:         nil,
			expectDeletedCount: 2,
			expectRestoreOK:    true,
		},
		{
			name:               "restore without deletion",
			initialIndices:     []string{},
			indicesToDelete:    []string{},
			deleteErr:          nil,
			restoreErr:         nil,
			expectDeletedCount: 0,
			expectRestoreOK:    true,
		},
		{
			name:               "index deletion fails",
			initialIndices:     []string{"sts_topology"},
			indicesToDelete:    []string{"sts_topology"},
			deleteErr:          fmt.Errorf("deletion failed"),
			restoreErr:         nil,
			expectDeletedCount: 0,
			expectRestoreOK:    false,
		},
		{
			name:               "restore fails",
			initialIndices:     []string{},
			indicesToDelete:    []string{},
			deleteErr:          nil,
			restoreErr:         fmt.Errorf("restore failed"),
			expectDeletedCount: 0,
			expectRestoreOK:    false,
		},
		{
			name:               "successful rollover",
			initialIndices:     []string{"sts_k8s_logs-000001"},
			indicesToDelete:    []string{},
			rolloverErr:        nil,
			expectDeletedCount: 0,
			expectRolloverOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockESClientForRestore{
				indices:        tt.initialIndices,
				deleteErr:      tt.deleteErr,
				restoreErr:     tt.restoreErr,
				rolloverErr:    tt.rolloverErr,
				indexExistsMap: make(map[string]bool),
				snapshot: &elasticsearch.Snapshot{
					Snapshot:   "test-snapshot",
					State:      "SUCCESS",
					Indices:    []string{"sts_topology", "sts_metrics"},
					Repository: "backup-repo",
				},
			}

			// Initialize index existence
			for _, idx := range tt.initialIndices {
				mockClient.indexExistsMap[idx] = true
			}

			// Test deletion
			for _, idx := range tt.indicesToDelete {
				err := mockClient.DeleteIndex(idx)
				if tt.deleteErr != nil {
					assert.Error(t, err)
					return
				}
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectDeletedCount, len(mockClient.deletedIndices))

			// Test rollover if applicable
			if tt.expectRolloverOK {
				err := mockClient.RolloverDatastream("sts_k8s_logs")
				assert.NoError(t, err)
				assert.Equal(t, "sts_k8s_logs", mockClient.rolledOverDS)
			}

			// Test restore
			err := mockClient.RestoreSnapshot("backup-repo", "test-snapshot", "sts_*", true)
			if tt.expectRestoreOK {
				assert.NoError(t, err)
				assert.Equal(t, "test-snapshot", mockClient.restoredSnapshot)
			} else if tt.restoreErr != nil {
				assert.Error(t, err)
			}
		})
	}
}

// TestDeleteIndexWithVerification tests index deletion with verification
func TestDeleteIndexWithVerification(t *testing.T) {
	tests := []struct {
		name             string
		indexName        string
		deleteErr        error
		indexExistsAfter bool
		expectError      bool
	}{
		{
			name:             "successful deletion",
			indexName:        "sts_test",
			deleteErr:        nil,
			indexExistsAfter: false,
			expectError:      false,
		},
		{
			name:        "deletion fails",
			indexName:   "sts_test",
			deleteErr:   fmt.Errorf("deletion error"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockESClientForRestore{
				deleteErr:      tt.deleteErr,
				indexExistsMap: map[string]bool{tt.indexName: true},
			}

			// Simulate the deletion
			err := mockClient.DeleteIndex(tt.indexName)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify index was marked as deleted
				exists, _ := mockClient.IndexExists(tt.indexName)
				assert.False(t, exists)
			}
		})
	}
}

// TestRestoreSnapshot_Integration tests snapshot info retrieval
func TestRestoreSnapshot_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockClient := &mockESClientForRestore{
		snapshot: &elasticsearch.Snapshot{
			Snapshot:   "backup-2024-01-01",
			UUID:       "test-uuid",
			Repository: "backup-repo",
			State:      "SUCCESS",
			StartTime:  time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			Indices:    []string{"sts_topology", "sts_metrics", "sts_traces"},
		},
	}

	snapshot, err := mockClient.GetSnapshot("backup-repo", "backup-2024-01-01")
	require.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Equal(t, "backup-2024-01-01", snapshot.Snapshot)
	assert.Equal(t, "SUCCESS", snapshot.State)
	assert.Equal(t, 3, len(snapshot.Indices))
}

// TestRestoreConstants tests the restore command constants
func TestRestoreConstants(t *testing.T) {
	assert.Equal(t, 30, defaultMaxIndexDeleteAttempts)
	assert.Equal(t, 1*time.Second, defaultIndexDeleteRetryInterval)
}
