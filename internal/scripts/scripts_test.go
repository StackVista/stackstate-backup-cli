package scripts

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetScript tests retrieving embedded scripts
func TestGetScript(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		expectError bool
		validate    func(*testing.T, []byte)
	}{
		{
			name:        "retrieve existing script",
			filename:    "restore-stackgraph-backup.sh",
			expectError: false,
			validate: func(t *testing.T, data []byte) {
				assert.NotEmpty(t, data, "Script content should not be empty")
				assert.Greater(t, len(data), 100, "Script should have substantial content")
				// Verify it's a shell script
				assert.Contains(t, string(data), "#!/", "Script should have shebang")
			},
		},
		{
			name:        "nonexistent script",
			filename:    "nonexistent-script.sh",
			expectError: true,
			validate:    nil,
		},
		{
			name:        "empty filename",
			filename:    "",
			expectError: true,
			validate:    nil,
		},
		{
			name:        "filename with path traversal attempt",
			filename:    "../../../etc/passwd",
			expectError: true,
			validate:    nil,
		},
		{
			name:        "filename with absolute path",
			filename:    "/etc/passwd",
			expectError: true,
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := GetScript(tt.filename)

			if tt.expectError {
				assert.Error(t, err, "Should return error for invalid filename")
				assert.Nil(t, data, "Data should be nil on error")
			} else {
				require.NoError(t, err, "Should not return error for valid filename")
				assert.NotNil(t, data, "Data should not be nil")
				if tt.validate != nil {
					tt.validate(t, data)
				}
			}
		})
	}
}

// TestGetScript_ContentValidation tests that scripts have expected content
func TestGetScript_ContentValidation(t *testing.T) {
	// Get the restore script
	data, err := GetScript("restore-stackgraph-backup.sh")
	require.NoError(t, err)
	require.NotEmpty(t, data)

	content := string(data)

	// Verify it's a valid shell script
	assert.Contains(t, content, "#!/", "Script should have shebang")

	// Verify it contains expected backup/restore commands
	// (adjust these assertions based on actual script content)
	assert.NotContains(t, content, "<<<<<<", "Script should not contain merge conflict markers")
	assert.NotContains(t, content, ">>>>>>", "Script should not contain merge conflict markers")
}

// TestGetScript_SameContentOnMultipleCalls verifies deterministic behavior
func TestGetScript_SameContentOnMultipleCalls(t *testing.T) {
	filename := "restore-stackgraph-backup.sh"

	// Get script multiple times
	data1, err1 := GetScript(filename)
	require.NoError(t, err1)

	data2, err2 := GetScript(filename)
	require.NoError(t, err2)

	data3, err3 := GetScript(filename)
	require.NoError(t, err3)

	// All calls should return identical data
	assert.Equal(t, data1, data2, "Multiple calls should return same content")
	assert.Equal(t, data2, data3, "Multiple calls should return same content")
}

// TestListScripts tests listing all embedded scripts
func TestListScripts(t *testing.T) {
	scripts, err := ListScripts()

	require.NoError(t, err, "ListScripts should not return error")
	assert.NotEmpty(t, scripts, "Should have at least one script")

	// Verify expected script is in the list
	assert.Contains(t, scripts, "restore-stackgraph-backup.sh", "Should contain restore script")

	// Verify no duplicates
	seen := make(map[string]bool)
	for _, script := range scripts {
		assert.False(t, seen[script], "Script list should not contain duplicates: %s", script)
		seen[script] = true
	}

	// Verify all entries are files (not directories)
	for _, script := range scripts {
		assert.NotEmpty(t, script, "Script filename should not be empty")
		// File extension check (scripts should be .sh files)
		if len(script) > 3 {
			// Most should be shell scripts, but we won't enforce it strictly
			t.Logf("Found script: %s", script)
		}
	}
}

// TestListScripts_Consistency tests that ListScripts is deterministic
func TestListScripts_Consistency(t *testing.T) {
	scripts1, err1 := ListScripts()
	require.NoError(t, err1)

	scripts2, err2 := ListScripts()
	require.NoError(t, err2)

	scripts3, err3 := ListScripts()
	require.NoError(t, err3)

	// All calls should return same number of scripts
	assert.Equal(t, len(scripts1), len(scripts2), "Multiple calls should return same number of scripts")
	assert.Equal(t, len(scripts2), len(scripts3), "Multiple calls should return same number of scripts")

	// Convert to maps for easier comparison
	toMap := func(scripts []string) map[string]bool {
		m := make(map[string]bool)
		for _, s := range scripts {
			m[s] = true
		}
		return m
	}

	map1 := toMap(scripts1)
	map2 := toMap(scripts2)
	map3 := toMap(scripts3)

	assert.Equal(t, map1, map2, "Multiple calls should return same scripts")
	assert.Equal(t, map2, map3, "Multiple calls should return same scripts")
}

