package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// Format represents supported output formats
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"

	// tabwriterPadding is the padding between columns in table output
	tabwriterPadding = 2
)

// Formatter handles output formatting for list commands
type Formatter struct {
	writer io.Writer
	format Format
}

// NewFormatter creates a new output formatter
// Defaults to table format if invalid format provided
func NewFormatter(format string) *Formatter {
	f := Format(format)
	if f != FormatTable && f != FormatJSON {
		f = FormatTable
	}
	return &Formatter{
		writer: os.Stdout,
		format: f,
	}
}

// Table represents a table with headers and rows
type Table struct {
	Headers []string
	Rows    [][]string
}

// PrintTable prints data in the configured format (table or json)
func (f *Formatter) PrintTable(table Table) error {
	if len(table.Rows) == 0 {
		if f.format == FormatTable {
			fmt.Fprintln(f.writer, "No data found")
		} else {
			// For JSON, output empty array
			return f.printJSON([]map[string]string{})
		}
		return nil
	}

	switch f.format {
	case FormatJSON:
		return f.printJSON(tableToMaps(table))
	case FormatTable:
		return f.printTable(table)
	default:
		return f.printTable(table)
	}
}

// printTable prints data in table format using tabwriter
func (f *Formatter) printTable(table Table) error {
	w := tabwriter.NewWriter(f.writer, 0, 0, tabwriterPadding, ' ', 0)

	// Print header
	fmt.Fprintln(w, strings.Join(table.Headers, "\t"))

	// Print rows
	for _, row := range table.Rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	return w.Flush()
}

// printJSON prints data in JSON format
func (f *Formatter) printJSON(data interface{}) error {
	encoder := json.NewEncoder(f.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// tableToMaps converts a Table to a slice of maps for JSON output
func tableToMaps(table Table) []map[string]string {
	result := make([]map[string]string, 0, len(table.Rows))
	for _, row := range table.Rows {
		item := make(map[string]string)
		for i, header := range table.Headers {
			if i < len(row) {
				item[header] = row[i]
			}
		}
		result = append(result, item)
	}
	return result
}

// PrintMessage prints a simple message (only in table format, ignored in JSON)
func (f *Formatter) PrintMessage(message string) {
	if f.format == FormatTable {
		fmt.Fprintln(f.writer, message)
	}
}

// PrintError prints an error message (only in table format, ignored in JSON)
func (f *Formatter) PrintError(err error) {
	if f.format == FormatTable {
		fmt.Fprintf(f.writer, "Errorf: %v\n", err)
	}
}
