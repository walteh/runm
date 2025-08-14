package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gitlab.com/tozd/go/errors"

	slogctx "github.com/veqryn/slog-context"
)

// OverlayImport represents an import that should be overlaid
type OverlayImport struct {
	ImportPath  string
	Alias       string
	SourcePath  string // Path to the forked package
	ExportInits bool
}

// runSourceMainMode converts a main package to an importable package.
// Returns the absolute path to the generated overlay.json file.
//
// This mode:
// 1. Finds the package directory using go list
// 2. Copies all .go files to tempDir
// 3. Converts "package main" to "package {dir}_main"
// 4. Converts "func main()" to "func Main___main()"
// 5. Generates overlay.json mapping original files to modified copies
func runSourceMainMode(ctx context.Context, pkg, tempDir, appendTo string) (overlayPath string, err error) {
	logger := slogctx.FromCtx(ctx)

	os.MkdirAll(tempDir, 0755)

	sourceDirAbs, err := filepath.Abs(pkg)
	if err != nil {
		return "", errors.Errorf("getting absolute path for source dir: %w", err)
	}

	logger.InfoContext(ctx, "converting main package",
		slog.String("source_dir", sourceDirAbs),
		slog.String("temp_dir", tempDir))

	// Build config for main package conversion
	config := Config{
		Replacements: []Replacement{
			{
				From: "package main",
				To:   "package overlay_main",
				File: "*.go",
			},
			{
				From: "package main_test",
				To:   "package overlay_main_test",
				File: "*.go",
			},
			{
				From: "func main()",
				To:   "func Main___main()",
				File: "*.go",
			},
		},
	}

	// Check if there are any Go files with overlay imports in this directory
	dirsToProcess, err := findOverlayImportsInDir(ctx, sourceDirAbs)
	if err != nil {
		return "", errors.Errorf("finding overlay imports: %w", err)
	}

	if appendTo == "" {
		appendTo = filepath.Join(tempDir, "overlay.json")
	}

	// If no overlay imports found, just process the current directory
	if len(dirsToProcess) == 0 {
		// ensure appendTo exists
		_, _, err := initBlankOverlay(appendTo)
		if err != nil {
			return "", errors.Errorf("initializing blank overlay: %w", err)
		}
		return appendTo, nil
	}

	// Process each directory with overlay imports
	for _, imp := range dirsToProcess {
		config.AbsoluteSourceDir = imp.SourcePath
		if imp.ExportInits {
			config.Replacements = append(config.Replacements, Replacement{
				From: "func init()",
				To:   "func Init___{{.Index}}()",
				File: "*.go",
			})
		}
		isolatedTempDir := filepath.Join(tempDir, imp.ImportPath)
		appendTo, err = runConfigMode(ctx, config, isolatedTempDir, appendTo)
		if err != nil {
			return "", errors.Errorf("running config mode: %w", err)
		}
	}

	// Use unified config mode processing
	return appendTo, nil
}

// findOverlayImportsInDir scans a directory for Go files with overlay imports
func findOverlayImportsInDir(ctx context.Context, dir string) ([]OverlayImport, error) {
	var allImports []OverlayImport

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only process .go files in the root directory (no subdirs)
		if !strings.HasSuffix(path, ".go") || info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Skip subdirectories
		if strings.Contains(relPath, "/") {
			return nil
		}

		// Parse this file for overlay imports
		imports, err := parseOverlayImports(ctx, path)
		if err != nil {
			// Log warning but don't fail - file might not have overlay imports
			slogctx.FromCtx(ctx).WarnContext(ctx, "failed to parse overlay imports",
				slog.String("file", path),
				slog.Any("error", err))
			return nil
		}

		allImports = append(allImports, imports...)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return allImports, nil
}

// parseOverlayImports scans a Go source file for imports marked with //go:overlay
func parseOverlayImports(ctx context.Context, sourceFile string) ([]OverlayImport, error) {
	logger := slogctx.FromCtx(ctx)

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, sourceFile, nil, parser.ParseComments)
	if err != nil {
		return nil, errors.Errorf("parsing Go file: %w", err)
	}

	var overlayImports []OverlayImport

	// Look for imports with preceding //go:overlay comments
	for _, spec := range node.Imports {
		if spec.Path == nil {
			continue
		}

		// Check if there's a //go:overlay comment before this import
		hasOverlay, flags := hasOverlayComment(fset, spec, node.Comments)
		if hasOverlay {
			importPath := strings.Trim(spec.Path.Value, `"`)
			alias := ""
			if spec.Name != nil {
				alias = spec.Name.Name
			}

			// Resolve source path from import path using go list
			sourcePath, err := resolveSourcePath(importPath)
			if err != nil {
				logger.WarnContext(ctx, "could not resolve source path",
					slog.String("import", importPath),
					slog.Any("error", err))
				continue
			}

			overlayImports = append(overlayImports, OverlayImport{
				ImportPath:  importPath,
				Alias:       alias,
				SourcePath:  sourcePath,
				ExportInits: strings.Contains(flags, "-export-inits"),
			})

			logger.InfoContext(ctx, "found overlay import",
				slog.String("import", importPath),
				slog.String("alias", alias),
				slog.String("source", sourcePath))
		}
	}

	return overlayImports, nil
}

// hasOverlayComment checks if an import has a //go:overlay comment preceding it
func hasOverlayComment(fset *token.FileSet, spec *ast.ImportSpec, comments []*ast.CommentGroup) (bool, string) {
	specPos := fset.Position(spec.Pos())

	// Look for //go:overlay comment before this import
	for _, group := range comments {
		groupPos := fset.Position(group.Pos())

		// Comment should be on the line before the import or a few lines before
		if groupPos.Line >= specPos.Line-5 && groupPos.Line < specPos.Line {
			for _, comment := range group.List {
				if strings.Contains(comment.Text, "//go:overlay") {
					return true, strings.Split(comment.Text, "//go:overlay")[1]
				}
			}
		}
	}

	return false, ""
}

// resolveSourcePath uses go list to find the actual source directory for an import path
func resolveSourcePath(importPath string) (string, error) {
	// Use go list to find the actual source directory
	cmd := fmt.Sprintf("go list -f '{{.Dir}}' %s", importPath)
	output, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return "", errors.Errorf("running go list for %s: %w", importPath, err)
	}

	sourcePath := strings.TrimSpace(string(output))
	if sourcePath == "" {
		return "", errors.Errorf("empty source path from go list for %s", importPath)
	}

	return sourcePath, nil
}
