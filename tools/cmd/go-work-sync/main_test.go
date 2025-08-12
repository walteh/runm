package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
)

type testCase struct {
	name        string
	description string
	goWork      string
	modules     map[string]string // path -> go.mod content
	expected    map[string][]string // path -> expected strings in output
	shouldError bool
}

func TestGoWorkSync(t *testing.T) {
	tests := []testCase{
		{
			name:        "basic_sync",
			description: "Sync basic replace directives from go.work to nested modules",
			goWork: `go 1.25

use (
	.
	./module-a
	./module-b
)

replace github.com/example/dep1 => ./shared-deps/dep1
replace github.com/example/dep2 => ./shared-deps/dep2
`,
			modules: map[string]string{
				"go.mod": `module github.com/example/root
go 1.25
`,
				"module-a/go.mod": `module github.com/example/module-a
go 1.25

require (
	github.com/example/dep1 v1.0.0
	github.com/example/dep2 v1.0.0
)
`,
				"module-b/go.mod": `module github.com/example/module-b
go 1.25

require github.com/example/dep1 v1.0.0
`,
			},
			expected: map[string][]string{
				"go.mod": {
					"replace github.com/example/dep1 => shared-deps/dep1 // managed by go-work-sync",
					"replace github.com/example/dep2 => shared-deps/dep2 // managed by go-work-sync",
				},
				"module-a/go.mod": {
					"replace github.com/example/dep1 => ../shared-deps/dep1 // managed by go-work-sync",
					"replace github.com/example/dep2 => ../shared-deps/dep2 // managed by go-work-sync",
				},
				"module-b/go.mod": {
					"replace github.com/example/dep1 => ../shared-deps/dep1 // managed by go-work-sync",
					"replace github.com/example/dep2 => ../shared-deps/dep2 // managed by go-work-sync",
				},
			},
		},
		{
			name:        "nested_modules",
			description: "Handle modules at different nesting levels with correct relative paths",
			goWork: `go 1.25

use (
	./api
	./nested/service
)

replace github.com/company/shared => ./libs/shared
`,
			modules: map[string]string{
				"api/go.mod": `module github.com/company/api
go 1.25

require github.com/company/shared v1.0.0
`,
				"nested/service/go.mod": `module github.com/company/service
go 1.25

require github.com/company/shared v1.0.0
`,
			},
			expected: map[string][]string{
				"api/go.mod": {
					"replace github.com/company/shared => ../libs/shared // managed by go-work-sync",
				},
				"nested/service/go.mod": {
					"replace github.com/company/shared => ../../libs/shared // managed by go-work-sync",
				},
			},
		},
		{
			name:        "preserve_existing",
			description: "Preserve existing replace directives that aren't managed by go.work",
			goWork: `go 1.25

use (
	./app
)

replace github.com/example/managed => ./deps/managed
`,
			modules: map[string]string{
				"app/go.mod": `module github.com/example/app
go 1.25

replace (
	github.com/example/managed => ./old-path/managed
	github.com/example/unmanaged => /absolute/path/unmanaged
	github.com/example/local-unmanaged => ../some/local/path
	github.com/corporate/private => git.company.com/private v1.0.0
)
`,
			},
			expected: map[string][]string{
				"app/go.mod": {
					// Managed replace should be updated
					"replace github.com/example/managed => ../deps/managed // managed by go-work-sync",
					// Unmanaged replaces should be preserved
					"github.com/example/unmanaged => /absolute/path/unmanaged",
					"github.com/example/local-unmanaged => ../some/local/path",
					"github.com/corporate/private => git.company.com/private v1.0.0",
				},
			},
		},
		{
			name:        "no_changes_needed",
			description: "Skip updates when replace directives are already correct",
			goWork: `go 1.25

use (
	./service
)

replace github.com/example/lib => ./shared/lib
`,
			modules: map[string]string{
				"service/go.mod": `module github.com/example/service
go 1.25

replace github.com/example/lib => ../shared/lib // managed by go-work-sync
`,
			},
			expected: map[string][]string{
				"service/go.mod": {
					// Should remain unchanged
					"replace github.com/example/lib => ../shared/lib // managed by go-work-sync",
				},
			},
		},
		{
			name:        "multiple_replaces_individual_lines",
			description: "Generate individual replace lines with comments for multiple replaces",
			goWork: `go 1.25

use (
	./client
)

replace github.com/pkg/errors => ./forks/errors
replace github.com/sirupsen/logrus => ./forks/logrus
replace github.com/gorilla/mux => ./forks/mux
`,
			modules: map[string]string{
				"client/go.mod": `module github.com/example/client
go 1.25

require (
	github.com/pkg/errors v1.0.0
	github.com/sirupsen/logrus v1.0.0
	github.com/gorilla/mux v1.0.0
)
`,
			},
			expected: map[string][]string{
				"client/go.mod": {
					"replace github.com/gorilla/mux => ../forks/mux // managed by go-work-sync",
					"replace github.com/pkg/errors => ../forks/errors // managed by go-work-sync",
					"replace github.com/sirupsen/logrus => ../forks/logrus // managed by go-work-sync",
				},
			},
		},
		{
			name:        "absolute_paths",
			description: "Handle absolute paths in go.work replace directives",
			goWork: `go 1.25

use (
	./webapp
)

replace github.com/example/lib => /usr/local/lib/example
`,
			modules: map[string]string{
				"webapp/go.mod": `module github.com/example/webapp
go 1.25

require github.com/example/lib v1.0.0
`,
			},
			expected: map[string][]string{
				"webapp/go.mod": {
					"replace github.com/example/lib => /usr/local/lib/example // managed by go-work-sync",
				},
			},
		},
		{
			name:        "versioned_replaces",
			description: "Handle versioned replace directives (like gvisor case)",
			goWork: `go 1.25

use (
	./service
)

replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250807194038-c9af560a03d9
replace github.com/example/fork => ../forks/example
`,
			modules: map[string]string{
				"service/go.mod": `module github.com/example/service
go 1.25

require (
	gvisor.dev/gvisor v0.0.0
	github.com/example/fork v1.0.0
)
`,
			},
			expected: map[string][]string{
				"service/go.mod": {
					"replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250807194038-c9af560a03d9 // managed by go-work-sync",
					"replace github.com/example/fork => ../../forks/example // managed by go-work-sync",
				},
			},
		},
		{
			name:        "same_module_versioned_replace",
			description: "Module replaced with itself and version should not get relative path prefix",
			goWork: `go 1.25

use (
	./client
	./nested/service
)

replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250807194038-c9af560a03d9
replace some.example.com/module => some.example.com/module v1.2.3-special
`,
			modules: map[string]string{
				"client/go.mod": `module github.com/example/client
go 1.25

require (
	gvisor.dev/gvisor v0.0.0
	some.example.com/module v1.0.0
)
`,
				"nested/service/go.mod": `module github.com/example/service
go 1.25

require gvisor.dev/gvisor v0.0.0
`,
			},
			expected: map[string][]string{
				"client/go.mod": {
					"replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250807194038-c9af560a03d9 // managed by go-work-sync",
					"replace some.example.com/module => some.example.com/module v1.2.3-special // managed by go-work-sync",
				},
				"nested/service/go.mod": {
					"replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250807194038-c9af560a03d9 // managed by go-work-sync",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create temporary workspace
			tempDir := setupTestWorkspace(t, tc)
			defer os.RemoveAll(tempDir)

			// Change to temp directory
			originalDir, err := os.Getwd()
			require.NoError(t, err)
			defer func() { require.NoError(t, os.Chdir(originalDir)) }()
			require.NoError(t, os.Chdir(tempDir))

			// Run sync-replaces
			err = syncReplaces()
			if tc.shouldError {
				assert.Error(t, err, "Expected an error for test case: %s", tc.description)
				return
			}
			require.NoError(t, err, "syncReplaces failed for test case: %s", tc.description)

			// Verify results
			verifyTestResults(t, tempDir, tc)
		})
	}
}

func setupTestWorkspace(t *testing.T, tc testCase) string {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "go-work-sync-test-*")
	require.NoError(t, err, "Failed to create temp dir for test: %s", tc.name)

	t.Logf("Test %s workspace: %s", tc.name, tempDir)

	// Create go.work file
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.work"), []byte(tc.goWork), 0644))

	// Create all module directories and files
	for modPath, content := range tc.modules {
		fullPath := filepath.Join(tempDir, modPath)
		dir := filepath.Dir(fullPath)
		
		require.NoError(t, os.MkdirAll(dir, 0755), "Failed to create directory for %s", modPath)
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644), "Failed to write %s", modPath)
	}

	// Create any dependency directories referenced in go.work
	// This is a simple heuristic based on the test cases
	depPaths := []string{
		"shared-deps/dep1", "shared-deps/dep2",
		"libs/shared",
		"deps/managed",
		"shared/lib",
		"forks/errors", "forks/logrus", "forks/mux", "forks/example",
	}
	
	for _, depPath := range depPaths {
		depDir := filepath.Join(tempDir, depPath)
		if err := os.MkdirAll(depDir, 0755); err == nil {
			// Create a simple go.mod for the dependency
			modContent := "module " + strings.ReplaceAll(depPath, "/", "/") + "\ngo 1.25\n"
			os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(modContent), 0644)
		}
	}

	return tempDir
}

