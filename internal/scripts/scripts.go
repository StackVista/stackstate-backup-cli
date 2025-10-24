// Package scripts provides embedded backup/restore scripts that are compiled into the binary.
// These scripts are used by Kubernetes Jobs for backup and restore operations.
package scripts

import (
	"embed"
	"fmt"
	"io/fs"
)

// Embed all scripts from the scripts directory at the root of the project
// Note: embed paths are relative to the source file, but we can only go down, not up
// So we need to embed from a different approach - embed the actual files
//
//go:embed all:scripts
var embeddedScripts embed.FS

// GetScript retrieves an embedded script by filename
func GetScript(filename string) ([]byte, error) {
	path := fmt.Sprintf("scripts/%s", filename)
	data, err := embeddedScripts.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded script %s: %w", filename, err)
	}
	return data, nil
}

// ListScripts returns a list of all embedded script filenames
func ListScripts() ([]string, error) {
	entries, err := embeddedScripts.ReadDir("scripts")
	if err != nil {
		return nil, fmt.Errorf("failed to list embedded scripts: %w", err)
	}

	var scripts []string
	for _, entry := range entries {
		if !entry.IsDir() {
			scripts = append(scripts, entry.Name())
		}
	}
	return scripts, nil
}

// GetScriptsFS returns the embedded filesystem containing all scripts
// This can be used to access scripts without extracting them individually
func GetScriptsFS() (fs.FS, error) {
	return fs.Sub(embeddedScripts, "scripts")
}
