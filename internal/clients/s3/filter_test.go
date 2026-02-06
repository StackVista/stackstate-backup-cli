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
			result := FilterMultipartBackupObjects(tt.objects, false)

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
			name: "groups multipart archives and sums their sizes",
			objects: []s3types.Object{
				{Key: aws.String("backup-2024-01-01.00"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-2024-01-01.01"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-2024-01-01.02"), Size: aws.Int64(300000000)},
				{Key: aws.String("backup-2024-01-02.00"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-2024-01-02.01"), Size: aws.Int64(400000000)},
			},
			multipartArchive: true,
			expectedCount:    2,
			expectedKeys:     []string{"backup-2024-01-01", "backup-2024-01-02"},
		},
		{
			name: "includes both multipart and single files",
			objects: []s3types.Object{
				{Key: aws.String("backup.tar.gz"), Size: aws.Int64(1000)},
				{Key: aws.String("backup-split.00"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-split.01"), Size: aws.Int64(500000000)},
				{Key: aws.String("backup-single"), Size: aws.Int64(1000000)},
			},
			multipartArchive: true,
			expectedCount:    3,
			expectedKeys:     []string{"backup.tar.gz", "backup-split", "backup-single"},
		},
		{
			name: "handles different split size values",
			objects: []s3types.Object{
				{Key: aws.String("backup-1G.00"), Size: aws.Int64(1000000000)},
				{Key: aws.String("backup-1G.01"), Size: aws.Int64(500000000)},
			},
			multipartArchive: true,
			expectedCount:    1,
			expectedKeys:     []string{"backup-1G"},
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
			expectedCount:    2,
			expectedKeys:     []string{"backup.00.tar", "backup-final"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterMultipartBackupObjects(tt.objects, tt.multipartArchive)

			assert.Equal(t, tt.expectedCount, len(result))

			resultKeys := make([]string, len(result))
			for i, obj := range result {
				resultKeys[i] = obj.Key
			}

			assert.ElementsMatch(t, tt.expectedKeys, resultKeys)
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
			Size:         aws.Int64(100000000),
			LastModified: aws.Time(yesterday),
		},
		{
			Key:          aws.String("backup-2024-01-02.01"),
			Size:         aws.Int64(50000000),
			LastModified: aws.Time(yesterday.Add(1 * time.Minute)), // Slightly later
		},
	}

	// Test single file mode
	result := FilterMultipartBackupObjects(objects, false)
	assert.Equal(t, 1, len(result))
	assert.Equal(t, "backup-2024-01-01.tar.gz", result[0].Key)
	assert.Equal(t, int64(1234567890), result[0].Size)
	assert.Equal(t, now.Unix(), result[0].LastModified.Unix())

	// Test multipart mode - should group parts and sum sizes
	result = FilterMultipartBackupObjects(objects, true)
	assert.Equal(t, 2, len(result)) // tar.gz file + grouped multipart

	// Find the multipart archive result
	var multipartResult *Object
	var singleResult *Object
	for i := range result {
		switch result[i].Key {
		case "backup-2024-01-02":
			multipartResult = &result[i]
		case "backup-2024-01-01.tar.gz":
			singleResult = &result[i]
		}
	}

	assert.NotNil(t, multipartResult, "Should find grouped multipart archive")
	assert.NotNil(t, singleResult, "Should find single file")

	// Verify multipart archive has summed size
	assert.Equal(t, "backup-2024-01-02", multipartResult.Key)
	assert.Equal(t, int64(150000000), multipartResult.Size)                                   // 100M + 50M
	assert.Equal(t, yesterday.Add(1*time.Minute).Unix(), multipartResult.LastModified.Unix()) // Most recent timestamp

	// Verify single file
	assert.Equal(t, "backup-2024-01-01.tar.gz", singleResult.Key)
	assert.Equal(t, int64(1234567890), singleResult.Size)
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
			result := FilterMultipartBackupObjects(tt.objects, tt.multipartArchive)
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
			expectedCount:    2, // Two grouped multipart archives
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
			expectedCount:    3, // One single file + two grouped multipart archives
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterMultipartBackupObjects(tt.objects, tt.multipartArchive)
			assert.Equal(t, tt.expectedCount, len(result), "Scenario: %s", tt.scenario)
		})
	}
}

