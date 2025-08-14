package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/tozd/go/errors"
)

type Config struct {
	tag            string
	typeFilter     string
	rootDir        string
	includeDotDirs bool
}

func main() {
	var config Config

	// Define command line flags
	flag.StringVar(&config.tag, "tag", "//go:mock", "Tag to search for in comments (e.g., //go:mock, //go:generate)")
	flag.StringVar(&config.typeFilter, "type", "", "Filter by specific type name (empty means all types)")
	flag.BoolVar(&config.includeDotDirs, "include-dot-dirs", false, "Include dot directories (e.g., .git, .idea, .vscode)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] <root_directory>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s /path/to/project\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --tag='//go:generate' /path/to/project\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --type=MyInterface /path/to/project\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --tag='//go:embed' --type=Config /path/to/project\n", os.Args[0])
	}
	flag.Parse()

	// Check if root directory is provided
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	config.rootDir = flag.Arg(0)

	err := filepath.Walk(config.rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !config.includeDotDirs && (strings.HasPrefix(path, ".") || strings.Contains(path, "/.")) {
			return nil
		}

		// Only process .go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		return processFile(path, config)
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "walking directory: %v\n", err)
		os.Exit(1)
	}
}

func processFile(filePath string, config Config) error {
	file, err := os.Open(filePath)
	if err != nil {
		return errors.Errorf("opening file %s: %w", filePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lines := make([]string, 0)

	// Read all lines
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return errors.Errorf("scanning file %s: %w", filePath, err)
	}

	// Look for the specified tag in comments
	for i, line := range lines {
		if strings.Contains(line, config.tag) {
			if config.typeFilter != "" {
				// Find the next type declaration
				typeName, typeType := findNextTypeDeclaration(lines, i+1)
				if typeName != "" {
					// Apply type filter if specified
					if !strings.Contains(typeType, config.typeFilter) {
						continue
					}

					// Format output like the shell script
					dir := filepath.Dir(filePath)
					if strings.HasPrefix(dir, config.rootDir) {
						dir = strings.TrimPrefix(dir, config.rootDir)
						dir = strings.TrimPrefix(dir, "/")
						dir = strings.TrimPrefix(dir, "\\")
					}
					if dir == "" {
						dir = "."
					}
					fmt.Printf("%s %s %s\n", dir, filePath, typeName)
				}
			} else {
				fmt.Printf("%s %s\n", filePath, line)
			}
		}
	}

	return nil
}

func findNextTypeDeclaration(lines []string, startIndex int) (string, string) {
	for i := startIndex; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Skip empty lines
		if line == "" {
			continue
		}

		// Skip comment lines
		if strings.HasPrefix(line, "//") {
			continue
		}

		// Check if this line starts with "type"
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "type" {
			// Extract interface name (remove generic type parameters if present)
			name := fields[1]
			if idx := strings.Index(name, "["); idx != -1 {
				name = name[:idx]
			}
			return name, fields[2]
		}

		// If we hit a non-comment, non-empty line that's not a type declaration, stop
		break
	}

	return "", ""
}
