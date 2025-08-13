package main

import (
	"context"
	"encoding/json"
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
			result := replaceAll(tt.content, tt.from, tt.to)
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

	err := processReplacement(ctx, srcFile, dstFile, "OLD_VALUE", "NEW_VALUE")
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

func TestRunBasicScenario(t *testing.T) {
	tempDir := t.TempDir()

	// Create source files
	srcDir := filepath.Join(tempDir, "pkg", "example")
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

	// Create config
	config := Config{
		Items: []Item{
			{
				Dir: "pkg/example",
				Replacements: []Replacement{
					{From: "v1.0.0", To: "v2.0.0", File: "main.go"},
					{From: "old-service", To: "new-service", File: "main.go"},
					{From: "production", To: "development", File: "main.go"},
					{From: "false", To: "true", File: "utils.go"},
					{From: "api.old-domain.com", To: "api.new-domain.com", File: "utils.go"},
				},
			},
		},
	}

	configPath := filepath.Join(tempDir, "overlaygen.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, configData, 0644))

	// Run overlaygen
	outputDir := filepath.Join(tempDir, "overlay-output")
	ctx := slogctx.NewCtx(context.Background(), slog.Default())

	err = run(ctx, configPath, outputDir)
	require.NoError(t, err)

	// Verify overlay.json exists and is valid
	overlayPath := filepath.Join(outputDir, "overlay.json")
	assert.FileExists(t, overlayPath)

	overlayData, err := os.ReadFile(overlayPath)
	require.NoError(t, err)

	var overlay OverlayFile
	require.NoError(t, json.Unmarshal(overlayData, &overlay))

	expectedReplacements := map[string]string{
		"pkg/example/main.go":  filepath.Join(outputDir, "pkg/example/main.go"),
		"pkg/example/utils.go": filepath.Join(outputDir, "pkg/example/utils.go"),
	}
	assert.Equal(t, expectedReplacements, overlay.Replace)

	// Verify main.go was modified correctly
	modifiedMain, err := os.ReadFile(filepath.Join(outputDir, "pkg/example/main.go"))
	require.NoError(t, err)

	expectedMain := `package example

const Version = "v2.0.0"
const ServiceName = "new-service"

func GetConfig() string {
	return "development"
}`
	assert.Equal(t, expectedMain, string(modifiedMain))

	// Verify utils.go was modified correctly
	modifiedUtils, err := os.ReadFile(filepath.Join(outputDir, "pkg/example/utils.go"))
	require.NoError(t, err)

	expectedUtils := `package example

func Debug() bool {
	return true
}

const API_URL = "https://api.new-domain.com"`
	assert.Equal(t, expectedUtils, string(modifiedUtils))
}

func TestRunMultipleItems(t *testing.T) {
	tempDir := t.TempDir()

	// Create first package
	pkg1Dir := filepath.Join(tempDir, "pkg", "service1")
	require.NoError(t, os.MkdirAll(pkg1Dir, 0755))

	service1Go := filepath.Join(pkg1Dir, "service.go")
	service1Content := `package service1

const Name = "service-one"
const Port = "8080"`
	require.NoError(t, os.WriteFile(service1Go, []byte(service1Content), 0644))

	// Create second package
	pkg2Dir := filepath.Join(tempDir, "pkg", "service2")
	require.NoError(t, os.MkdirAll(pkg2Dir, 0755))

	service2Go := filepath.Join(pkg2Dir, "service.go")
	service2Content := `package service2

const Name = "service-two"
const Port = "9090"`
	require.NoError(t, os.WriteFile(service2Go, []byte(service2Content), 0644))

	// Create config with multiple items
	config := Config{
		Items: []Item{
			{
				Dir: "pkg/service1",
				Replacements: []Replacement{
					{From: "service-one", To: "service-alpha", File: "service.go"},
					{From: "8080", To: "8081", File: "service.go"},
				},
			},
			{
				Dir: "pkg/service2",
				Replacements: []Replacement{
					{From: "service-two", To: "service-beta", File: "service.go"},
					{From: "9090", To: "9091", File: "service.go"},
				},
			},
		},
	}

	configPath := filepath.Join(tempDir, "overlaygen.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, configData, 0644))

	// Run overlaygen
	outputDir := filepath.Join(tempDir, "overlay-output")
	ctx := slogctx.NewCtx(context.Background(), slog.Default())

	err = run(ctx, configPath, outputDir)
	require.NoError(t, err)

	// Verify overlay.json
	overlayPath := filepath.Join(outputDir, "overlay.json")
	overlayData, err := os.ReadFile(overlayPath)
	require.NoError(t, err)

	var overlay OverlayFile
	require.NoError(t, json.Unmarshal(overlayData, &overlay))

	assert.Len(t, overlay.Replace, 2)
	assert.Contains(t, overlay.Replace, "pkg/service1/service.go")
	assert.Contains(t, overlay.Replace, "pkg/service2/service.go")

	// Verify first service modification
	modifiedService1, err := os.ReadFile(filepath.Join(outputDir, "pkg/service1/service.go"))
	require.NoError(t, err)

	expectedService1 := `package service1

const Name = "service-alpha"
const Port = "8081"`
	assert.Equal(t, expectedService1, string(modifiedService1))

	// Verify second service modification
	modifiedService2, err := os.ReadFile(filepath.Join(outputDir, "pkg/service2/service.go"))
	require.NoError(t, err)

	expectedService2 := `package service2

const Name = "service-beta"
const Port = "9091"`
	assert.Equal(t, expectedService2, string(modifiedService2))
}

func TestRunNestedDirectories(t *testing.T) {
	tempDir := t.TempDir()

	// Create nested structure
	nestedDir := filepath.Join(tempDir, "internal", "pkg", "deep", "nested")
	require.NoError(t, os.MkdirAll(nestedDir, 0755))

	configGo := filepath.Join(nestedDir, "config.go")
	configContent := `package nested

import "time"

const Timeout = 30 * time.Second
const RetryCount = 3`
	require.NoError(t, os.WriteFile(configGo, []byte(configContent), 0644))

	// Create config
	config := Config{
		Items: []Item{
			{
				Dir: "internal/pkg/deep/nested",
				Replacements: []Replacement{
					{From: "30", To: "60", File: "config.go"},
					{From: "RetryCount = 3", To: "RetryCount = 5", File: "config.go"},
				},
			},
		},
	}

	configPath := filepath.Join(tempDir, "overlaygen.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, configData, 0644))

	// Run overlaygen
	outputDir := filepath.Join(tempDir, "overlay-output")
	ctx := slogctx.NewCtx(context.Background(), slog.Default())

	err = run(ctx, configPath, outputDir)
	require.NoError(t, err)

	// Verify output
	modifiedConfig, err := os.ReadFile(filepath.Join(outputDir, "internal/pkg/deep/nested/config.go"))
	require.NoError(t, err)

	expectedConfig := `package nested

import "time"

const Timeout = 60 * time.Second
const RetryCount = 5`
	assert.Equal(t, expectedConfig, string(modifiedConfig))
}
