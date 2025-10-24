package elasticsearch

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockESServer creates a test HTTP server with Elasticsearch headers
func mockESServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add Elasticsearch headers for client validation
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		handler(w, r)
	}))
}

func TestClient_ListSnapshots(t *testing.T) {
	tests := []struct {
		name           string
		repository     string
		responseBody   string
		responseStatus int
		expectedCount  int
		expectError    bool
	}{
		{
			name:           "successful list with multiple snapshots",
			repository:     "test-repo",
			responseStatus: http.StatusOK,
			responseBody: `{
				"snapshots": [
					{
						"snapshot": "snapshot-2024-01-01",
						"uuid": "uuid-1",
						"repository": "test-repo",
						"state": "SUCCESS"
					},
					{
						"snapshot": "snapshot-2024-01-02",
						"uuid": "uuid-2",
						"repository": "test-repo",
						"state": "SUCCESS"
					}
				],
				"total": 2,
				"remaining": 0
			}`,
			expectedCount: 2,
			expectError:   false,
		},
		{
			name:           "empty snapshot list",
			repository:     "empty-repo",
			responseStatus: http.StatusOK,
			responseBody: `{
				"snapshots": [],
				"total": 0,
				"remaining": 0
			}`,
			expectedCount: 0,
			expectError:   false,
		},
		{
			name:           "elasticsearch returns error",
			repository:     "bad-repo",
			responseStatus: http.StatusNotFound,
			responseBody:   `{"error": "repository not found"}`,
			expectedCount:  0,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := mockESServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request path
				expectedPath := "/_snapshot/" + tt.repository + "/_all"
				assert.Equal(t, expectedPath, r.URL.Path)
				assert.Equal(t, http.MethodGet, r.Method)

				w.WriteHeader(tt.responseStatus)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Create client
			client, err := NewClient(server.URL)
			require.NoError(t, err)

			// Execute test
			snapshots, err := client.ListSnapshots(tt.repository)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, len(snapshots))

			// Verify snapshot details if any
			if tt.expectedCount > 0 {
				assert.Equal(t, "snapshot-2024-01-01", snapshots[0].Snapshot)
				assert.Equal(t, tt.repository, snapshots[0].Repository)
			}
		})
	}
}

