package s3

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	multipartArchiveSuffixLength = 2
)

// Object represents a simplified S3 object with key metadata
type Object struct {
	Key          string
	LastModified time.Time
	Size         int64
}

// FilterMultipartBackupObjects filters S3 objects based on whether the archive is split or not
// If it is not multipartArchive, it filters out multipart archives (files ending with .digits)
// Otherwise, it groups multipart archives by base name and sums their sizes
func FilterMultipartBackupObjects(objects []s3types.Object, multipartArchive bool) []Object {
	if !multipartArchive {
		return filterNonMultipart(objects)
	}
	return aggregateMultipart(objects)
}

// filterNonMultipart filters out multipart archives (files ending with .digits)
func filterNonMultipart(objects []s3types.Object) []Object {
	var filteredObjects []Object

	for _, obj := range objects {
		key := aws.ToString(obj.Key)

		// Skip if it ends with .digits (multipart archive)
		if strings.Contains(key, ".") {
			parts := strings.Split(key, ".")
			lastPart := parts[len(parts)-1]
			isDigits := true
			for _, c := range lastPart {
				if c < '0' || c > '9' {
					isDigits = false
					break
				}
			}
			if isDigits && len(lastPart) > 0 {
				continue
			}
		}

		filteredObjects = append(filteredObjects, Object{
			Key:          key,
			LastModified: aws.ToTime(obj.LastModified),
			Size:         aws.ToInt64(obj.Size),
		})
	}

	return filteredObjects
}

// aggregateMultipart groups multipart archives by base name and sums their sizes
func aggregateMultipart(objects []s3types.Object) []Object {
	// Map to group objects by base name
	archiveMap := make(map[string]*Object)

	for _, obj := range objects {
		key := aws.ToString(obj.Key)

		// Check if this is a multipart file (ends with .NN where NN are digits)
		baseName, isMultipart := getBaseName(key)
		if !isMultipart {
			// Not a multipart file, include as-is
			archiveMap[key] = &Object{
				Key:          key,
				LastModified: aws.ToTime(obj.LastModified),
				Size:         aws.ToInt64(obj.Size),
			}
			continue
		}

		// Group multipart files by base name
		if existing, exists := archiveMap[baseName]; exists {
			// Add size to existing entry
			existing.Size += aws.ToInt64(obj.Size)
			// Keep the most recent LastModified time
			if aws.ToTime(obj.LastModified).After(existing.LastModified) {
				existing.LastModified = aws.ToTime(obj.LastModified)
			}
		} else {
			// Create new entry
			archiveMap[baseName] = &Object{
				Key:          baseName,
				LastModified: aws.ToTime(obj.LastModified),
				Size:         aws.ToInt64(obj.Size),
			}
		}
	}

	// Convert map to slice
	var filteredObjects []Object
	for _, obj := range archiveMap {
		filteredObjects = append(filteredObjects, *obj)
	}

	return filteredObjects
}

// getBaseName extracts the base name from a multipart archive filename
// Returns (baseName, isMultipart)
// Example: "backup.graph.00" -> ("backup.graph", true)
//
//	"backup.graph" -> ("backup.graph", false)
func getBaseName(key string) (string, bool) {
	if !strings.Contains(key, ".") {
		return key, false
	}

	parts := strings.Split(key, ".")
	lastPart := parts[len(parts)-1]

	// Check if last part is all digits (2 digits for part numbers like .00, .01, etc.)
	if len(lastPart) == multipartArchiveSuffixLength {
		isDigits := true
		for _, c := range lastPart {
			if c < '0' || c > '9' {
				isDigits = false
				break
			}
		}
		if isDigits {
			// Remove the .NN suffix to get base name
			baseName := strings.Join(parts[:len(parts)-1], ".")
			return baseName, true
		}
	}

	return key, false
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
