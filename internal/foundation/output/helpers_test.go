package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

//nolint:funlen
func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		// Bytes (< 1024)
		{
			name:     "zero bytes",
			bytes:    0,
			expected: "0B",
		},
		{
			name:     "single byte",
			bytes:    1,
			expected: "1B",
		},
		{
			name:     "multiple bytes",
			bytes:    512,
			expected: "512B",
		},
		{
			name:     "max bytes before KiB",
			bytes:    1023,
			expected: "1023B",
		},

		// KiB (1024 to < 1024²)
		{
			name:     "exactly 1 KiB",
			bytes:    1024,
			expected: "1KiB",
		},
		{
			name:     "1.5 KiB",
			bytes:    1536, // 1024 * 1.5
			expected: "2KiB",
		},
		{
			name:     "100 KiB",
			bytes:    102400, // 1024 * 100
			expected: "100KiB",
		},
		{
			name:     "max KiB before MiB",
			bytes:    1048575, // 1024² - 1
			expected: "1024KiB",
		},

		// MiB (1024² to < 1024³)
		{
			name:     "exactly 1 MiB",
			bytes:    1048576, // 1024²
			expected: "1MiB",
		},
		{
			name:     "10 MiB",
			bytes:    10485760, // 1024² * 10
			expected: "10MiB",
		},
		{
			name:     "624 MiB",
			bytes:    654311424, // 1024² * 624
			expected: "624MiB",
		},
		{
			name:     "max MiB before GiB",
			bytes:    1073741823, // 1024³ - 1
			expected: "1024MiB",
		},

		// GiB (1024³ to < 1024⁴)
		{
			name:     "exactly 1 GiB",
			bytes:    1073741824, // 1024³
			expected: "1GiB",
		},
		{
			name:     "5 GiB",
			bytes:    5368709120, // 1024³ * 5
			expected: "5GiB",
		},
		{
			name:     "100 GiB",
			bytes:    107374182400, // 1024³ * 100
			expected: "100GiB",
		},
		{
			name:     "max GiB before TiB",
			bytes:    1099511627775, // 1024⁴ - 1
			expected: "1024GiB",
		},

		// TiB (1024⁴ to < 1024⁵)
		{
			name:     "exactly 1 TiB",
			bytes:    1099511627776, // 1024⁴
			expected: "1TiB",
		},
		{
			name:     "2 TiB",
			bytes:    2199023255552, // 1024⁴ * 2
			expected: "2TiB",
		},
		{
			name:     "10 TiB",
			bytes:    10995116277760, // 1024⁴ * 10
			expected: "10TiB",
		},
		{
			name:     "max TiB before PiB",
			bytes:    1125899906842623, // 1024⁵ - 1
			expected: "1024TiB",
		},

		// PiB (1024⁵+)
		{
			name:     "exactly 1 PiB",
			bytes:    1125899906842624, // 1024⁵
			expected: "1PiB",
		},
		{
			name:     "5 PiB",
			bytes:    5629499534213120, // 1024⁵ * 5
			expected: "5PiB",
		},
		{
			name:     "1000 PiB",
			bytes:    1125899906842624000, // 1024⁵ * 1000
			expected: "1000PiB",
		},

		// Rounding tests
		{
			name:     "rounds down KiB",
			bytes:    1024 + 256, // 1.25 KiB
			expected: "1KiB",
		},
		{
			name:     "rounds up KiB",
			bytes:    1024 + 512, // 1.5 KiB
			expected: "2KiB",
		},
		{
			name:     "rounds down MiB",
			bytes:    1048576 + 262144, // 1.25 MiB
			expected: "1MiB",
		},
		{
			name:     "rounds up MiB",
			bytes:    1048576 + 524288, // 1.5 MiB
			expected: "2MiB",
		},
		{
			name:     "rounds down GiB",
			bytes:    1073741824 + 268435456, // 1.25 GiB
			expected: "1GiB",
		},
		{
			name:     "rounds up GiB",
			bytes:    1073741824 + 536870912, // 1.5 GiB
			expected: "2GiB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result, "FormatBytes(%d) should return %s", tt.bytes, tt.expected)
		})
	}
}

// TestFormatBytes_Consistency verifies that the function produces consistent results
func TestFormatBytes_Consistency(t *testing.T) {
	testCases := []int64{0, 1, 1024, 1048576, 1073741824}

	for _, bytes := range testCases {
		result1 := FormatBytes(bytes)
		result2 := FormatBytes(bytes)
		assert.Equal(t, result1, result2, "FormatBytes should be deterministic for %d bytes", bytes)
	}
}

// TestFormatBytes_NoSpaces verifies that output has no spaces (as per requirement)
func TestFormatBytes_NoSpaces(t *testing.T) {
	testCases := []int64{0, 512, 1024, 1536, 1048576, 1073741824}

	for _, bytes := range testCases {
		result := FormatBytes(bytes)
		assert.NotContains(t, result, " ", "FormatBytes(%d) should not contain spaces, got: %s", bytes, result)
	}
}

// TestFormatBytes_UnitsArray verifies all unit suffixes are present in output
func TestFormatBytes_UnitsArray(t *testing.T) {
	tests := []struct {
		bytes        int64
		expectedUnit string
	}{
		{100, "B"},
		{1024, "KiB"},
		{1048576, "MiB"},
		{1073741824, "GiB"},
		{1099511627776, "TiB"},
		{1125899906842624, "PiB"},
	}

	for _, tt := range tests {
		result := FormatBytes(tt.bytes)
		assert.Contains(t, result, tt.expectedUnit, "FormatBytes(%d) should contain unit %s", tt.bytes, tt.expectedUnit)
	}
}
