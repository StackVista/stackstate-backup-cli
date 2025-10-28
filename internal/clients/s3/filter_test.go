package s3

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
)

// TestFilterBackupObjects_SingleFileMode tests filtering when it is not multipartArchive
func TestFilterBackupObjects_SingleFileMode(t *testing.T) {
	tests := []struct {
		name          string
		objects       []s3types.Object
		expectedCount int
		expectedKeys  []string
	}{
		{
			name: "filters out multipart archives with numeric extensions",
			objects: []s3types.Object{
				{Key: aws.String("backup-2024-01-01.tar.gz"), Size: aws.Int64(1000)},
				{Key: aws.String("backup-2024-01-02.00"), Size: aws.Int64(1000)},
				{Key: aws.String("backup-2024-01-02.01"), Size: aws.Int64(1000)},
				{Key: aws.String("backup-2024-01-02.02"), Size: aws.Int64(1000)},
				{Key: aws.String("backup-2024-01-03.tar"), Size: aws.Int64(1000)},
			},
			expectedCount: 2,
			expectedKeys:  []string{"backup-2024-01-01.tar.gz", "backup-2024-01-03.tar"},
		},
		{
			name: "includes files without extensions",
			objects: []s3types.Object{
				{Key: aws.String("backup-no-extension"), Size: aws.Int64(1000)},
				{Key: aws.String("backup-with-ext.tar"), Size: aws.Int64(2000)},
			},
			expectedCount: 2,
			expectedKeys:  []string{"backup-no-extension", "backup-with-ext.tar"},
		},
		{
			name: "includes files with non-numeric extensions",
			objects: []s3types.Object{
				{Key: aws.String("backup.tar.gz"), Size: aws.Int64(1000)},
				{Key: aws.String("backup.log"), Size: aws.Int64(100)},
				{Key: aws.String("backup.00"), Size: aws.Int64(1000)},
				{Key: aws.String("backup.abc"), Size: aws.Int64(1000)},
			},
			expectedCount: 3,
			expectedKeys:  []string{"backup.tar.gz", "backup.log", "backup.abc"},
		},
		{
			name: "handles edge case with single digit extension",
			objects: []s3types.Object{
				{Key: aws.String("backup.0"), Size: aws.Int64(1000)},
				{Key: aws.String("backup.1"), Size: aws.Int64(1000)},
				{Key: aws.String("backup.tar"), Size: aws.Int64(2000)},
			},
			expectedCount: 1,
			expectedKeys:  []string{"backup.tar"},
		},
		{
			name: "handles multiple dots in filename",
			objects: []s3types.Object{
				{Key: aws.String("backup.2024.01.01.tar.gz"), Size: aws.Int64(1000)},
				{Key: aws.String("backup.2024.01.02.00"), Size: aws.Int64(1000)},
				{Key: aws.String("backup.2024.01.03.999"), Size: aws.Int64(1000)},
			},
			expectedCount: 1,
			expectedKeys:  []string{"backup.2024.01.01.tar.gz"},
		},
		{
			name:          "handles empty object list",
			objects:       []s3types.Object{},
			expectedCount: 0,
			expectedKeys:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterBackupObjects(tt.objects, false)

			assert.Equal(t, tt.expectedCount, len(result))

			resultKeys := make([]string, len(result))
			for i, obj := range result {
				resultKeys[i] = obj.Key
			}

			assert.Equal(t, tt.expectedKeys, resultKeys)
		})
	}
}

