package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/mod/modfile"
)

var (
	quiet        bool
	skipReplaces bool
)

func init() {
	flag.BoolVar(&quiet, "quiet", false, "don't print anything")
	flag.BoolVar(&skipReplaces, "skip-replaces", false, "skip updating replace directives")
	flag.Parse()
}

func logDebug(format string, a ...any) {
	if !quiet {
		fmt.Fprintf(os.Stdout, format, a...)
	}
}

func logError(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
}

type replaceInfo struct {
	path    string
	version string
}

func main() {
	if !skipReplaces {
		if err := syncReplaces(); err != nil {
			logError("Error: %v\n", err)
			os.Exit(1)
		}
	}
}

func syncReplaces() error {
	// Find go.work file
	workPath, err := findGoWork()
	if err != nil {
		return fmt.Errorf("finding go.work: %w", err)
	}

	logDebug("Found go.work at: %s\n", workPath)

	// Parse go.work file
	workData, err := os.ReadFile(workPath)
	if err != nil {
		return fmt.Errorf("reading go.work: %w", err)
	}

	workFile, err := modfile.ParseWork(workPath, workData, nil)
	if err != nil {
		return fmt.Errorf("parsing go.work: %w", err)
	}

	if len(workFile.Replace) == 0 {
		logDebug("No replace directives found in go.work")
		return nil
	}

	logDebug("Found %d replace directives in go.work\n", len(workFile.Replace))
	for _, r := range workFile.Replace {
		logDebug("  %s => %s\n", r.Old.Path, r.New.Path)
	}

	// Process each module in the workspace
	workDir := filepath.Dir(workPath)
	for _, use := range workFile.Use {
		modPath := filepath.Join(workDir, use.Path, "go.mod")
		if !fileExists(modPath) {
			logDebug("Skipping %s (no go.mod found)\n", use.Path)
			continue
		}

		logDebug("\nProcessing module: %s\n", use.Path)
		if err := syncModReplaces(modPath, workFile.Replace, workDir, use.Path); err != nil {
			return fmt.Errorf("syncing replaces for %s: %w", use.Path, err)
		}
	}

	return nil
}

func syncModReplaces(modPath string, workReplaces []*modfile.Replace, workDir, relModDir string) error {
	// Parse current go.mod
	modData, err := os.ReadFile(modPath)
	if err != nil {
		return fmt.Errorf("reading go.mod: %w", err)
	}

	modFile, err := modfile.Parse(modPath, modData, nil)
	if err != nil {
		return fmt.Errorf("parsing go.mod: %w", err)
	}

	// Check if replaces need updating and build new replace map
	modDir := filepath.Dir(modPath)
	newReplaces := make(map[string]replaceInfo) // module path -> replace info

	for _, workReplace := range workReplaces {
		var newPath string

		// Check if this is a same-module replace (module => module vX.Y.Z)
		// In this case, we should keep the module path as-is, not treat it as a file path
		if workReplace.Old.Path == workReplace.New.Path && workReplace.New.Version != "" {
			// This is a versioned replace of the same module (like gvisor => gvisor v1.2.3)
			newPath = workReplace.New.Path
		} else if filepath.IsAbs(workReplace.New.Path) {
			// This is an absolute filesystem path
			newPath = workReplace.New.Path
		} else {
			// This is a relative path to a local directory
			// Calculate relative path from module to the replace target
			workReplacePath := filepath.Join(workDir, workReplace.New.Path)
			relPath, err := filepath.Rel(modDir, workReplacePath)
			if err != nil {
				return fmt.Errorf("calculating relative path: %w", err)
			}
			newPath = relPath
		}

		newReplaces[workReplace.Old.Path] = replaceInfo{
			path:    newPath,
			version: workReplace.New.Version,
		}
	}

	logDebug("  Updating %d replace directives\n", len(newReplaces))
	return updateGoMod(modPath, modFile, newReplaces)
}

func updateGoMod(modPath string, modFile *modfile.File, newReplaces map[string]replaceInfo) error {
	// Only remove replace directives that are being replaced by go.work
	// Keep all others (absolute paths, version-specific, unmanaged local paths)
	toRemove := make([]*modfile.Replace, 0)
	for _, r := range modFile.Replace {
		if _, shouldReplace := newReplaces[r.Old.Path]; shouldReplace {
			toRemove = append(toRemove, r)
		}
	}

	// Remove old replaces that are being updated
	for _, r := range toRemove {
		if err := modFile.DropReplace(r.Old.Path, r.Old.Version); err != nil {
			return fmt.Errorf("dropping replace %s: %w", r.Old.Path, err)
		}
	}

	// clean up duplicate replaces
	modFile.Cleanup()

	// Add new replaces (sorted for consistency)
	modules := make([]string, 0, len(newReplaces))
	for module := range newReplaces {
		modules = append(modules, module)
	}
	sort.Strings(modules)

	for _, module := range modules {
		replaceInfo := newReplaces[module]
		if err := modFile.AddReplace(module, "", replaceInfo.path, replaceInfo.version); err != nil {
			return fmt.Errorf("adding replace %s => %s %s: %w", module, replaceInfo.path, replaceInfo.version, err)
		}
		rep := modFile.Replace[len(modFile.Replace)-1]
		rep.Syntax.Suffix = append(rep.Syntax.Suffix, modfile.Comment{
			Token: "// managed by go-work-sync",
		})
	}

	// Format and write the updated file
	modFile.Cleanup()
	newContent, err := modFile.Format()
	if err != nil {
		return fmt.Errorf("formatting go.mod: %w", err)
	}

	return os.WriteFile(modPath, newContent, 0644)
}

func findGoWork() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Look for go.work in current directory and up
	for {
		workPath := filepath.Join(wd, "go.work")
		if fileExists(workPath) {
			return workPath, nil
		}

		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}

	return "", fmt.Errorf("go.work not found")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
