package s3

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	// DefaultRegion is the default region for Minio S3 clients
	DefaultRegion = "minio"
)

// NewClient creates a new S3 client configured for Minio
// Returns an Interface that wraps the underlying AWS S3 client
func NewClient(endpoint, accessKey, secretKey string) (Interface, error) {
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(DefaultRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKey,
			secretKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	return &Client{client: s3Client}, nil
}
