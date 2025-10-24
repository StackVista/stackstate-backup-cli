package s3

import (
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

// FilterBackupObjects filters S3 objects based on archive split size configuration
// If archiveSplitSize is "0", it filters out multipart archives (files ending with .digits)
// Otherwise, it only includes the first part of multipart archives (files ending with .00)
func FilterBackupObjects(objects []s3types.Object, archiveSplitSize string) []Object {
	var filteredObjects []Object

	for _, obj := range objects {
		key := aws.ToString(obj.Key)

		// Skip if archiveSplitSize is "0" and object is multipart (ends with .digits)
		if archiveSplitSize == "0" {
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
		} else {
			// Only show first file of multipart archive (ends with .00)
			if !strings.HasSuffix(key, ".00") {
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
