package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gitlab.com/tozd/go/errors"

	slogctx "github.com/veqryn/slog-context"
)

// Config represents the JSON configuration for overlay generation
type Config struct {
	AbsoluteSourceDir string        `json:"absolute_source_dir"`
	Replacements      []Replacement `json:"replacements"`
}

// take any replacement with a glob and expand it to all files that match the glob
func (c *Config) ExpandGlobs(ctx context.Context) ([]Replacement, error) {
	expandedReplacements := make([]Replacement, 0)
	for _, replacement := range c.Replacements {
		if !strings.Contains(replacement.File, "*") {
			expandedReplacements = append(expandedReplacements, replacement)
			continue
		}

		files, err := filepath.Glob(filepath.Join(c.AbsoluteSourceDir, replacement.File))
		if err != nil {
			return nil, errors.Errorf("resolving glob %s: %w", replacement.File, err)
		}

		for _, file := range files {
			relPath, err := filepath.Rel(c.AbsoluteSourceDir, file)
			if err != nil {
				return nil, errors.Errorf("getting relative path for %s: %w", file, err)
			}
			expandedReplacements = append(expandedReplacements, Replacement{
				From: replacement.From,
				To:   replacement.To,
				File: relPath,
			})
		}
	}

	return expandedReplacements, nil
}

// Replacement represents a single string replacement in a file
type Replacement struct {
	From string `json:"from"`
	To   string `json:"to"`
	File string `json:"file"`
}

// OverlayFile represents the structure of overlay.json for go build
type OverlayFile struct {
	Replace map[string]string `json:"replace"`
}

// runConfigMode processes a JSON configuration file to generate overlay files.
// Returns the absolute path to the generated overlay.json file.
//
// The config.json should contain:
//
//	{
//	  "items": [
//	    {
//	      "dir": "relative/path/to/source",
//	      "replacements": [
//	        {
//	          "from": "old string",
//	          "to": "new string",
//	          "file": "filename.go"
//	        }
//	      ]
//	    }
//	  ]
//	}
func runConfigModeFromFile(ctx context.Context, configPath, tempDir, appendTo string) (string, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return "", errors.Errorf("reading config file %s: %w", configPath, err)
	}

	var config Config
	if err := json.Unmarshal(configData, &config); err != nil {
		return "", errors.Errorf("parsing config JSON: %w", err)
	}

	if config.AbsoluteSourceDir == "" {
		config.AbsoluteSourceDir = filepath.Dir(configPath)
	}

	return runConfigMode(ctx, config, tempDir, appendTo)
}

func initBlankOverlay(overlayPath string) (string, *OverlayFile, error) {

	if _, err := os.Stat(overlayPath); err == nil {
		overlay := OverlayFile{}
		overlayData, err := os.ReadFile(overlayPath)
		if err != nil {
			return "", nil, errors.Errorf("reading overlay file %s: %w", overlayPath, err)
		}
		if err := json.Unmarshal(overlayData, &overlay); err != nil {
			return "", nil, errors.Errorf("unmarshaling overlay JSON: %w", err)
		}
		return overlayPath, &overlay, nil
	}

	overlay := OverlayFile{
		Replace: make(map[string]string),
	}

	overlayData, err := json.MarshalIndent(overlay, "", "\t")
	if err != nil {
		return "", nil, errors.Errorf("marshaling overlay JSON: %w", err)
	}

	if err := os.WriteFile(overlayPath, overlayData, 0644); err != nil {
		return "", nil, errors.Errorf("writing overlay file %s: %w", overlayPath, err)
	}

	return overlayPath, &overlay, nil
}