func verifyTestResults(t *testing.T, tempDir string, tc testCase) {
	t.Helper()

	for modPath, expectedStrings := range tc.expected {
		fullPath := filepath.Join(tempDir, modPath)
		
		content, err := os.ReadFile(fullPath)
		require.NoError(t, err, "Failed to read %s", modPath)
		
		contentStr := string(content)
		t.Logf("\n--- %s content ---\n%s", modPath, contentStr)

		for _, expected := range expectedStrings {
			assert.Contains(t, contentStr, expected, 
				"Expected string not found in %s:\n  Expected: %q\n  Content:\n%s", 
				modPath, expected, contentStr)
		}
	}
}

func TestParsingWithNativeModfile(t *testing.T) {
	tests := []struct {
		name        string
		workContent string
		modContent  string
		expectError bool
	}{
		{
			name: "valid_go_work",
			workContent: `go 1.25

toolchain go1.25rc3

use (
	.
	./tools
	./exp/k8s
)

replace github.com/example/dep => ../dep
replace github.com/another/dep => ../another
`,
			expectError: false,
		},
		{
			name: "valid_go_mod",
			modContent: `module github.com/example/test

go 1.25

require github.com/example/dep v1.0.0

replace (
	github.com/example/dep => ../dep
	github.com/another/dep => /absolute/path
)

replace github.com/single/line => ../single
`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "parsing-test-*")
			require.NoError(t, err)
			defer os.RemoveAll(tempDir)

			if tt.workContent != "" {
				// Test go.work parsing
				workPath := filepath.Join(tempDir, "go.work")
				require.NoError(t, os.WriteFile(workPath, []byte(tt.workContent), 0644))

				workData, err := os.ReadFile(workPath)
				require.NoError(t, err)

				workFile, err := parseGoWork(workPath, workData)
				if tt.expectError {
					assert.Error(t, err)
					return
				}
				require.NoError(t, err)
				
				// Verify we can parse the structure
				assert.NotNil(t, workFile)
				t.Logf("Parsed %d use directives and %d replace directives", 
					len(workFile.Use), len(workFile.Replace))
			}

			if tt.modContent != "" {
				// Test go.mod parsing
				modPath := filepath.Join(tempDir, "go.mod")
				require.NoError(t, os.WriteFile(modPath, []byte(tt.modContent), 0644))

				modData, err := os.ReadFile(modPath)
				require.NoError(t, err)

				modFile, err := parseGoMod(modPath, modData)
				if tt.expectError {
					assert.Error(t, err)
					return
				}
				require.NoError(t, err)

				// Verify we can parse the structure
				assert.NotNil(t, modFile)
				t.Logf("Parsed module: %s with %d replace directives", 
					modFile.Module.Mod.Path, len(modFile.Replace))
			}
		})
	}
}

