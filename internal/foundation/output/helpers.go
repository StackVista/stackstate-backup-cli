package output

import "fmt"

// FormatBytes formats bytes to human-readable format without spaces (e.g., "624MiB")
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	return fmt.Sprintf("%.0f%s", float64(bytes)/float64(div), units[exp])
}
