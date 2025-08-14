package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	slogctx "github.com/veqryn/slog-context"
)

func TestReplaceAll(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		from     string
		to       string
		expected string
	}{
		{
			name:     "simple replacement",
			content:  "hello world",
			from:     "world",
			to:       "golang",
			expected: "hello golang",
		},
		{
			name:     "multiple replacements",
			content:  "foo bar foo baz foo",
			from:     "foo",
			to:       "qux",
			expected: "qux bar qux baz qux",
		},
		{
			name:     "no matches",
			content:  "hello world",
			from:     "xyz",
			to:       "abc",
			expected: "hello world",
		},
		{
			name:     "empty from string",
			content:  "hello world",
			from:     "",
			to:       "abc",
			expected: "hello world",
		},
		{
			name:     "overlapping matches",
			content:  "aaa",
			from:     "aa",
			to:       "b",
			expected: "ba",
		},
		{
			name:     "multiline replacement",
			content:  "line1\nold_value\nline3",
			from:     "old_value",
			to:       "new_value",
			expected: "line1\nnew_value\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceAll(tt.content, tt.from, tt.to, make(map[string]int))
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindString(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		substr   string
		expected int
	}{
		{
			name:     "found at beginning",
			content:  "hello world",
			substr:   "hello",
			expected: 0,
		},
		{
			name:     "found in middle",
			content:  "hello world",
			substr:   "lo wo",
			expected: 3,
		},
		{
			name:     "found at end",
			content:  "hello world",
			substr:   "world",
			expected: 6,
		},
		{
			name:     "not found",
			content:  "hello world",
			substr:   "xyz",
			expected: -1,
		},
		{
			name:     "empty substring",
			content:  "hello world",
			substr:   "",
			expected: 0,
		},
		{
			name:     "substring longer than content",
			content:  "hi",
			substr:   "hello",
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findString(tt.content, tt.substr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProcessReplacement(t *testing.T) {
	tempDir := t.TempDir()

	// Create source file
	srcDir := filepath.Join(tempDir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	srcFile := filepath.Join(srcDir, "test.go")
	srcContent := `package main

import "fmt"

func main() {
	fmt.Println("OLD_VALUE")
}`
	require.NoError(t, os.WriteFile(srcFile, []byte(srcContent), 0644))

	// Process replacement
	dstFile := filepath.Join(tempDir, "dst", "test.go")
	ctx := slogctx.NewCtx(context.Background(), slog.Default())

	replacement := Replacement{
		From: "OLD_VALUE",
		To:   "NEW_VALUE",
		File: "test.go",
	}

	err := processReplacement(ctx, srcFile, dstFile, replacement, make(map[string]int))
	require.NoError(t, err)

	// Verify output
	dstContent, err := os.ReadFile(dstFile)
	require.NoError(t, err)

	expectedContent := `package main

import "fmt"

func main() {
	fmt.Println("NEW_VALUE")
}`
	assert.Equal(t, expectedContent, string(dstContent))
}

func TestConfigModeWithReplacements(t *testing.T) {
	tempDir := t.TempDir()

	// Create source files
	srcDir := filepath.Join(tempDir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))

	mainGo := filepath.Join(srcDir, "main.go")
	mainContent := `package example

const Version = "v1.0.0"
const ServiceName = "old-service"

func GetConfig() string {
	return "production"
}`
	require.NoError(t, os.WriteFile(mainGo, []byte(mainContent), 0644))

	utilsGo := filepath.Join(srcDir, "utils.go")
	utilsContent := `package example

func Debug() bool {
	return false
}

const API_URL = "https://api.old-domain.com"`
	require.NoError(t, os.WriteFile(utilsGo, []byte(utilsContent), 0644))

	// Create config with new structure
	config := Config{
		AbsoluteSourceDir: srcDir,
		Replacements: []Replacement{
			{From: "v1.0.0", To: "v2.0.0", File: "main.go"},
			{From: "old-service", To: "new-service", File: "main.go"},
			{From: "production", To: "development", File: "main.go"},
			{From: "false", To: "true", File: "utils.go"},
			{From: "api.old-domain.com", To: "api.new-domain.com", File: "utils.go"},
		},
	}

	// Run config mode directly
	outputDir := filepath.Join(tempDir, "overlay-output")
	ctx := slogctx.NewCtx(context.Background(), slog.Default())

	overlayPath, err := runConfigMode(ctx, config, outputDir, "")
	require.NoError(t, err)

	// Verify overlay.json exists and is valid
	assert.FileExists(t, overlayPath)

	overlayData, err := os.ReadFile(overlayPath)
	require.NoError(t, err)

	var overlay OverlayFile
	require.NoError(t, json.Unmarshal(overlayData, &overlay))

	// Verify the mappings exist
	assert.Len(t, overlay.Replace, 2)

	// Verify main.go was modified correctly
	mainOverlayFile := ""
	utilsOverlayFile := ""
	for src, dst := range overlay.Replace {
		if filepath.Base(src) == "main.go" {
			mainOverlayFile = dst
		} else if filepath.Base(src) == "utils.go" {
			utilsOverlayFile = dst
		}
	}

	require.NotEmpty(t, mainOverlayFile, "main.go overlay mapping not found")
	require.NotEmpty(t, utilsOverlayFile, "utils.go overlay mapping not found")

	modifiedMain, err := os.ReadFile(mainOverlayFile)
	require.NoError(t, err)

	expectedMain := `package example

const Version = "v2.0.0"
const ServiceName = "new-service"

func GetConfig() string {
	return "development"
}`
	assert.Equal(t, expectedMain, string(modifiedMain))

	// Verify utils.go was modified correctly
	modifiedUtils, err := os.ReadFile(utilsOverlayFile)
	require.NoError(t, err)

	expectedUtils := `package example

func Debug() bool {
	return true
}

const API_URL = "https://api.new-domain.com"`
	assert.Equal(t, expectedUtils, string(modifiedUtils))
}

func TestSourceMainMode(t *testing.T) {
	tempDir := t.TempDir()

	// Create a simple main package without overlay imports
	srcDir := filepath.Join(tempDir, "testpkg")
	require.NoError(t, os.MkdirAll(srcDir, 0755))

	// Create main.go with main function
	mainGo := filepath.Join(srcDir, "main.go")
	mainContent := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
	process()
}

func process() {
	fmt.Println("Processing...")
}`
	require.NoError(t, os.WriteFile(mainGo, []byte(mainContent), 0644))

	// Create another file to test package conversion
	helperGo := filepath.Join(srcDir, "helper.go")
	helperContent := `package main

import "fmt"

func helper() {
	fmt.Println("Helper function")
}`
	require.NoError(t, os.WriteFile(helperGo, []byte(helperContent), 0644))

	// Create a test file (should be converted too)
	testGo := filepath.Join(srcDir, "main_test.go")
	testContent := `package main_test

import "testing"

func TestSomething(t *testing.T) {
	// Test code here
}`
	require.NoError(t, os.WriteFile(testGo, []byte(testContent), 0644))

	// Create a simple .go file without overlay imports to avoid parsing issues
	simpleGo := filepath.Join(srcDir, "simple.go")
	simpleContent := `// Simple Go file without imports or overlay comments
package main

const Message = "test"`
	require.NoError(t, os.WriteFile(simpleGo, []byte(simpleContent), 0644))

	// Run source main mode
	outputDir := filepath.Join(tempDir, "overlay-output")
	ctx := slogctx.NewCtx(context.Background(), slog.Default())

	overlayPath, err := runSourceMainMode(ctx, srcDir, outputDir, "")
	require.NoError(t, err)

	// Verify overlay.json was created
	assert.FileExists(t, overlayPath)

	// Read and verify overlay.json
	overlayData, err := os.ReadFile(overlayPath)
	require.NoError(t, err)

	var overlay OverlayFile
	require.NoError(t, json.Unmarshal(overlayData, &overlay))

	// Should have overlay mappings for the .go files
	assert.NotEmpty(t, overlay.Replace)

	// Find the overlay files
	var mainOverlayPath, helperOverlayPath, testOverlayPath, simpleOverlayPath string
	for original, overlayFile := range overlay.Replace {
		switch filepath.Base(original) {
		case "main.go":
			mainOverlayPath = overlayFile
		case "helper.go":
			helperOverlayPath = overlayFile
		case "main_test.go":
			testOverlayPath = overlayFile
		case "simple.go":
			simpleOverlayPath = overlayFile
		}
	}

	// Verify main.go was converted
	require.NotEmpty(t, mainOverlayPath, "main.go overlay not found")
	modifiedMain, err := os.ReadFile(mainOverlayPath)
	require.NoError(t, err)

	expectedMain := `package overlay_main

import "fmt"

func Main___main() {
	fmt.Println("Hello, World!")
	process()
}

func process() {
	fmt.Println("Processing...")
}`
	assert.Equal(t, expectedMain, string(modifiedMain))

	// Verify helper.go was converted
	require.NotEmpty(t, helperOverlayPath, "helper.go overlay not found")
	modifiedHelper, err := os.ReadFile(helperOverlayPath)
	require.NoError(t, err)

	expectedHelper := `package overlay_main

import "fmt"

func helper() {
	fmt.Println("Helper function")
}`
	assert.Equal(t, expectedHelper, string(modifiedHelper))

	// Verify test file was converted
	require.NotEmpty(t, testOverlayPath, "main_test.go overlay not found")
	modifiedTest, err := os.ReadFile(testOverlayPath)
	require.NoError(t, err)

	expectedTest := `package overlay_main_test

import "testing"

func TestSomething(t *testing.T) {
	// Test code here
}`
	assert.Equal(t, expectedTest, string(modifiedTest))

	// Verify simple.go was converted
	require.NotEmpty(t, simpleOverlayPath, "simple.go overlay not found")
	modifiedSimple, err := os.ReadFile(simpleOverlayPath)
	require.NoError(t, err)

	expectedSimple := `// Simple Go file without imports or overlay comments
package overlay_main

const Message = "test"`
	assert.Equal(t, expectedSimple, string(modifiedSimple))
}

func TestConfigGlobExpansion(t *testing.T) {
	tempDir := t.TempDir()

	// Create source files
	srcDir := filepath.Join(tempDir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))

	// Create multiple .go files
	for i, name := range []string{"file1.go", "file2.go", "file3.go"} {
		content := fmt.Sprintf(`package test

const Value%d = "old_value_%d"`, i+1, i+1)
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0644))
	}

	// Create a non-Go file (should be ignored by glob)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "readme.txt"), []byte("not a go file"), 0644))

	// Test glob expansion
	config := Config{
		AbsoluteSourceDir: srcDir,
		Replacements: []Replacement{
			{From: "old_value", To: "new_value", File: "*.go"}, // Should match all .go files
		},
	}

	ctx := slogctx.NewCtx(context.Background(), slog.Default())
	expandedReplacements, err := config.ExpandGlobs(ctx)
	require.NoError(t, err)

	// Should have 3 replacements (one for each .go file)
	assert.Len(t, expandedReplacements, 3)

	// Verify all .go files are included
	fileSet := make(map[string]bool)
	for _, replacement := range expandedReplacements {
		fileSet[replacement.File] = true
		assert.Equal(t, "old_value", replacement.From)
		assert.Equal(t, "new_value", replacement.To)
	}

	assert.True(t, fileSet["file1.go"])
	assert.True(t, fileSet["file2.go"])
	assert.True(t, fileSet["file3.go"])
	assert.False(t, fileSet["readme.txt"]) // Should not be included
}

func TestConfigFromFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create source file
	srcDir := filepath.Join(tempDir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))

	srcFile := filepath.Join(srcDir, "test.go")
	srcContent := `package main

const Value = "original"`
	require.NoError(t, os.WriteFile(srcFile, []byte(srcContent), 0644))

	// Create config file
	config := Config{
		Replacements: []Replacement{
			{From: "original", To: "modified", File: "test.go"},
		},
	}

	configPath := filepath.Join(srcDir, "config.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, configData, 0644))

	// Run config mode from file
	outputDir := filepath.Join(tempDir, "output")
	ctx := slogctx.NewCtx(context.Background(), slog.Default())

	overlayPath, err := runConfigModeFromFile(ctx, configPath, outputDir, "")
	require.NoError(t, err)

	// Verify the file was processed
	overlayData, err := os.ReadFile(overlayPath)
	require.NoError(t, err)

	var overlay OverlayFile
	require.NoError(t, json.Unmarshal(overlayData, &overlay))

	assert.Len(t, overlay.Replace, 1)

	// Find the modified file
	var modifiedFilePath string
	for _, dst := range overlay.Replace {
		modifiedFilePath = dst
		break
	}

	require.NotEmpty(t, modifiedFilePath)
	modifiedContent, err := os.ReadFile(modifiedFilePath)
	require.NoError(t, err)

	expectedContent := `package main

const Value = "modified"`
	assert.Equal(t, expectedContent, string(modifiedContent))
}