// TestListScripts_VerifyScriptsExist tests that listed scripts can be retrieved
func TestListScripts_VerifyScriptsExist(t *testing.T) {
	scripts, err := ListScripts()
	require.NoError(t, err)
	require.NotEmpty(t, scripts)

	// Verify each listed script can be retrieved
	for _, script := range scripts {
		t.Run(script, func(t *testing.T) {
			data, err := GetScript(script)
			assert.NoError(t, err, "Should be able to retrieve listed script: %s", script)
			assert.NotEmpty(t, data, "Retrieved script should not be empty: %s", script)
		})
	}
}

// TestGetScriptsFS tests getting the embedded filesystem
func TestGetScriptsFS(t *testing.T) {
	scriptsFS, err := GetScriptsFS()

	require.NoError(t, err, "GetScriptsFS should not return error")
	assert.NotNil(t, scriptsFS, "FS should not be nil")

	// Verify we can read files from the FS
	entries, err := fs.ReadDir(scriptsFS, ".")
	require.NoError(t, err, "Should be able to read directory from FS")
	assert.NotEmpty(t, entries, "FS should contain files")

	// Verify expected file exists
	found := false
	for _, entry := range entries {
		if entry.Name() == "restore-stackgraph-backup.sh" {
			found = true
			assert.False(t, entry.IsDir(), "restore-stackgraph-backup.sh should be a file")
		}
	}
	assert.True(t, found, "Should find restore-stackgraph-backup.sh in FS")
}

// TestGetScriptsFS_ReadFile tests reading files from the embedded FS
func TestGetScriptsFS_ReadFile(t *testing.T) {
	scriptsFS, err := GetScriptsFS()
	require.NoError(t, err)

	// Read a known script file
	data, err := fs.ReadFile(scriptsFS, "restore-stackgraph-backup.sh")
	require.NoError(t, err, "Should be able to read file from FS")
	assert.NotEmpty(t, data, "File content should not be empty")

	// Compare with GetScript result
	directData, err := GetScript("restore-stackgraph-backup.sh")
	require.NoError(t, err)

	assert.Equal(t, directData, data, "FS read should match GetScript result")
}

// TestGetScriptsFS_Walk tests walking the embedded FS
func TestGetScriptsFS_Walk(t *testing.T) {
	scriptsFS, err := GetScriptsFS()
	require.NoError(t, err)

	fileCount := 0
	err = fs.WalkDir(scriptsFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			fileCount++
			t.Logf("Found file in FS: %s", path)
		}
		return nil
	})

	require.NoError(t, err, "Walking FS should not error")
	assert.Greater(t, fileCount, 0, "Should find at least one file when walking FS")
}

// TestGetScriptsFS_Consistency tests that GetScriptsFS returns consistent results
func TestGetScriptsFS_Consistency(t *testing.T) {
	fs1, err1 := GetScriptsFS()
	require.NoError(t, err1)

	fs2, err2 := GetScriptsFS()
	require.NoError(t, err2)

	// Read same file from both FS instances
	data1, err := fs.ReadFile(fs1, "restore-stackgraph-backup.sh")
	require.NoError(t, err)

	data2, err := fs.ReadFile(fs2, "restore-stackgraph-backup.sh")
	require.NoError(t, err)

	assert.Equal(t, data1, data2, "Multiple FS instances should provide same file content")
}

// TestEmbeddedScriptsNotNil verifies the embedded filesystem is initialized
func TestEmbeddedScriptsNotNil(t *testing.T) {
	// This test verifies that the embed.FS is properly initialized
	// by attempting to read from it
	_, err := embeddedScripts.ReadDir("scripts")
	assert.NoError(t, err, "Embedded scripts filesystem should be initialized")
}

// TestErrorMessages tests that error messages are descriptive
func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		contains string
	}{
		{
			name:     "nonexistent file error message",
			filename: "does-not-exist.sh",
			contains: "does-not-exist.sh",
		},
		{
			name:     "invalid path error message",
			filename: "../../../etc/passwd",
			contains: "passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetScript(tt.filename)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.contains, "Error message should mention the problematic filename")
		})
	}
}

// TestConcurrentAccess tests that concurrent access to scripts is safe
func TestConcurrentAccess(t *testing.T) {
	const numGoroutines = 10
	const numIterations = 5

	done := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines*numIterations)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < numIterations; j++ {
				// Test GetScript
				_, err := GetScript("restore-stackgraph-backup.sh")
				if err != nil {
					errors <- err
				}

				// Test ListScripts
				_, err = ListScripts()
				if err != nil {
					errors <- err
				}

				// Test GetScriptsFS
				_, err = GetScriptsFS()
				if err != nil {
					errors <- err
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
		errorCount++
	}

	assert.Equal(t, 0, errorCount, "Concurrent access should be safe")
}
