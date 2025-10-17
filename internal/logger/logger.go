package logger

import (
	"fmt"
	"io"
	"os"
)

// Logger handles operational logging to stderr, keeping stdout clean for data output
type Logger struct {
	writer io.Writer
	quiet  bool
	debug  bool
}

// New creates a new logger that writes to stderr
func New(quiet, debug bool) *Logger {
	return &Logger{
		writer: os.Stderr,
		quiet:  quiet,
		debug:  debug,
	}
}

// Infof logs an informational message
func (l *Logger) Infof(format string, args ...interface{}) {
	if !l.quiet {
		_, _ = fmt.Fprintf(l.writer, format+"\n", args...)
	}
}

// Successf logs a success message
func (l *Logger) Successf(format string, args ...interface{}) {
	if !l.quiet {
		_, _ = fmt.Fprintf(l.writer, "✓ "+format+"\n", args...)
	}
}

// Warningf logs a warning message
func (l *Logger) Warningf(format string, args ...interface{}) {
	if !l.quiet {
		_, _ = fmt.Fprintf(l.writer, "Warning: "+format+"\n", args...)
	}
}

// Errorf logs an error message (always shown, even in quiet mode)
func (l *Logger) Errorf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(l.writer, "Error: "+format+"\n", args...)
}

// Debug logs a debug message (only shown when debug mode is enabled)
func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.debug {
		_, _ = fmt.Fprintf(l.writer, "DEBUG: "+format+"\n", args...)
	}
}

// Println prints a blank line (for spacing)
func (l *Logger) Println() {
	if !l.quiet {
		_, _ = fmt.Fprintln(l.writer)
	}
}
