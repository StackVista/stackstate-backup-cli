package s3

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
)

// TestClientImplementsInterface verifies that Client implements Interface at compile time
func TestClientImplementsInterface(_ *testing.T) {
	var _ Interface = (*Client)(nil)
}

// TestInterfaceContract verifies that Client correctly wraps AWS S3 client methods
func TestInterfaceContract(t *testing.T) {
	// Create a client
	client, err := NewClient("http://test-s3proxy:9000", "test-access", "test-secret")
	assert.NoError(t, err)
	assert.NotNil(t, client)

	// Verify it's specifically a *Client that implements Interface
	_, ok := client.(*Client)
	assert.True(t, ok, "NewClient should return a *Client implementing Interface")
}

// TestClientMethods verifies that all interface methods are implemented
// Note: These tests don't call real S3 - they just verify the methods exist
func TestClientMethods(t *testing.T) {
	client, err := NewClient("http://test-s3proxy:9000", "test-access", "test-secret")
	assert.NoError(t, err)
	require := assert.New(t)

	ctx := context.Background()

	// Test that methods exist and can be called (will fail without real S3, but that's expected)
	// We're just verifying the interface contract

	t.Run("ListObjectsV2 method exists", func(_ *testing.T) {
		require.NotPanics(func() {
			_, _ = client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{})
		})
	})

	t.Run("GetObject method exists", func(_ *testing.T) {
		require.NotPanics(func() {
			_, _ = client.GetObject(ctx, &s3.GetObjectInput{})
		})
	})

	t.Run("PutObject method exists", func(_ *testing.T) {
		require.NotPanics(func() {
			_, _ = client.PutObject(ctx, &s3.PutObjectInput{})
		})
	})

	t.Run("DeleteObject method exists", func(_ *testing.T) {
		require.NotPanics(func() {
			_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{})
		})
	})
}

// MockS3Client is a mock implementation of the S3 Interface for testing
type MockS3Client struct {
	ListObjectsV2Func func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObjectFunc     func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObjectFunc     func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObjectFunc  func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// Ensure MockS3Client implements Interface
var _ Interface = (*MockS3Client)(nil)

// ListObjectsV2 delegates to the mock function
func (m *MockS3Client) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if m.ListObjectsV2Func != nil {
		return m.ListObjectsV2Func(ctx, params, optFns...)
	}
	return &s3.ListObjectsV2Output{}, nil
}

// GetObject delegates to the mock function
func (m *MockS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.GetObjectFunc != nil {
		return m.GetObjectFunc(ctx, params, optFns...)
	}
	return &s3.GetObjectOutput{}, nil
}

// PutObject delegates to the mock function
func (m *MockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.PutObjectFunc != nil {
		return m.PutObjectFunc(ctx, params, optFns...)
	}
	return &s3.PutObjectOutput{}, nil
}

// DeleteObject delegates to the mock function
func (m *MockS3Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.DeleteObjectFunc != nil {
		return m.DeleteObjectFunc(ctx, params, optFns...)
	}
	return &s3.DeleteObjectOutput{}, nil
}

// TestMockS3Client verifies that our mock implementation works correctly
func TestMockS3Client(t *testing.T) {
	ctx := context.Background()

	t.Run("mock with custom function", func(t *testing.T) {
		called := false
		mock := &MockS3Client{
			ListObjectsV2Func: func(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
				called = true
				return &s3.ListObjectsV2Output{}, nil
			},
		}

		_, err := mock.ListObjectsV2(ctx, &s3.ListObjectsV2Input{})
		assert.NoError(t, err)
		assert.True(t, called, "Mock function should have been called")
	})

	t.Run("mock with default behavior", func(t *testing.T) {
		mock := &MockS3Client{}

		// Should not panic and return empty output
		output, err := mock.ListObjectsV2(ctx, &s3.ListObjectsV2Input{})
		assert.NoError(t, err)
		assert.NotNil(t, output)
	})

	t.Run("mock implements interface", func(t *testing.T) {
		var client Interface = &MockS3Client{}
		assert.NotNil(t, client)
	})
}

// TestInterfaceUsage demonstrates how the interface enables dependency injection
func TestInterfaceUsage(t *testing.T) {
	// Example function that accepts the interface
	listBucketContents := func(client Interface, bucket string) error {
		_, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
			Bucket: &bucket,
		})
		return err
	}

	t.Run("with real client", func(t *testing.T) {
		client, err := NewClient("http://test:9000", "access", "secret")
		assert.NoError(t, err)

		// Function can accept real client (will fail without real S3, but that's ok)
		_ = listBucketContents(client, "test-bucket")
	})

	t.Run("with mock client", func(t *testing.T) {
		mock := &MockS3Client{
			ListObjectsV2Func: func(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
				// Simulate successful response
				return &s3.ListObjectsV2Output{}, nil
			},
		}

		// Function can accept mock client for testing
		err := listBucketContents(mock, "test-bucket")
		assert.NoError(t, err)
	})
}