func TestClient_GetSnapshot(t *testing.T) {
	tests := []struct {
		name           string
		repository     string
		snapshotName   string
		responseBody   string
		responseStatus int
		expectError    bool
	}{
		{
			name:           "successful get snapshot",
			repository:     "test-repo",
			snapshotName:   "snapshot-2024-01-01",
			responseStatus: http.StatusOK,
			responseBody: `{
				"snapshots": [
					{
						"snapshot": "snapshot-2024-01-01",
						"uuid": "uuid-1",
						"repository": "test-repo",
						"state": "SUCCESS",
						"indices": ["index-1", "index-2"]
					}
				]
			}`,
			expectError: false,
		},
		{
			name:           "snapshot not found",
			repository:     "test-repo",
			snapshotName:   "nonexistent",
			responseStatus: http.StatusOK,
			responseBody: `{
				"snapshots": []
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := mockESServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/_snapshot/" + tt.repository + "/" + tt.snapshotName
				assert.Equal(t, expectedPath, r.URL.Path)

				w.WriteHeader(tt.responseStatus)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Create client
			client, err := NewClient(server.URL)
			require.NoError(t, err)

			// Execute test
			snapshot, err := client.GetSnapshot(tt.repository, tt.snapshotName)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, snapshot)
			assert.Equal(t, tt.snapshotName, snapshot.Snapshot)
			assert.Equal(t, tt.repository, snapshot.Repository)
		})
	}
}

func TestClient_ListIndices(t *testing.T) {
	tests := []struct {
		name           string
		pattern        string
		responseBody   string
		responseStatus int
		expectedCount  int
		expectError    bool
	}{
		{
			name:           "list all indices",
			pattern:        "*",
			responseStatus: http.StatusOK,
			responseBody: `[
				{"index": "index-1"},
				{"index": "index-2"},
				{"index": "index-3"}
			]`,
			expectedCount: 3,
			expectError:   false,
		},
		{
			name:           "list specific pattern",
			pattern:        "logs-*",
			responseStatus: http.StatusOK,
			responseBody: `[
				{"index": "logs-2024-01"},
				{"index": "logs-2024-02"}
			]`,
			expectedCount: 2,
			expectError:   false,
		},
		{
			name:           "no indices found",
			pattern:        "nonexistent-*",
			responseStatus: http.StatusOK,
			responseBody:   `[]`,
			expectedCount:  0,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := mockESServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/_cat/indices/"+tt.pattern, r.URL.Path)
				assert.Equal(t, "json", r.URL.Query().Get("format"))
				assert.Equal(t, "index", r.URL.Query().Get("h"))

				w.WriteHeader(tt.responseStatus)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Create client
			client, err := NewClient(server.URL)
			require.NoError(t, err)

			// Execute test
			indices, err := client.ListIndices(tt.pattern)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, len(indices))
		})
	}
}

func TestClient_DeleteIndex(t *testing.T) {
	tests := []struct {
		name           string
		index          string
		responseStatus int
		expectError    bool
	}{
		{
			name:           "successful delete",
			index:          "test-index",
			responseStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "index not found",
			index:          "nonexistent",
			responseStatus: http.StatusNotFound,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := mockESServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/"+tt.index, r.URL.Path)
				assert.Equal(t, http.MethodDelete, r.Method)

				w.WriteHeader(tt.responseStatus)
			}))
			defer server.Close()

			// Create client
			client, err := NewClient(server.URL)
			require.NoError(t, err)

			// Execute test
			err = client.DeleteIndex(tt.index)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClient_IndexExists(t *testing.T) {
	tests := []struct {
		name           string
		index          string
		responseStatus int
		expectedExists bool
	}{
		{
			name:           "index exists",
			index:          "existing-index",
			responseStatus: http.StatusOK,
			expectedExists: true,
		},
		{
			name:           "index does not exist",
			index:          "nonexistent-index",
			responseStatus: http.StatusNotFound,
			expectedExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := mockESServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/"+tt.index, r.URL.Path)
				assert.Equal(t, http.MethodHead, r.Method)

				w.WriteHeader(tt.responseStatus)
			}))
			defer server.Close()

			// Create client
			client, err := NewClient(server.URL)
			require.NoError(t, err)

			// Execute test
			exists, err := client.IndexExists(tt.index)

			// Assertions
			require.NoError(t, err)
			assert.Equal(t, tt.expectedExists, exists)
		})
	}
}

func TestClient_RestoreSnapshot(t *testing.T) {
	tests := []struct {
		name              string
		repository        string
		snapshotName      string
		indicesPattern    string
		waitForCompletion bool
		responseStatus    int
		expectError       bool
	}{
		{
			name:              "successful restore",
			repository:        "test-repo",
			snapshotName:      "snapshot-2024-01-01",
			indicesPattern:    "*",
			waitForCompletion: true,
			responseStatus:    http.StatusOK,
			expectError:       false,
		},
		{
			name:              "snapshot not found",
			repository:        "test-repo",
			snapshotName:      "nonexistent",
			indicesPattern:    "*",
			waitForCompletion: false,
			responseStatus:    http.StatusNotFound,
			expectError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := mockESServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/_snapshot/" + tt.repository + "/" + tt.snapshotName + "/_restore"
				assert.Equal(t, expectedPath, r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)

				if tt.waitForCompletion {
					assert.Equal(t, "true", r.URL.Query().Get("wait_for_completion"))
				}

				w.WriteHeader(tt.responseStatus)
			}))
			defer server.Close()

			// Create client
			client, err := NewClient(server.URL)
			require.NoError(t, err)

			// Execute test
			err = client.RestoreSnapshot(tt.repository, tt.snapshotName, tt.indicesPattern, tt.waitForCompletion)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	client, err := NewClient("http://localhost:9200")
	require.NoError(t, err)
	assert.NotNil(t, client)
}
