package s3

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Object represents a simplified S3 object with key metadata
type Object struct {
	Key          string
	LastModified time.Time
	Size         int64
}

// FilterBackupObjects filters out backup part files ending with .digits.
func FilterBackupObjects(objects []s3types.Object) []Object {
	var filteredObjects []Object

	for _, obj := range objects {
		key := aws.ToString(obj.Key)

		if hasNumericFileSuffix(key) {
			continue
		}

		filteredObjects = append(filteredObjects, Object{
			Key:          key,
			LastModified: aws.ToTime(obj.LastModified),
			Size:         aws.ToInt64(obj.Size),
		})
	}

	return filteredObjects
}

// ConvertBackupObjects filters out backup part files ending with .digits.
func ConvertBackupObjects(objects []s3types.Object) []Object {
	var filteredObjects []Object

	for _, obj := range objects {
		key := aws.ToString(obj.Key)

		filteredObjects = append(filteredObjects, Object{
			Key:          key,
			LastModified: aws.ToTime(obj.LastModified),
			Size:         aws.ToInt64(obj.Size),
		})
	}

	return filteredObjects
}

func hasNumericFileSuffix(key string) bool {
	if !strings.Contains(key, ".") {
		return false
	}

	parts := strings.Split(key, ".")
	lastPart := parts[len(parts)-1]

	if len(lastPart) == 0 {
		return false
	}

	for _, c := range lastPart {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// FilterByPrefixAndRegex filters objects to only include direct children of the given prefix
// that match the specified regex pattern. It excludes objects in nested subdirectories and
// strips the prefix from the key, returning just the filename portion.
//
// For example, with prefix "backups/" and pattern `^sts-backup-.*\.graph$`:
//   - "backups/sts-backup-20240101.graph" -> included, Key becomes "sts-backup-20240101.graph"
//   - "backups/other-file.txt" -> excluded (doesn't match pattern)
//   - "backups/subdir/sts-backup-20240101.graph" -> excluded (nested)
func FilterByPrefixAndRegex(objects []Object, prefix string, pattern string) ([]Object, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	var filtered []Object
	for _, obj := range objects {
		// Strip the prefix from the key
		relativePath := strings.TrimPrefix(obj.Key, prefix)

		// Skip if the relative path contains a slash (indicating nested directory)
		if strings.Contains(relativePath, "/") {
			continue
		}

		// Skip empty relative paths (the prefix itself)
		if relativePath == "" {
			continue
		}

		// Check if the filename matches the regex pattern
		if !re.MatchString(relativePath) {
			continue
		}

		filtered = append(filtered, Object{
			Key:          relativePath,
			LastModified: obj.LastModified,
			Size:         obj.Size,
		})
	}
	return filtered, nil
}

func FilterByCommonPrefix(objects []s3types.CommonPrefix) []Object {
	var filteredObjects []Object

	for _, obj := range objects {
		key := aws.ToString(obj.Prefix)
		key = strings.TrimSuffix(key, "/")
		filteredObjects = append(filteredObjects, Object{
			Key:          key,
			LastModified: aws.ToTime(nil),
			Size:         0,
		})
	}

	return filteredObjects
}
