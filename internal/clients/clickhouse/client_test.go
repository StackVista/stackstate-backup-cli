package clickhouse

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		wantError bool
	}{
		{
			name:      "valid backupAPIURL",
			baseURL:   "http://localhost:7171",
			wantError: false,
		},
		{
			name:      "empty backupAPIURL",
			baseURL:   "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.baseURL, "localhost:9000", "default", "default", "password")
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
				assert.Equal(t, tt.baseURL, client.backupAPIURL)
			}
		})
	}
}

func TestListBackups_Success(t *testing.T) {
	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/backup/list", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// NDJSON format: newline-delimited JSON objects
		_, _ = w.Write([]byte(`{"name":"full_2025-11-18T11-45-04","created":"2025-11-18 11:45:07","size":48915,"data_size":2649,"metadata_size":7955,"compressed_size":40960,"location":"remote","required":"","desc":"tar, regular"}
{"name":"incremental_2025-11-18T12-45-00","created":"2025-11-18 12:45:03","size":21827776,"data_size":21301225,"metadata_size":10944,"compressed_size":21816832,"location":"remote","required":"full_2025-11-18T11-45-04","desc":"tar, regular"}
`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "localhost:9000", "default", "default", "password")
	require.NoError(t, err)

	backups, err := client.ListBackups()
	require.NoError(t, err)
	assert.Len(t, backups, 2)

	assert.Equal(t, "full_2025-11-18T11-45-04", backups[0].Name)
	assert.Equal(t, "2025-11-18 11:45:07", backups[0].Created)
	assert.Equal(t, int64(48915), backups[0].Size)

	assert.Equal(t, "incremental_2025-11-18T12-45-00", backups[1].Name)
	assert.Equal(t, "2025-11-18 12:45:03", backups[1].Created)
	assert.Equal(t, int64(21827776), backups[1].Size)
}

func TestListBackups_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Empty NDJSON response - no content
		_, _ = w.Write([]byte(``))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "localhost:9000", "default", "default", "password")
	require.NoError(t, err)

	backups, err := client.ListBackups()
	require.NoError(t, err)
	assert.Empty(t, backups)
}

func TestListBackups_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "localhost:9000", "default", "default", "password")
	require.NoError(t, err)

	backups, err := client.ListBackups()
	assert.Error(t, err)
	assert.Nil(t, backups)
	assert.Contains(t, err.Error(), "backup API returned status 500")
}

func TestListBackups_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "localhost:9000", "default", "default", "password")
	require.NoError(t, err)

	backups, err := client.ListBackups()
	assert.Error(t, err)
	assert.Nil(t, backups)
	assert.Contains(t, err.Error(), "failed to decode response")
}