// TestFilterBackupObjects_SizeSummation tests that sizes are correctly summed for multipart archives
func TestFilterBackupObjects_SizeSummation(t *testing.T) {
	objects := []s3types.Object{
		{Key: aws.String("sts-backup-20251028-1546.graph.00"), Size: aws.Int64(104857600)},
		{Key: aws.String("sts-backup-20251028-1546.graph.01"), Size: aws.Int64(6885342)},
		{Key: aws.String("sts-backup-20251029-0300.graph.00"), Size: aws.Int64(104857600)},
		{Key: aws.String("sts-backup-20251029-0300.graph.01"), Size: aws.Int64(4348555)},
		{Key: aws.String("sts-backup-20251029-0924.graph.00"), Size: aws.Int64(104857600)},
		{Key: aws.String("sts-backup-20251029-0924.graph.01"), Size: aws.Int64(6567239)},
	}

	result := FilterMultipartBackupObjects(objects, true)

	// Should have 3 grouped archives
	assert.Equal(t, 3, len(result))

	// Create a map for easier lookup
	sizeMap := make(map[string]int64)
	for _, obj := range result {
		sizeMap[obj.Key] = obj.Size
	}

	// Verify sizes are correctly summed
	assert.Equal(t, int64(111742942), sizeMap["sts-backup-20251028-1546.graph"]) // 104857600 + 6885342
	assert.Equal(t, int64(109206155), sizeMap["sts-backup-20251029-0300.graph"]) // 104857600 + 4348555
	assert.Equal(t, int64(111424839), sizeMap["sts-backup-20251029-0924.graph"]) // 104857600 + 6567239
}

