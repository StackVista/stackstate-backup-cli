package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Interface defines the contract for S3 operations
// This interface allows mocking S3 operations in tests
type Interface interface {
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// Client wraps the AWS S3 client to implement our interface
type Client struct {
	client *s3.Client
}

// Ensure Client implements Interface at compile time
var _ Interface = (*Client)(nil)

// ListObjectsV2 lists objects in an S3 bucket
func (c *Client) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return c.client.ListObjectsV2(ctx, params, optFns...)
}

// GetObject retrieves an object from S3
func (c *Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return c.client.GetObject(ctx, params, optFns...)
}

// PutObject uploads an object to S3
func (c *Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return c.client.PutObject(ctx, params, optFns...)
}

// DeleteObject deletes an object from S3
func (c *Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return c.client.DeleteObject(ctx, params, optFns...)
}