func runConfigMode(ctx context.Context, config Config, tempDir, appendTo string) (string, error) {
	logger := slogctx.FromCtx(ctx)

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", errors.Errorf("creating temp directory %s: %w", tempDir, err)
	}

	if appendTo == "" {
		appendTo = filepath.Join(tempDir, "overlay.json")
	}

	overlayPath, overlay, err := initBlankOverlay(appendTo)
	if err != nil {
		return "", errors.Errorf("initializing blank overlay: %w", err)
	}

	replacements, err := config.ExpandGlobs(ctx)
	if err != nil {
		return "", errors.Errorf("expanding globs: %w", err)
	}

	// Group replacements by file to handle multiple replacements per file
	fileReplacements := make(map[string][]Replacement)
	for _, replacement := range replacements {
		fileReplacements[replacement.File] = append(fileReplacements[replacement.File], replacement)
	}

	for fileName, replacements := range fileReplacements {
		logger.InfoContext(ctx, "processing file",
			slog.String("file", fileName),
			slog.Int("replacements", len(replacements)))

		// Source path is relative to the config source directory
		srcPath := filepath.Join(config.AbsoluteSourceDir, fileName)
		dstPath := filepath.Join(tempDir, fileName)

		// Ensure destination directory exists
		dstDir := filepath.Dir(dstPath)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return "", errors.Errorf("creating destination directory %s: %w", dstDir, err)
		}

		logger.InfoContext(ctx, "processing file with multiple replacements",
			slog.String("file", fileName),
			slog.String("src", srcPath),
			slog.String("dst", dstPath),
			slog.Int("replacement_count", len(replacements)))

		if err := processMultipleReplacements(ctx, srcPath, dstPath, replacements); err != nil {
			return "", errors.Errorf("processing replacements in %s: %w", fileName, err)
		}

		overlay.Replace[srcPath] = dstPath
	}

	overlayData, err := json.MarshalIndent(overlay, "", "\t")
	if err != nil {
		return "", errors.Errorf("marshaling overlay JSON: %w", err)
	}

	if err := os.WriteFile(overlayPath, overlayData, 0644); err != nil {
		return "", errors.Errorf("writing overlay file %s: %w", overlayPath, err)
	}

	logger.InfoContext(ctx, "overlay generated successfully",
		slog.String("overlay_path", overlayPath),
		slog.Int("bytes", len(overlayData)),
		slog.Int("replacements", len(overlay.Replace)))

	// Return absolute path
	absPath, err := filepath.Abs(overlayPath)
	if err != nil {
		return "", errors.Errorf("getting absolute path: %w", err)
	}

	return absPath, nil
}

// processMultipleReplacements applies multiple replacements to a single file in sequence
func processMultipleReplacements(ctx context.Context, srcPath, dstPath string, replacements []Replacement) error {
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		return errors.Errorf("reading source file %s: %w", srcPath, err)
	}

	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return errors.Errorf("creating destination directory %s: %w", dstDir, err)
	}

	content := string(srcData)

	slices.SortFunc(replacements, func(a, b Replacement) int {
		return strings.Compare(a.File, b.File)
	})

	replaceCounts := make(map[string]int)

	// Apply all replacements sequentially to the same content
	for _, replacement := range replacements {
		slogctx.FromCtx(ctx).InfoContext(ctx, "applying replacement",
			slog.String("from", replacement.From),
			slog.String("to", replacement.To),
			slog.String("file", replacement.File))

		content = replaceAll(content, replacement.From, replacement.To, replaceCounts)
	}

	if err := os.WriteFile(dstPath, []byte(content), 0644); err != nil {
		return errors.Errorf("writing destination file %s: %w", dstPath, err)
	}

	slogctx.FromCtx(ctx).InfoContext(ctx, "file processed",
		slog.String("src", srcPath),
		slog.String("dst", dstPath),
		slog.Int("original_bytes", len(srcData)),
		slog.Int("modified_bytes", len(content)),
		slog.Int("replacements_applied", len(replacements)))

	return nil
}

// processReplacements applies multiple replacements to a single file
func processReplacement(ctx context.Context, srcPath, dstPath string, replacement Replacement, replaceCounts map[string]int) error {
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		return errors.Errorf("reading source file %s: %w", srcPath, err)
	}

	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return errors.Errorf("creating destination directory %s: %w", dstDir, err)
	}

	content := string(srcData)

	// Apply all replacements sequentially
	slogctx.FromCtx(ctx).InfoContext(ctx, "applying replacement",
		slog.String("from", replacement.From),
		slog.String("to", replacement.To),
		slog.String("file", replacement.File))

	content = replaceAll(content, replacement.From, replacement.To, replaceCounts)

	if err := os.WriteFile(dstPath, []byte(content), 0644); err != nil {
		return errors.Errorf("writing destination file %s: %w", dstPath, err)
	}

	slogctx.FromCtx(ctx).InfoContext(ctx, "file processed",
		slog.String("src", srcPath),
		slog.String("dst", dstPath),
		slog.Int("original_bytes", len(srcData)),
		slog.Int("modified_bytes", len(content)),
		slog.Int("replacements_applied", 1))

	return nil
}

// replaceAll performs all occurrences of a string replacement
func replaceAll(content, from, to string, replaceCounts map[string]int) string {
	result := content
	for {
		newResult := replaceFirst(result, from, to, replaceCounts)
		if newResult == result {
			break
		}
		result = newResult
	}
	return result
}

// replaceFirst performs the first occurrence of a string replacement
func replaceFirst(content, from, to string, replaceCounts map[string]int) string {
	if from == "" {
		return content
	}

	idx := findString(content, from)
	if idx == -1 {
		return content
	}

	replaceCounts[from]++

	if strings.Contains(to, "{{.Index}}") {
		to = strings.ReplaceAll(to, "{{.Index}}", strconv.Itoa(replaceCounts[from]))
	}

	return content[:idx] + to + content[idx+len(from):]
}

// findString finds the first occurrence of a substring
func findString(content, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(content) < len(substr) {
		return -1
	}

	for i := 0; i <= len(content)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if content[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