// TestFilterByPrefixAndRegex tests the combined filtering by prefix and regex pattern
func TestFilterByPrefixAndRegex(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		objects      []Object
		prefix       string
		pattern      string
		expectedKeys []string
		expectError  bool
	}{
		{
			name: "filters stackgraph backups with prefix and .graph extension",
			objects: []Object{
				{Key: "backups/sts-backup-20240101.graph", Size: 1000, LastModified: now},
				{Key: "backups/sts-backup-20240102.graph", Size: 2000, LastModified: now},
				{Key: "backups/other-file.txt", Size: 500, LastModified: now},
				{Key: "backups/sts-backup-20240103.tar.gz", Size: 3000, LastModified: now},
			},
			prefix:       "backups/",
			pattern:      `^sts-backup-.*\.graph$`,
			expectedKeys: []string{"sts-backup-20240101.graph", "sts-backup-20240102.graph"},
			expectError:  false,
		},
		{
			name: "filters settings backups with .sty extension",
			objects: []Object{
				{Key: "settings/sts-backup-20240101.sty", Size: 1000, LastModified: now},
				{Key: "settings/sts-backup-20240102.sty", Size: 2000, LastModified: now},
				{Key: "settings/other-file.txt", Size: 500, LastModified: now},
				{Key: "settings/sts-backup-20240103.graph", Size: 3000, LastModified: now},
			},
			prefix:       "settings/",
			pattern:      `^sts-backup-.*\.sty$`,
			expectedKeys: []string{"sts-backup-20240101.sty", "sts-backup-20240102.sty"},
			expectError:  false,
		},
		{
			name: "excludes nested files even if they match pattern",
			objects: []Object{
				{Key: "backups/sts-backup-20240101.graph", Size: 1000, LastModified: now},
				{Key: "backups/old/sts-backup-20240102.graph", Size: 2000, LastModified: now},
				{Key: "backups/archive/2023/sts-backup-20230101.graph", Size: 3000, LastModified: now},
			},
			prefix:       "backups/",
			pattern:      `^sts-backup-.*\.graph$`,
			expectedKeys: []string{"sts-backup-20240101.graph"},
			expectError:  false,
		},
		{
			name: "works with empty prefix",
			objects: []Object{
				{Key: "sts-backup-20240101.graph", Size: 1000, LastModified: now},
				{Key: "sts-backup-20240102.graph", Size: 2000, LastModified: now},
				{Key: "subdir/sts-backup-20240103.graph", Size: 3000, LastModified: now},
				{Key: "other-file.txt", Size: 500, LastModified: now},
			},
			prefix:       "",
			pattern:      `^sts-backup-.*\.graph$`,
			expectedKeys: []string{"sts-backup-20240101.graph", "sts-backup-20240102.graph"},
			expectError:  false,
		},
		{
			name: "returns empty slice when no matches",
			objects: []Object{
				{Key: "backups/other-file.txt", Size: 500, LastModified: now},
				{Key: "backups/another-file.log", Size: 100, LastModified: now},
			},
			prefix:       "backups/",
			pattern:      `^sts-backup-.*\.graph$`,
			expectedKeys: []string{},
			expectError:  false,
		},
		{
			name:         "handles empty object list",
			objects:      []Object{},
			prefix:       "backups/",
			pattern:      `^sts-backup-.*\.graph$`,
			expectedKeys: []string{},
			expectError:  false,
		},
		{
			name: "returns error for invalid regex",
			objects: []Object{
				{Key: "backups/sts-backup-20240101.graph", Size: 1000, LastModified: now},
			},
			prefix:       "backups/",
			pattern:      `[invalid`,
			expectedKeys: nil,
			expectError:  true,
		},
		{
			name: "excludes the prefix directory itself",
			objects: []Object{
				{Key: "backups/", Size: 0, LastModified: now},
				{Key: "backups/sts-backup-20240101.graph", Size: 1000, LastModified: now},
			},
			prefix:       "backups/",
			pattern:      `^sts-backup-.*\.graph$`,
			expectedKeys: []string{"sts-backup-20240101.graph"},
			expectError:  false,
		},
		{
			name: "returns empty when all files are nested",
			objects: []Object{
				{Key: "backups/old/sts-backup-20240101.graph", Size: 1000, LastModified: now},
				{Key: "backups/archive/sts-backup-20240102.graph", Size: 2000, LastModified: now},
			},
			prefix:       "backups/",
			pattern:      `^sts-backup-.*\.graph$`,
			expectedKeys: []string{},
			expectError:  false,
		},
		{
			name: "filters with complex regex pattern",
			objects: []Object{
				{Key: "backups/sts-backup-20240101-1200.graph", Size: 1000, LastModified: now},
				{Key: "backups/sts-backup-20240102-1300.graph", Size: 2000, LastModified: now},
				{Key: "backups/sts-backup-invalid.graph", Size: 500, LastModified: now},
				{Key: "backups/sts-backup-20240103.graph", Size: 3000, LastModified: now},
			},
			prefix:       "backups/",
			pattern:      `^sts-backup-\d{8}-\d{4}\.graph$`,
			expectedKeys: []string{"sts-backup-20240101-1200.graph", "sts-backup-20240102-1300.graph"},
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FilterByPrefixAndRegex(tt.objects, tt.prefix, tt.pattern)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				return
			}

			assert.NoError(t, err)

			resultKeys := make([]string, len(result))
			for i, obj := range result {
				resultKeys[i] = obj.Key
			}

			assert.Equal(t, tt.expectedKeys, resultKeys)
		})
	}
}

// TestFilterByPrefixAndRegex_PreservesMetadata tests that object metadata is preserved after filtering
func TestFilterByPrefixAndRegex_PreservesMetadata(t *testing.T) {
	now := time.Now()

	objects := []Object{
		{Key: "backups/sts-backup-20240101.graph", Size: 1234567890, LastModified: now},
		{Key: "backups/other-file.txt", Size: 500, LastModified: now.Add(-24 * time.Hour)},
		{Key: "backups/nested/sts-backup-20240102.graph", Size: 999, LastModified: now},
	}

	result, err := FilterByPrefixAndRegex(objects, "backups/", `^sts-backup-.*\.graph$`)

	assert.NoError(t, err)
	assert.Equal(t, 1, len(result))
	assert.Equal(t, "sts-backup-20240101.graph", result[0].Key)
	assert.Equal(t, int64(1234567890), result[0].Size)
	assert.Equal(t, now.Unix(), result[0].LastModified.Unix())
}
