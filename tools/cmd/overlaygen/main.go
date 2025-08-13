package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	slogctx "github.com/veqryn/slog-context"
	"gitlab.com/tozd/go/errors"
)

type Config struct {
	Items []Item `json:"items"`
}

type Item struct {
	Dir          string        `json:"dir"`
	Replacements []Replacement `json:"replacements"`
}

type Replacement struct {
	From string `json:"from"`
	To   string `json:"to"`
	File string `json:"file"`
}

type OverlayFile struct {
	Replace map[string]string `json:"replace"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <overlaygen.json> <output-dir>\n", os.Args[0])
		os.Exit(1)
	}

	configPath := os.Args[1]
	outputDir := os.Args[2]

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx = slogctx.NewCtx(ctx, logger)

	if err := run(ctx, configPath, outputDir); err != nil {
		slog.ErrorContext(ctx, "overlaygen failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath, outputDir string) error {
	logger := slogctx.FromCtx(ctx)

	configData, err := os.ReadFile(configPath)
	if err != nil {
		return errors.Errorf("reading config file %s: %w", configPath, err)
	}

	var config Config
	if err := json.Unmarshal(configData, &config); err != nil {
		return errors.Errorf("parsing config JSON: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return errors.Errorf("creating output directory %s: %w", outputDir, err)
	}

	// Get the directory containing the config file to use as base for resolving relative paths
	configDir := filepath.Dir(configPath)

	overlay := OverlayFile{
		Replace: make(map[string]string),
	}

	for _, item := range config.Items {
		logger.InfoContext(ctx, "processing item", slog.String("dir", item.Dir))

		itemDir := filepath.Join(outputDir, item.Dir)
		if err := os.MkdirAll(itemDir, 0755); err != nil {
			return errors.Errorf("creating item directory %s: %w", itemDir, err)
		}

		// Group replacements by file to handle multiple replacements in the same file
		fileReplacements := make(map[string][]Replacement)
		for _, replacement := range item.Replacements {
			fileReplacements[replacement.File] = append(fileReplacements[replacement.File], replacement)
		}

		for fileName, replacements := range fileReplacements {
			// Source path is relative to the config file directory
			srcPath := filepath.Join(configDir, item.Dir, fileName)
			dstPath := filepath.Join(itemDir, fileName)
			// Path for overlay.json should be relative, not absolute
			overlayKey := filepath.Join(item.Dir, fileName)

			logger.InfoContext(ctx, "processing file",
				slog.String("file", fileName),
				slog.Int("replacements", len(replacements)),
				slog.String("src", srcPath),
				slog.String("dst", dstPath))

			if err := processReplacements(ctx, srcPath, dstPath, replacements); err != nil {
				return errors.Errorf("processing replacements in %s: %w", fileName, err)
			}

			overlay.Replace[overlayKey] = dstPath
		}
	}

	overlayPath := filepath.Join(outputDir, "overlay.json")
	overlayData, err := json.MarshalIndent(overlay, "", "  ")
	if err != nil {
		return errors.Errorf("marshaling overlay JSON: %w", err)
	}

	if err := os.WriteFile(overlayPath, overlayData, 0644); err != nil {
		return errors.Errorf("writing overlay file %s: %w", overlayPath, err)
	}

	logger.InfoContext(ctx, "overlay generated successfully",
		slog.String("overlay_path", overlayPath),
		slog.Int("replacements", len(overlay.Replace)))

	return nil
}

func processReplacements(ctx context.Context, srcPath, dstPath string, replacements []Replacement) error {
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
	for _, replacement := range replacements {
		slogctx.FromCtx(ctx).InfoContext(ctx, "applying replacement",
			slog.String("from", replacement.From),
			slog.String("to", replacement.To),
			slog.String("file", replacement.File))
		
		content = replaceAll(content, replacement.From, replacement.To)
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

func processReplacement(ctx context.Context, srcPath, dstPath, from, to string) error {
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		return errors.Errorf("reading source file %s: %w", srcPath, err)
	}

	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return errors.Errorf("creating destination directory %s: %w", dstDir, err)
	}

	content := string(srcData)
	modified := replaceAll(content, from, to)

	if err := os.WriteFile(dstPath, []byte(modified), 0644); err != nil {
		return errors.Errorf("writing destination file %s: %w", dstPath, err)
	}

	slogctx.FromCtx(ctx).InfoContext(ctx, "file processed",
		slog.String("src", srcPath),
		slog.String("dst", dstPath),
		slog.Int("original_bytes", len(srcData)),
		slog.Int("modified_bytes", len(modified)))

	return nil
}

func replaceAll(content, from, to string) string {
	result := content
	for {
		newResult := replaceFirst(result, from, to)
		if newResult == result {
			break
		}
		result = newResult
	}
	return result
}

func replaceFirst(content, from, to string) string {
	if from == "" {
		return content
	}

	idx := findString(content, from)
	if idx == -1 {
		return content
	}

	return content[:idx] + to + content[idx+len(from):]
}

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
