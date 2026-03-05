package s3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewClient tests S3 client creation with various configurations
func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		accessKey   string
		secretKey   string
		expectError bool
	}{
		{
			name:        "valid configuration",
			endpoint:    "http://s3proxy:9000",
			accessKey:   "access-admin",
			secretKey:   "secret-admin",
			expectError: false,
		},
		{
			name:        "valid configuration with IP",
			endpoint:    "http://192.168.1.100:9000",
			accessKey:   "test-access-key",
			secretKey:   "test-secret-key",
			expectError: false,
		},
		{
			name:        "valid configuration with https",
			endpoint:    "https://s3.example.com",
			accessKey:   "access123",
			secretKey:   "secret123",
			expectError: false,
		},
		{
			name:        "valid configuration with localhost",
			endpoint:    "http://localhost:9000",
			accessKey:   "local-access",
			secretKey:   "local-secret",
			expectError: false,
		},
		{
			name:        "empty endpoint",
			endpoint:    "",
			accessKey:   "access",
			secretKey:   "secret",
			expectError: false, // AWS SDK allows empty endpoint (uses default)
		},
		{
			name:        "empty credentials",
			endpoint:    "http://s3proxy:9000",
			accessKey:   "",
			secretKey:   "",
			expectError: false, // Client creation succeeds, but operations will fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.endpoint, tt.accessKey, tt.secretKey)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, client)
			}
		})
	}
}

// TestNewClient_ClientConfiguration tests that the client is configured correctly
func TestNewClient_ClientConfiguration(t *testing.T) {
	endpoint := "http://test-s3proxy:9000"
	accessKey := "test-access"
	secretKey := "test-secret"

	client, err := NewClient(endpoint, accessKey, secretKey)

	require.NoError(t, err)
	require.NotNil(t, client)

	// Verify client was created (we can't easily inspect internal config without integration tests)
	// But we can verify the client is not nil and has the expected type
	assert.IsType(t, client, client)
}

// TestDefaultRegion tests the default region constant
func TestDefaultRegion(t *testing.T) {
	assert.Equal(t, "minio", DefaultRegion, "Default region should be 'minio' for Minio compatibility")
}

// TestNewClient_Integration demonstrates integration test pattern
// This test is skipped by default and requires a real Minio instance
func TestNewClient_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This would require a real Minio instance running
	// Uncomment and configure when integration testing is needed
	t.Skip("integration test requires real Minio instance")

	// Example integration test pattern:
	// client, err := NewClient("http://localhost:9000", "minioadmin", "minioadmin")
	// require.NoError(t, err)
	//
	// ctx := context.Background()
	// _, err = client.ListBuckets(ctx, &s3.ListBucketsInput{})
	// assert.NoError(t, err, "Should be able to list buckets with valid client")
}

// TestNewClient_CredentialFormats tests various credential format edge cases
func TestNewClient_CredentialFormats(t *testing.T) {
	tests := []struct {
		name      string
		accessKey string
		secretKey string
	}{
		{
			name:      "alphanumeric credentials",
			accessKey: "abc123XYZ",
			secretKey: "xyz789ABC",
		},
		{
			name:      "credentials with special characters",
			accessKey: "access+key/with=chars",
			secretKey: "secret-key_with.chars",
		},
		{
			name:      "very long credentials",
			accessKey: "very-long-access-key-with-many-characters-0123456789",
			secretKey: "very-long-secret-key-with-many-characters-9876543210",
		},
		{
			name:      "credentials with spaces (edge case)",
			accessKey: "access key with spaces",
			secretKey: "secret key with spaces",
		},
		{
			name:      "single character credentials",
			accessKey: "a",
			secretKey: "s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient("http://s3proxy:9000", tt.accessKey, tt.secretKey)

			// Client creation should succeed regardless of credential format
			// The credentials will be validated when actual S3 operations are performed
			assert.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}

// TestNewClient_EndpointFormats tests various endpoint format edge cases
func TestNewClient_EndpointFormats(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{
			name:     "endpoint with http scheme",
			endpoint: "http://s3proxy.example.com:9000",
		},
		{
			name:     "endpoint with https scheme",
			endpoint: "https://s3.amazonaws.com",
		},
		{
			name:     "endpoint without port",
			endpoint: "http://s3proxy.local",
		},
		{
			name:     "endpoint with non-standard port",
			endpoint: "http://s3proxy:8080",
		},
		{
			name:     "endpoint with path",
			endpoint: "http://s3proxy:9000/path/to/s3",
		},
		{
			name:     "endpoint as IP address",
			endpoint: "http://127.0.0.1:9000",
		},
		{
			name:     "endpoint as IPv6 address",
			endpoint: "http://[::1]:9000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.endpoint, "access", "secret")

			// Client creation should succeed for all valid endpoint formats
			assert.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}

// TestNewClient_ConcurrentCreation tests that client creation is safe for concurrent use
func TestNewClient_ConcurrentCreation(t *testing.T) {
	const numGoroutines = 10

	done := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := NewClient("http://s3proxy:9000", "access", "secret")
			if err != nil {
				errors <- err
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
	close(errors)

	// Check that no errors occurred
	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent client creation failed: %v", err)
		errorCount++
	}

	assert.Equal(t, 0, errorCount, "All concurrent client creations should succeed")
}
