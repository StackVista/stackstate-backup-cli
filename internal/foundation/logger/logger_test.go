package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name  string
		quiet bool
		debug bool
	}{
		{
			name:  "normal mode",
			quiet: false,
			debug: false,
		},
		{
			name:  "quiet mode",
			quiet: true,
			debug: false,
		},
		{
			name:  "debug mode",
			quiet: false,
			debug: true,
		},
		{
			name:  "quiet and debug mode",
			quiet: true,
			debug: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.quiet, tt.debug)
			assert.NotNil(t, logger)
			assert.Equal(t, tt.quiet, logger.quiet)
			assert.Equal(t, tt.debug, logger.debug)
			assert.NotNil(t, logger.writer)
		})
	}
}

func TestLogger_Infof(t *testing.T) {
	tests := []struct {
		name           string
		quiet          bool
		message        string
		args           []interface{}
		expectedOutput string
		shouldOutput   bool
	}{
		{
			name:           "info message in normal mode",
			quiet:          false,
			message:        "Processing %s",
			args:           []interface{}{"test"},
			expectedOutput: "Processing test\n",
			shouldOutput:   true,
		},
		{
			name:           "info message in quiet mode",
			quiet:          true,
			message:        "Processing %s",
			args:           []interface{}{"test"},
			expectedOutput: "",
			shouldOutput:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := &Logger{
				writer: buf,
				quiet:  tt.quiet,
			}

			logger.Infof(tt.message, tt.args...)

			if tt.shouldOutput {
				assert.Equal(t, tt.expectedOutput, buf.String())
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestLogger_Successf(t *testing.T) {
	tests := []struct {
		name           string
		quiet          bool
		message        string
		args           []interface{}
		shouldOutput   bool
		containsSymbol bool
	}{
		{
			name:           "success message in normal mode",
			quiet:          false,
			message:        "Completed %s",
			args:           []interface{}{"task"},
			shouldOutput:   true,
			containsSymbol: true,
		},
		{
			name:           "success message in quiet mode",
			quiet:          true,
			message:        "Completed %s",
			args:           []interface{}{"task"},
			shouldOutput:   false,
			containsSymbol: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := &Logger{
				writer: buf,
				quiet:  tt.quiet,
			}

			logger.Successf(tt.message, tt.args...)

			if tt.shouldOutput {
				output := buf.String()
				assert.Contains(t, output, "Completed task")
				if tt.containsSymbol {
					assert.Contains(t, output, "✓")
				}
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

//nolint:dupl // Test functions are intentionally similar for consistency
func TestLogger_Warningf(t *testing.T) {
	tests := []struct {
		name         string
		quiet        bool
		message      string
		args         []interface{}
		shouldOutput bool
	}{
		{
			name:         "warning in normal mode",
			quiet:        false,
			message:      "Deprecated %s",
			args:         []interface{}{"feature"},
			shouldOutput: true,
		},
		{
			name:         "warning in quiet mode",
			quiet:        true,
			message:      "Deprecated %s",
			args:         []interface{}{"feature"},
			shouldOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := &Logger{
				writer: buf,
				quiet:  tt.quiet,
			}

			logger.Warningf(tt.message, tt.args...)

			if tt.shouldOutput {
				output := buf.String()
				assert.Contains(t, output, "Warning:")
				assert.Contains(t, output, "Deprecated feature")
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestLogger_Errorf(t *testing.T) {
	tests := []struct {
		name    string
		quiet   bool
		message string
		args    []interface{}
	}{
		{
			name:    "error in normal mode",
			quiet:   false,
			message: "Failed to %s",
			args:    []interface{}{"connect"},
		},
		{
			name:    "error in quiet mode (still outputs)",
			quiet:   true,
			message: "Failed to %s",
			args:    []interface{}{"connect"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := &Logger{
				writer: buf,
				quiet:  tt.quiet,
			}

			logger.Errorf(tt.message, tt.args...)

			// Errors always output, regardless of quiet mode
			output := buf.String()
			assert.Contains(t, output, "Error:")
			assert.Contains(t, output, "Failed to connect")
		})
	}
}

//nolint:dupl // Test functions are intentionally similar for consistency
func TestLogger_Debugf(t *testing.T) {
	tests := []struct {
		name         string
		debug        bool
		message      string
		args         []interface{}
		shouldOutput bool
	}{
		{
			name:         "debug message with debug enabled",
			debug:        true,
			message:      "Debug info: %s",
			args:         []interface{}{"details"},
			shouldOutput: true,
		},
		{
			name:         "debug message with debug disabled",
			debug:        false,
			message:      "Debug info: %s",
			args:         []interface{}{"details"},
			shouldOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := &Logger{
				writer: buf,
				debug:  tt.debug,
			}

			logger.Debugf(tt.message, tt.args...)

			if tt.shouldOutput {
				output := buf.String()
				assert.Contains(t, output, "DEBUG:")
				assert.Contains(t, output, "Debug info: details")
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestLogger_Println(t *testing.T) {
	tests := []struct {
		name         string
		quiet        bool
		shouldOutput bool
	}{
		{
			name:         "blank line in normal mode",
			quiet:        false,
			shouldOutput: true,
		},
		{
			name:         "blank line in quiet mode",
			quiet:        true,
			shouldOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := &Logger{
				writer: buf,
				quiet:  tt.quiet,
			}

			logger.Println()

			if tt.shouldOutput {
				assert.Equal(t, "\n", buf.String())
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestLogger_MultipleCalls(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		writer: buf,
		quiet:  false,
		debug:  true,
	}

	logger.Infof("Starting process")
	logger.Debugf("Debug details")
	logger.Successf("Process completed")
	logger.Warningf("Cleanup recommended")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Len(t, lines, 4)

	assert.Contains(t, output, "Starting process")
	assert.Contains(t, output, "DEBUG: Debug details")
	assert.Contains(t, output, "✓ Process completed")
	assert.Contains(t, output, "Warning: Cleanup recommended")
}
