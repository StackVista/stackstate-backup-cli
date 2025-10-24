package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFormatter(t *testing.T) {
	tests := []struct {
		name           string
		format         string
		expectedFormat Format
	}{
		{
			name:           "table format",
			format:         "table",
			expectedFormat: FormatTable,
		},
		{
			name:           "json format",
			format:         "json",
			expectedFormat: FormatJSON,
		},
		{
			name:           "invalid format defaults to table",
			format:         "invalid",
			expectedFormat: FormatTable,
		},
		{
			name:           "empty format defaults to table",
			format:         "",
			expectedFormat: FormatTable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatter := NewFormatter(tt.format)
			assert.NotNil(t, formatter)
			assert.Equal(t, tt.expectedFormat, formatter.format)
			assert.NotNil(t, formatter.writer)
		})
	}
}

func TestFormatter_PrintTable_TableFormat(t *testing.T) {
	tests := []struct {
		name             string
		table            Table
		expectedContains []string
	}{
		{
			name: "table with data",
			table: Table{
				Headers: []string{"NAME", "STATUS", "COUNT"},
				Rows: [][]string{
					{"snapshot-1", "SUCCESS", "100"},
					{"snapshot-2", "PARTIAL", "50"},
				},
			},
			expectedContains: []string{"NAME", "STATUS", "COUNT", "snapshot-1", "SUCCESS", "100", "snapshot-2", "PARTIAL", "50"},
		},
		{
			name: "table with single row",
			table: Table{
				Headers: []string{"ID", "VALUE"},
				Rows: [][]string{
					{"1", "test"},
				},
			},
			expectedContains: []string{"ID", "VALUE", "1", "test"},
		},
		{
			name: "empty table",
			table: Table{
				Headers: []string{"NAME", "STATUS"},
				Rows:    [][]string{},
			},
			expectedContains: []string{"No data found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			formatter := &Formatter{
				writer: buf,
				format: FormatTable,
			}

			err := formatter.PrintTable(tt.table)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedContains {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatter_PrintTable_JSONFormat(t *testing.T) {
	tests := []struct {
		name          string
		table         Table
		expectedLen   int
		validateFirst map[string]string
	}{
		{
			name: "json with data",
			table: Table{
				Headers: []string{"NAME", "STATUS"},
				Rows: [][]string{
					{"snapshot-1", "SUCCESS"},
					{"snapshot-2", "PARTIAL"},
				},
			},
			expectedLen: 2,
			validateFirst: map[string]string{
				"NAME":   "snapshot-1",
				"STATUS": "SUCCESS",
			},
		},
		{
			name: "json with single row",
			table: Table{
				Headers: []string{"ID", "VALUE"},
				Rows: [][]string{
					{"1", "test"},
				},
			},
			expectedLen: 1,
			validateFirst: map[string]string{
				"ID":    "1",
				"VALUE": "test",
			},
		},
		{
			name: "empty json",
			table: Table{
				Headers: []string{"NAME"},
				Rows:    [][]string{},
			},
			expectedLen:   0,
			validateFirst: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			formatter := &Formatter{
				writer: buf,
				format: FormatJSON,
			}

			err := formatter.PrintTable(tt.table)
			require.NoError(t, err)

			var result []map[string]string
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err)

			assert.Len(t, result, tt.expectedLen)

			if tt.validateFirst != nil && len(result) > 0 {
				for key, expectedValue := range tt.validateFirst {
					assert.Equal(t, expectedValue, result[0][key])
				}
			}
		})
	}
}

func TestFormatter_PrintMessage(t *testing.T) {
	tests := []struct {
		name         string
		format       Format
		message      string
		shouldOutput bool
	}{
		{
			name:         "message in table format",
			format:       FormatTable,
			message:      "Operation completed successfully",
			shouldOutput: true,
		},
		{
			name:         "message in json format (ignored)",
			format:       FormatJSON,
			message:      "Operation completed successfully",
			shouldOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			formatter := &Formatter{
				writer: buf,
				format: tt.format,
			}

			formatter.PrintMessage(tt.message)

			if tt.shouldOutput {
				assert.Contains(t, buf.String(), tt.message)
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestFormatter_PrintError(t *testing.T) {
	tests := []struct {
		name         string
		format       Format
		err          error
		shouldOutput bool
	}{
		{
			name:         "error in table format",
			format:       FormatTable,
			err:          assert.AnError,
			shouldOutput: true,
		},
		{
			name:         "error in json format (ignored)",
			format:       FormatJSON,
			err:          assert.AnError,
			shouldOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			formatter := &Formatter{
				writer: buf,
				format: tt.format,
			}

			formatter.PrintError(tt.err)

			if tt.shouldOutput {
				output := buf.String()
				assert.Contains(t, output, "Errorf:")
				assert.Contains(t, output, tt.err.Error())
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestTableToMaps(t *testing.T) {
	tests := []struct {
		name     string
		table    Table
		expected []map[string]string
	}{
		{
			name: "standard table",
			table: Table{
				Headers: []string{"NAME", "STATUS"},
				Rows: [][]string{
					{"test1", "active"},
					{"test2", "inactive"},
				},
			},
			expected: []map[string]string{
				{"NAME": "test1", "STATUS": "active"},
				{"NAME": "test2", "STATUS": "inactive"},
			},
		},
		{
			name: "table with mismatched row length",
			table: Table{
				Headers: []string{"NAME", "STATUS", "COUNT"},
				Rows: [][]string{
					{"test1", "active"},
					{"test2", "inactive", "5"},
				},
			},
			expected: []map[string]string{
				{"NAME": "test1", "STATUS": "active"},
				{"NAME": "test2", "STATUS": "inactive", "COUNT": "5"},
			},
		},
		{
			name: "empty table",
			table: Table{
				Headers: []string{"NAME"},
				Rows:    [][]string{},
			},
			expected: []map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tableToMaps(tt.table)
			assert.Equal(t, len(tt.expected), len(result))

			for i, expectedMap := range tt.expected {
				assert.Equal(t, expectedMap, result[i])
			}
		})
	}
}

func TestFormatter_TableAlignment(t *testing.T) {
	buf := &bytes.Buffer{}
	formatter := &Formatter{
		writer: buf,
		format: FormatTable,
	}

	table := Table{
		Headers: []string{"SHORT", "VERY_LONG_HEADER", "MED"},
		Rows: [][]string{
			{"a", "value1", "x"},
			{"bb", "value2", "yy"},
			{"ccc", "value3", "zzz"},
		},
	}

	err := formatter.PrintTable(table)
	require.NoError(t, err)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have header + 3 data rows
	assert.Len(t, lines, 4)

	// Check that columns are aligned (each line should have proper spacing)
	for _, line := range lines {
		assert.NotEmpty(t, line)
		// Should contain multiple columns separated by spaces
		parts := strings.Fields(line)
		assert.GreaterOrEqual(t, len(parts), 3)
	}
}

func TestFormatter_JSONIndentation(t *testing.T) {
	buf := &bytes.Buffer{}
	formatter := &Formatter{
		writer: buf,
		format: FormatJSON,
	}

	table := Table{
		Headers: []string{"NAME", "VALUE"},
		Rows: [][]string{
			{"test", "data"},
		},
	}

	err := formatter.PrintTable(table)
	require.NoError(t, err)

	output := buf.String()

	// JSON should be indented (contains newlines and spaces)
	assert.Contains(t, output, "\n")
	assert.Contains(t, output, "  ")

	// Should be valid JSON
	var result []map[string]string
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
}