// TestFilterBackupObjects_MultipartMode tests filtering when it is multipartArchive
func TestFilterBackupObjects_MultipartMode(t *testing.T) {
	tests := []struct {
		name             string
		objects          []s3types.Object
		multipartArchive bool
		expectedCount    int
		expectedKeys     []string
	}{
		{
			name: "only includes first part of multipart archives (.00)",
			objects: []s3types.Object{
				{Key: aws.String("backup-2024-01-01.00"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-2024-01-01.01"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-2024-01-01.02"), Size: aws.Int64(300000000)},
				{Key: aws.String("backup-2024-01-02.00"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-2024-01-02.01"), Size: aws.Int64(400000000)},
			},
			multipartArchive: true,
			expectedCount:    2,
			expectedKeys:     []string{"backup-2024-01-01.00", "backup-2024-01-02.00"},
		},
		{
			name: "filters out files not ending with .00",
			objects: []s3types.Object{
				{Key: aws.String("backup.tar.gz"), Size: aws.Int64(1000)},
				{Key: aws.String("backup-split.00"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-split.01"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-single"), Size: aws.Int64(1000000)},
			},
			multipartArchive: true,
			expectedCount:    1,
			expectedKeys:     []string{"backup-split.00"},
		},
		{
			name: "handles different split size values",
			objects: []s3types.Object{
				{Key: aws.String("backup-1G.00"), Size: aws.Int64(1000000000)},
				{Key: aws.String("backup-1G.01"), Size: aws.Int64(500000000)},
			},
			multipartArchive: true,
			expectedCount:    1,
			expectedKeys:     []string{"backup-1G.00"},
		},
		{
			name:             "handles empty object list",
			objects:          []s3types.Object{},
			multipartArchive: true,
			expectedCount:    0,
			expectedKeys:     []string{},
		},
		{
			name: "handles objects with .00 in middle of filename",
			objects: []s3types.Object{
				{Key: aws.String("backup.00.tar"), Size: aws.Int64(1000)},
				{Key: aws.String("backup-final.00"), Size: aws.Int64(500000000)},
			},
			multipartArchive: true,
			expectedCount:    1,
			expectedKeys:     []string{"backup-final.00"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterBackupObjects(tt.objects, tt.multipartArchive)

			assert.Equal(t, tt.expectedCount, len(result))

			resultKeys := make([]string, len(result))
			for i, obj := range result {
				resultKeys[i] = obj.Key
			}

			assert.Equal(t, tt.expectedKeys, resultKeys)
		})
	}
}

// TestFilterBackupObjects_ObjectMetadata tests that metadata is preserved correctly
func TestFilterBackupObjects_ObjectMetadata(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)

	objects := []s3types.Object{
		{
			Key:          aws.String("backup-2024-01-01.tar.gz"),
			Size:         aws.Int64(1234567890),
			LastModified: aws.Time(now),
		},
		{
			Key:          aws.String("backup-2024-01-02.00"),
			Size:         aws.Int64(9876543210),
			LastModified: aws.Time(yesterday),
		},
	}

	// Test single file mode
	result := FilterBackupObjects(objects, false)
	assert.Equal(t, 1, len(result))
	assert.Equal(t, "backup-2024-01-01.tar.gz", result[0].Key)
	assert.Equal(t, int64(1234567890), result[0].Size)
	assert.Equal(t, now.Unix(), result[0].LastModified.Unix())

	// Test multipart mode
	result = FilterBackupObjects(objects, true)
	assert.Equal(t, 1, len(result))
	assert.Equal(t, "backup-2024-01-02.00", result[0].Key)
	assert.Equal(t, int64(9876543210), result[0].Size)
	assert.Equal(t, yesterday.Unix(), result[0].LastModified.Unix())
}

// TestFilterBackupObjects_EdgeCases tests edge cases and boundary conditions
func TestFilterBackupObjects_EdgeCases(t *testing.T) {
	tests := []struct {
		name             string
		objects          []s3types.Object
		multipartArchive bool
		expectedCount    int
	}{
		{
			name: "file ending with just a dot",
			objects: []s3types.Object{
				{Key: aws.String("backup."), Size: aws.Int64(1000)},
			},
			multipartArchive: false,
			expectedCount:    1, // Empty extension, should be included
		},
		{
			name: "file with mixed alphanumeric extension",
			objects: []s3types.Object{
				{Key: aws.String("backup.123abc"), Size: aws.Int64(1000)},
			},
			multipartArchive: false,
			expectedCount:    1, // Contains non-digits, should be included
		},
		{
			name: "very long numeric extension",
			objects: []s3types.Object{
				{Key: aws.String("backup.00000000000000000001"), Size: aws.Int64(1000)},
			},
			multipartArchive: false,
			expectedCount:    0, // All digits, should be filtered
		},
		{
			name: "multipart with .00 but it is not multipartArchive",
			objects: []s3types.Object{
				{Key: aws.String("backup.00"), Size: aws.Int64(1000)},
			},
			multipartArchive: false,
			expectedCount:    0, // Numeric extension in single file mode, should be filtered
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterBackupObjects(tt.objects, tt.multipartArchive)
			assert.Equal(t, tt.expectedCount, len(result))
		})
	}
}

// TestFilterBackupObjects_RealWorldScenarios tests realistic backup scenarios
func TestFilterBackupObjects_RealWorldScenarios(t *testing.T) {
	tests := []struct {
		name             string
		scenario         string
		objects          []s3types.Object
		multipartArchive bool
		expectedCount    int
	}{
		{
			name:     "stackgraph backups without splitting",
			scenario: "Single large archive files",
			objects: []s3types.Object{
				{Key: aws.String("stackgraph-backup-2024-01-01.tar.gz"), Size: aws.Int64(10000000)},
				{Key: aws.String("stackgraph-backup-2024-01-02.tar.gz"), Size: aws.Int64(12000000)},
				{Key: aws.String("stackgraph-backup-2024-01-03.tar.gz"), Size: aws.Int64(11000000)},
			},
			multipartArchive: false,
			expectedCount:    3,
		},
		{
			name:     "stackgraph backups with 500M splitting",
			scenario: "Split archives at 500M",
			objects: []s3types.Object{
				{Key: aws.String("stackgraph-backup-2024-01-01.00"), Size: aws.Int64(500000000)},
				{Key: aws.String("stackgraph-backup-2024-01-01.01"), Size: aws.Int64(500000000)},
				{Key: aws.String("stackgraph-backup-2024-01-01.02"), Size: aws.Int64(200000000)},
				{Key: aws.String("stackgraph-backup-2024-01-02.00"), Size: aws.Int64(500000000)},
				{Key: aws.String("stackgraph-backup-2024-01-02.01"), Size: aws.Int64(300000000)},
			},
			multipartArchive: true,
			expectedCount:    2, // Only .00 files
		},
		{
			name:     "mixed backup types in same bucket",
			scenario: "Single and split backups mixed",
			objects: []s3types.Object{
				{Key: aws.String("backup-old.tar.gz"), Size: aws.Int64(1000000)},
				{Key: aws.String("backup-new-split.00"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-new-split.01"), Size: aws.Int64(400000000)},
				{Key: aws.String("backup-old-split.00"), Size: aws.Int64(1000)},
				{Key: aws.String("backup-old-split.01"), Size: aws.Int64(1000)},
			},
			multipartArchive: true,
			expectedCount:    2, // Two .00 files
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterBackupObjects(tt.objects, tt.multipartArchive)
			assert.Equal(t, tt.expectedCount, len(result), "Scenario: %s", tt.scenario)
		})
	}
}