// Helper functions for the parsing test
func parseGoWork(path string, data []byte) (*modfile.WorkFile, error) {
	return modfile.ParseWork(path, data, nil)
}

func parseGoMod(path string, data []byte) (*modfile.File, error) {
	return modfile.Parse(path, data, nil)
}

func TestFindGoWork(t *testing.T) {
	tests := []struct {
		name         string
		setupDirs    []string    // directories to create
		workLocation string      // where to place go.work (relative to temp root)
		startDir     string      // directory to start search from (relative to temp root)
		expectError  bool
	}{
		{
			name:         "go_work_in_current_dir",
			setupDirs:    []string{"."},
			workLocation: "go.work",
			startDir:     "",
			expectError:  false,
		},
		{
			name:         "go_work_in_parent",
			setupDirs:    []string{".", "subdir"},
			workLocation: "go.work",
			startDir:     "subdir",
			expectError:  false,
		},
		{
			name:         "no_go_work_found",
			setupDirs:    []string{".", "subdir"},
			workLocation: "", // don't create go.work
			startDir:     "subdir",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "find-work-test-*")
			require.NoError(t, err)
			defer os.RemoveAll(tempDir)

			// Create directory structure
			for _, dir := range tt.setupDirs {
				fullDir := filepath.Join(tempDir, dir)
				require.NoError(t, os.MkdirAll(fullDir, 0755))
			}

			// Create go.work if specified
			if tt.workLocation != "" {
				workPath := filepath.Join(tempDir, tt.workLocation)
				workContent := "go 1.25\n\nuse (\n\t.\n)\n"
				require.NoError(t, os.WriteFile(workPath, []byte(workContent), 0644))
			}

			// Change to start directory
			originalDir, err := os.Getwd()
			require.NoError(t, err)
			defer func() { require.NoError(t, os.Chdir(originalDir)) }()

			startPath := filepath.Join(tempDir, tt.startDir)
			require.NoError(t, os.Chdir(startPath))

			// Test findGoWork
			workPath, err := findGoWork()
			if tt.expectError {
				assert.Error(t, err)
				assert.Empty(t, workPath)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, workPath)
				assert.True(t, fileExists(workPath))
			}
		})
	}
}