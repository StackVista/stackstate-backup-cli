package elasticsearch

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
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
	deletedIndices   []string
	restoredSnapshot string
	restoredTargets  string
	restoredPartial  bool
	// datastreams maps a live data stream to its backing indices.
	datastreams           map[string][]string
	datastreamDeleteCalls []string
	deleteDatastreamErr   error
	// listCalls counts ListIndices calls; addOnListCall adds indices on the given call, modelling a writer.
	listCalls     int
	addOnListCall map[int][]string
}

func (m *mockESClientForRestore) ListIndices(_ string) ([]string, error) {
	m.listCalls++
	if added := m.addOnListCall[m.listCalls]; len(added) > 0 {
		if m.indexExistsMap == nil {
			m.indexExistsMap = make(map[string]bool)
		}
		for _, index := range added {
			m.indices = append(m.indices, index)
			m.indexExistsMap[index] = true
		}
	}
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
	for _, backing := range m.datastreams {
		if len(backing) > 0 && backing[len(backing)-1] == index {
			return fmt.Errorf("index [%s] is the write index for data stream and cannot be deleted", index)
		}
	}
	m.deletedIndices = append(m.deletedIndices, index)
	m.indices = slices.DeleteFunc(m.indices, func(i string) bool { return i == index })
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

// RestoreSnapshot fails like Elasticsearch when a requested snapshot index is still open on the target.
func (m *mockESClientForRestore) RestoreSnapshot(_, snapshotName, targets string, partial bool) error {
	if m.restoreErr != nil {
		return m.restoreErr
	}
	for _, target := range strings.Split(targets, ",") {
		if m.snapshot != nil && !slices.Contains(m.snapshot.Indices, target) && !slices.Contains(m.snapshot.DataStreams, target) {
			return fmt.Errorf("index_not_found_exception: no such index [%s]", target)
		}
		if slices.Contains(m.indices, target) {
			return fmt.Errorf("cannot restore index [%s] because an open index with same name already exists", target)
		}
	}
	m.restoredSnapshot = snapshotName
	m.restoredTargets = targets
	m.restoredPartial = partial
	return nil
}

func (m *mockESClientForRestore) DeleteDatastream(datastreamName string) (bool, error) {
	m.datastreamDeleteCalls = append(m.datastreamDeleteCalls, datastreamName)
	if m.deleteDatastreamErr != nil {
		return false, m.deleteDatastreamErr
	}
	backing, ok := m.datastreams[datastreamName]
	if !ok {
		return false, nil
	}
	m.indices = slices.DeleteFunc(m.indices, func(i string) bool { return slices.Contains(backing, i) })
	if m.indexExistsMap == nil {
		m.indexExistsMap = make(map[string]bool)
	}
	for _, index := range backing {
		m.indexExistsMap[index] = false
	}
	delete(m.datastreams, datastreamName)
	return true, nil
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

func (m *mockESClientForRestore) DeleteSnapshotRepository(_ string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClientForRestore) ConfigureSLMPolicy(_, _, _, _, _, _ string, _, _ int) error {
	return fmt.Errorf("not implemented")
}

func (m *mockESClientForRestore) GetIndicesHealth() (map[string]elasticsearch.IndexHealth, error) {
	return nil, fmt.Errorf("not implemented")
}

// TestRestoreCmd_Unit tests the command structure
func TestRestoreCmd_Unit(t *testing.T) {
	flags := config.NewCLIGlobalFlags()
	flags.Namespace = testNamespace
	flags.ConfigMapName = testConfigMapName
	cmd := restoreCmd(flags)

	// Test command metadata
	assert.Equal(t, "restore", cmd.Use)
	assert.Equal(t, "Restore Elasticsearch from a snapshot", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.Run)

	// Test flags
	snapshotFlag := cmd.Flags().Lookup("snapshot")
	require.NotNil(t, snapshotFlag)
	assert.Equal(t, "s", snapshotFlag.Shorthand)

	backgroundFlag := cmd.Flags().Lookup("background")
	require.NotNil(t, backgroundFlag)

	yesFlag := cmd.Flags().Lookup("yes")
	require.NotNil(t, yesFlag)
	assert.Equal(t, "y", yesFlag.Shorthand)
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
		{
			name: "sibling datastream is not included",
			allIndices: []string{
				".ds-sts_k8s_logs-2026.08.28-012011",
				".ds-sts_k8s_logs_archive-2026.08.28-000001",
			},
			indexPrefix:      "sts",
			datastreamPrefix: ".ds-sts_k8s_logs",
			expectedCount:    1,
			expectedIndices:  []string{".ds-sts_k8s_logs-2026.08.28-012011"},
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

func TestFilterIndicesByPattern(t *testing.T) {
	indices := []string{
		"sts_topology",
		"sts_metrics",
		".ds-sts_k8s_logs-2026.08.28-012011",
	}

	tests := []struct {
		name       string
		expression string
		expected   []string
		expectErr  string
	}{
		{
			name:       "narrowed restore",
			expression: "sts_topology,.ds-sts_k8s_logs-*",
			expected: []string{
				"sts_topology",
				".ds-sts_k8s_logs-2026.08.28-012011",
			},
		},
		{
			name:       "exclusion",
			expression: "sts*,-sts_metrics",
			expected:   []string{"sts_topology"},
		},
		{
			name:       "all",
			expression: "_all",
			expected:   indices,
		},
		{
			name:       "invalid pattern",
			expression: "sts[",
			expectErr:  "invalid pattern",
		},
		{
			name:       "exclusion only",
			expression: "-sts_metrics",
			expectErr:  "at least one inclusion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := filterIndicesByPattern(indices, tt.expression)

			if tt.expectErr != "" {
				require.ErrorContains(t, err, tt.expectErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDeleteAllSTSIndicesHonorsIndicesPattern(t *testing.T) {
	client := &mockESClientForRestore{
		indices: []string{"sts_topology", "sts_metrics", "other"},
		indexExistsMap: map[string]bool{
			"sts_topology": true,
			"sts_metrics":  true,
		},
	}
	appCtx := &app.Context{
		Config: &config.Config{
			Elasticsearch: config.ElasticsearchConfig{
				Restore: config.RestoreConfig{
					IndexPrefix:           "sts",
					DatastreamIndexPrefix: ".ds-sts_k8s_logs",
					IndicesPattern:        "sts_topology",
				},
			},
		},
		Logger: logger.New(true, false),
	}

	err := deleteAllSTSIndices(client, appCtx)

	require.NoError(t, err)
	assert.Equal(t, []string{"sts_topology"}, client.deletedIndices)
}

func newLogsRestoreContext() *app.Context {
	return &app.Context{
		Config: &config.Config{
			Elasticsearch: config.ElasticsearchConfig{
				Restore: config.RestoreConfig{
					IndexPrefix:           "sts",
					DatastreamIndexPrefix: ".ds-sts_k8s_logs",
					DatastreamName:        "sts_k8s_logs",
					IndicesPattern:        "sts*,.ds-sts_k8s_logs*",
				},
			},
		},
		Logger: logger.New(true, false),
	}
}

//nolint:funlen
func TestReplaceSTSIndicesRestoresLogsDatastream(t *testing.T) {
	const (
		topo = "sts_topology_events-2026.11.01"
		b1   = ".ds-sts_k8s_logs-2026.11.01-000001"
		b2   = ".ds-sts_k8s_logs-2026.11.01-000002"
		b3   = ".ds-sts_k8s_logs-2026.11.09-000003"
		full = topo + "," + b1 + "," + b2 + ",sts_k8s_logs"
	)
	dsSnapshot := "sts_k8s_logs"

	tests := []struct {
		name                string
		pattern             string
		targetIndices       []string
		targetDatastreams   map[string][]string
		addOnListCall       map[int][]string
		deleteDatastreamErr error
		snapshotState       string
		expectDSCalls       []string
		expectDeleted       []string
		expectTargets       string
		expectRemaining     []string
		expectListCalls     int
		expectPartial       bool
		expectErr           string
	}{
		{
			name:            "T1 no STS indices",
			targetIndices:   []string{},
			expectTargets:   full,
			expectRemaining: []string{},
			expectListCalls: 2,
		},
		{
			name:            "T2 plain indices only",
			targetIndices:   []string{topo, "other"},
			expectDeleted:   []string{topo},
			expectTargets:   full,
			expectRemaining: []string{"other"},
		},
		{
			name:              "T3 live data stream write index collides",
			targetIndices:     []string{topo, b2},
			targetDatastreams: map[string][]string{dsSnapshot: {b2}},
			expectDSCalls:     []string{dsSnapshot},
			expectDeleted:     []string{topo},
			expectTargets:     full,
			expectRemaining:   []string{},
			expectListCalls:   3,
		},
		{
			name:          "T4 orphaned backing indices, no data stream",
			targetIndices: []string{topo, b1},
			expectDSCalls: []string{dsSnapshot},
			expectDeleted: []string{topo, b1},
			expectTargets: full,
		},
		{
			name:              "T5 data stream plus orphans",
			targetIndices:     []string{topo, b1, b3},
			targetDatastreams: map[string][]string{dsSnapshot: {b3}},
			expectDSCalls:     []string{dsSnapshot},
			expectDeleted:     []string{topo, b1},
			expectTargets:     full,
			expectRemaining:   []string{},
		},
		{
			name:              "T7 pattern excludes logs",
			pattern:           "sts_topology*",
			targetIndices:     []string{topo, b2},
			targetDatastreams: map[string][]string{dsSnapshot: {b2}},
			expectDeleted:     []string{topo},
			expectTargets:     topo,
			expectRemaining:   []string{b2},
		},
		{
			name:          "T8 writer recreates the data stream",
			targetIndices: []string{topo},
			addOnListCall: map[int][]string{2: {".ds-sts_k8s_logs-2026.11.09-000001"}},
			expectDeleted: []string{topo},
			expectErr:     "reappeared after deletion",
		},
		{
			name:                "T9 data stream delete fails",
			targetIndices:       []string{b1},
			deleteDatastreamErr: fmt.Errorf("boom"),
			expectDSCalls:       []string{dsSnapshot},
			expectErr:           "failed to delete datastream",
			expectRemaining:     []string{b1},
		},
		{
			name:              "T10 partial is passed through",
			targetIndices:     []string{topo, b2},
			targetDatastreams: map[string][]string{dsSnapshot: {b2}},
			snapshotState:     "PARTIAL",
			expectDSCalls:     []string{dsSnapshot},
			expectDeleted:     []string{topo},
			expectTargets:     full,
			expectRemaining:   []string{},
			expectPartial:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tt.snapshotState
			if state == "" {
				state = elasticsearch.StatusSuccess
			}
			snapshot := &elasticsearch.Snapshot{
				Snapshot:    "sts-backup-20261101",
				State:       state,
				Indices:     []string{topo, b1, b2},
				DataStreams: []string{dsSnapshot},
			}
			client := &mockESClientForRestore{
				indices:             slices.Clone(tt.targetIndices),
				snapshot:            snapshot,
				datastreams:         tt.targetDatastreams,
				addOnListCall:       tt.addOnListCall,
				deleteDatastreamErr: tt.deleteDatastreamErr,
				indexExistsMap:      map[string]bool{},
			}
			for _, index := range tt.targetIndices {
				client.indexExistsMap[index] = true
			}
			appCtx := newLogsRestoreContext()
			if tt.pattern != "" {
				appCtx.Config.Elasticsearch.Restore.IndicesPattern = tt.pattern
			}
			restoreIndices, err := restorableSnapshotIndices(snapshot, appCtx.Config.Elasticsearch.Restore)
			require.NoError(t, err)
			targets, err := restoreTargets(snapshot, restoreIndices, appCtx.Config.Elasticsearch.Restore)
			require.NoError(t, err)

			err = replaceSTSIndices(client, appCtx, "backup-repo", snapshot, targets)

			assert.Equal(t, tt.expectDSCalls, client.datastreamDeleteCalls)
			assert.Equal(t, tt.expectDeleted, client.deletedIndices)
			if tt.expectErr != "" {
				require.ErrorContains(t, err, tt.expectErr)
				assert.Empty(t, client.restoredTargets, "restore must not be sent")
				if tt.name == "T8 writer recreates the data stream" {
					assert.ErrorContains(t, err, ".ds-sts_k8s_logs-2026.11.09-000001")
				}
				if tt.expectRemaining != nil {
					assert.Equal(t, tt.expectRemaining, client.indices)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectTargets, client.restoredTargets)
			assert.Equal(t, tt.expectPartial, client.restoredPartial)
			if tt.expectRemaining != nil {
				assert.Equal(t, tt.expectRemaining, client.indices)
			}
			if tt.expectListCalls != 0 {
				assert.Equal(t, tt.expectListCalls, client.listCalls)
			}
		})
	}
}

func TestRestoreTargetsRejectsSnapshotWithoutDatastream(t *testing.T) {
	restoreConfig := newLogsRestoreContext().Config.Elasticsearch.Restore
	const topo = "sts_topology_events-2026.11.01"

	_, err := restoreTargets(&elasticsearch.Snapshot{Snapshot: "snap"},
		[]string{topo, ".ds-sts_k8s_logs-2026.11.01-000001"}, restoreConfig)
	require.ErrorContains(t, err, "not the sts_k8s_logs data stream")

	targets, err := restoreTargets(&elasticsearch.Snapshot{Snapshot: "snap"}, []string{topo}, restoreConfig)
	require.NoError(t, err)
	assert.Equal(t, []string{topo}, targets)
}

func TestRestoreTargetsKeepCompletionExpectation(t *testing.T) {
	const (
		topo = "sts_topology_events-2026.11.01"
		b1   = ".ds-sts_k8s_logs-2026.11.01-000001"
		b2   = ".ds-sts_k8s_logs-2026.11.01-000002"
	)
	appCtx := newLogsRestoreContext()
	restoreConfig := appCtx.Config.Elasticsearch.Restore
	snapshot := &elasticsearch.Snapshot{
		Snapshot:    "snap",
		Indices:     []string{topo, b1, b2},
		DataStreams: []string{"sts_k8s_logs"},
	}

	expected, err := restorableSnapshotIndices(snapshot, restoreConfig)
	require.NoError(t, err)
	targets, err := restoreTargets(snapshot, expected, restoreConfig)
	require.NoError(t, err)
	fromCheck, err := expectedRestoredIndices(&snapshotOnlyClient{snapshot: snapshot}, appCtx, "repo", "snap")
	require.NoError(t, err)

	assert.Equal(t, expected, fromCheck)
	assert.NotContains(t, expected, "sts_k8s_logs")
	assert.Equal(t, append(slices.Clone(expected), "sts_k8s_logs"), targets)

	restored := elasticsearch.IndexHealth{NumberOfShards: 1, ActivePrimaryShards: 1}
	health := map[string]elasticsearch.IndexHealth{topo: restored, b1: restored, b2: restored}
	assert.True(t, measureRestore(expected, restoreConfig.DatastreamIndexPrefix, health).complete())
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
		expectDeletedCount int
		expectRestoreOK    bool
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockESClientForRestore{
				indices:        tt.initialIndices,
				deleteErr:      tt.deleteErr,
				restoreErr:     tt.restoreErr,
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

			// Test restore
			err := mockClient.RestoreSnapshot("backup-repo", "test-snapshot", "sts_topology,sts_metrics", false)
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

// TestRestoreCmd_AllowPartialFlag tests that the allow-partial flag is registered
func TestRestoreCmd_AllowPartialFlag(t *testing.T) {
	flags := config.NewCLIGlobalFlags()
	flags.Namespace = testNamespace
	flags.ConfigMapName = testConfigMapName
	cmd := restoreCmd(flags)

	allowPartialFlag := cmd.Flags().Lookup("allow-partial")
	require.NotNil(t, allowPartialFlag)
	assert.Equal(t, "false", allowPartialFlag.DefValue)
}

// TestValidateSnapshotState tests snapshot state validation logic
func TestValidateSnapshotState(t *testing.T) {
	appCtx := &app.Context{
		Logger: logger.New(false, false),
	}

	tests := []struct {
		name         string
		state        string
		failures     []elasticsearch.SnapshotFailure
		shards       struct{ Total, Failed, Successful int }
		skipConfirm  bool
		allowPartial bool
		expectErr    bool
		errContains  string
	}{
		{
			name:  "SUCCESS state passes",
			state: "SUCCESS",
		},
		{
			name:         "PARTIAL with --allow-partial passes",
			state:        "PARTIAL",
			failures:     []elasticsearch.SnapshotFailure{{Index: "idx1", Reason: "conn refused"}},
			allowPartial: true,
		},
		{
			name:        "PARTIAL with --yes but no --allow-partial fails",
			state:       "PARTIAL",
			failures:    []elasticsearch.SnapshotFailure{{Index: "idx1", Reason: "conn refused"}},
			skipConfirm: true,
			expectErr:   true,
			errContains: "use --allow-partial together with --yes",
		},
		{
			name:        "FAILED state is rejected",
			state:       "FAILED",
			expectErr:   true,
			errContains: "FAILED state and cannot be restored",
		},
		{
			name:        "IN_PROGRESS state is rejected",
			state:       "IN_PROGRESS",
			expectErr:   true,
			errContains: "IN_PROGRESS state and cannot be restored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := &elasticsearch.Snapshot{
				Snapshot: "test-snapshot",
				State:    tt.state,
				Failures: tt.failures,
				Shards: struct {
					Total      int `json:"total"`
					Failed     int `json:"failed"`
					Successful int `json:"successful"`
				}{
					Total:      tt.shards.Total,
					Failed:     tt.shards.Failed,
					Successful: tt.shards.Successful,
				},
			}

			err := validateSnapshotState(snapshot, appCtx, tt.skipConfirm, tt.allowPartial)
			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRestoreConstants tests the restore command constants
func TestRestoreConstants(t *testing.T) {
	assert.Equal(t, 30, defaultMaxIndexDeleteAttempts)
	assert.Equal(t, 1*time.Second, defaultIndexDeleteRetryInterval)
}
