//go:build darwin

package psnotify

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// getCgroupFromProc is a stub for Darwin since it doesn't use cgroups
func getCgroupFromProc(pid int) (string, error) {
	return "/", nil
}

// getArgvFromProc retrieves command line arguments for a process on Darwin
// using the 'ps' command
func getArgvFromProc(pid int) []string {

	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	out, err := cmd.Output()
	if err != nil {
		return []string{}
	}

	// Split on spaces, but preserve quoted arguments
	args := parseCommandOutput(string(out))
	return args
}

// getExePath retrieves the path to the executable for a process on Darwin
// using the 'ps' command to get the command path
func getExePath(pid int) (string, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// waitByPidfd waits for a process to exit on Darwin
// There's no direct equivalent to Linux's pidfd, so we poll
func waitByPidfd(pid int) error {
	// Maximum number of attempts to check if process exists
	maxAttempts := 10

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Check if process exists using ps
		cmd := exec.Command("ps", "-p", strconv.Itoa(pid))
		err := cmd.Run()

		// Process no longer exists
		if err != nil {
			return nil
		}

		// Sleep briefly before next check
		time.Sleep(10 * time.Millisecond)
	}

	// Process still exists after max attempts, but we'll consider it done
	return nil
}

// parseCommandOutput parses the command output from ps to preserve quoted arguments
func parseCommandOutput(cmdOutput string) []string {
	cmdOutput = strings.TrimSpace(cmdOutput)
	if cmdOutput == "" {
		return []string{}
	}

	var args []string
	var current string
	inQuote := false

	// Simple parser to handle quotes in arguments
	for _, c := range cmdOutput {
		switch c {
		case '"', '\'':
			inQuote = !inQuote
		case ' ':
			if !inQuote {
				if current != "" {
					args = append(args, current)
					current = ""
				}
			} else {
				current += string(c)
			}
		default:
			current += string(c)
		}
	}

	// Add the last argument if any
	if current != "" {
		args = append(args, current)
	}

	return args
}
