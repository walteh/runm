package env

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/walteh/runm/pkg/logging"
)

// hasActiveDapConnection checks if there's an active DAP connection by detecting connection behavior
func hasActiveDapConnection(port int) bool {
	// First check if we can connect at all
	conn, err := net.DialTimeout("tcp", "localhost:"+strconv.Itoa(port), 1*time.Second)
	if err != nil {
		slog.Debug("Cannot connect to Delve server", "port", port, "error", err)
		return false
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			slog.Debug("Failed to close connection", "error", closeErr)
		}
	}()

	// Since Delve is running with --accept-multiclient, we can check if there are active connections
	// by examining the connection behavior. If VS Code is connected, the server should behave differently.
	//
	// Method 1: Check if we can connect immediately vs having to wait
	// When no clients are connected, Delve accepts connections immediately
	// When clients are connected, there might be slight delays or different behavior

	// Try multiple quick connections to see if the server behavior indicates active clients
	activeConnections := 0
	for i := 0; i < 3; i++ {
		testConn, err := net.DialTimeout("tcp", "localhost:"+strconv.Itoa(port), 500*time.Millisecond)
		if err != nil {
			continue
		}

		// Try to write a simple test message to see if there's active communication
		testConn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		_, writeErr := testConn.Write([]byte("test\n"))

		testConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buffer := make([]byte, 10)
		_, readErr := testConn.Read(buffer)

		testConn.Close()

		// If we can write but read times out, there might be an active session
		if writeErr == nil && readErr != nil {
			activeConnections++
		}

		time.Sleep(50 * time.Millisecond)
	}

	// If we detected signs of active connections, consider it successful
	if activeConnections > 0 {
		slog.Debug("Detected signs of active DAP connection", "port", port, "indicators", activeConnections)
		return true
	}

	// Method 2: Use netstat-like approach to check for established connections
	// This is more reliable but platform-specific
	return hasEstablishedConnections(port)
}

// hasEstablishedConnections checks if there are established connections to the given port
func hasEstablishedConnections(port int) bool {
	// On macOS, use lsof to check for connections to the port
	cmd := exec.Command("lsof", "-i", ":"+strconv.Itoa(port), "-n", "-P")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("Failed to run lsof", "error", err)
		// Fallback to netstat if lsof fails
		return hasEstablishedConnectionsNetstat(port)
	}

	lines := strings.Split(string(output), "\n")
	connectionCount := 0

	for _, line := range lines {
		// Skip the header line
		if strings.Contains(line, "COMMAND") || strings.TrimSpace(line) == "" {
			continue
		}

		// Look for established connections (not LISTEN)
		if strings.Contains(line, "ESTABLISHED") ||
			(strings.Contains(line, "TCP") && !strings.Contains(line, "LISTEN")) {
			connectionCount++
			slog.Debug("Found active connection", "line", line)
		}
	}

	// We expect at least 1 connection (VS Code), but our own test connection might also show up
	// So look for >= 1 connections
	if connectionCount >= 1 {
		slog.Debug("Found active connections", "port", port, "count", connectionCount)
		return true
	}

	slog.Debug("No active connections found", "port", port, "count", connectionCount)
	return false
}

// hasEstablishedConnectionsNetstat fallback using netstat
func hasEstablishedConnectionsNetstat(port int) bool {
	cmd := exec.Command("netstat", "-an")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("Failed to run netstat", "error", err)
		return false
	}

	lines := strings.Split(string(output), "\n")
	portStr := ":" + strconv.Itoa(port)

	establishedCount := 0
	for _, line := range lines {
		if strings.Contains(line, portStr) && strings.Contains(line, "ESTABLISHED") {
			establishedCount++
			slog.Debug("Found established connection", "line", line)
		}
	}

	// Look for at least 1 established connection
	if establishedCount >= 1 {
		slog.Debug("Found established connections", "port", port, "count", establishedCount)
		return true
	}

	slog.Debug("No established connections found", "port", port, "count", establishedCount)
	return false
}

// retryDebugCallbackUntilActiveConnection retries debug-callback until active connection is established
func retryDebugCallbackUntilActiveConnection(ctx context.Context, debugCallback string, port int) bool {
	maxAttempts := 3 // Limit to 3 attempts to avoid infinite callbacks

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		slog.Info("Calling debug-callback", "attempt", attempt, "port", port)

		// Call the debug callback
		cm := exec.CommandContext(ctx, debugCallback, strconv.Itoa(port), "go")
		cm.Stdout = logging.GetDefaultRawWriter()
		cm.Stderr = logging.GetDefaultRawWriter()
		err := cm.Run()

		if err != nil {
			slog.Debug("debug-callback command failed", "attempt", attempt, "error", err)
			// If the callback itself fails, wait a bit and try again
			if attempt < maxAttempts {
				time.Sleep(5 * time.Second)
				continue
			}
			return false
		}

		slog.Debug("debug-callback command completed", "attempt", attempt)

		// Wait for connection to be established, checking periodically
		slog.Info("Waiting for DAP client to connect", "port", port, "attempt", attempt)

		// Check multiple times over a reasonable period
		for checkAttempt := 1; checkAttempt <= 6; checkAttempt++ {
			select {
			case <-ctx.Done():
				return false
			default:
			}

			if hasActiveDapConnection(port) {
				slog.Info("DAP connection established", "port", port, "callback_attempt", attempt, "check_attempt", checkAttempt)
				return true
			}

			// Wait 5 seconds between checks
			if checkAttempt < 6 {
				time.Sleep(5 * time.Second)
			}
		}

		slog.Warn("No active DAP connection detected after callback", "port", port, "attempt", attempt)

		// If this isn't the last attempt, wait a bit before retrying the callback
		if attempt < maxAttempts {
			slog.Info("Retrying debug-callback", "next_attempt", attempt+1, "port", port)
			time.Sleep(5 * time.Second)
		}
	}

	slog.Error("Failed to establish DAP connection after all attempts", "port", port, "max_attempts", maxAttempts)
	return false
}

func DebugIsEnabled() (enabled bool) {
	if os.Getenv(underDlvEnvVar) == "1" {
		return false
	}

	envVar := resolveDebugEnvVar()
	if envVar == "" {
		return false
	}

	return true
}

// EnableDebugging sets up DAP debugging support for the shim
// It execs directly to dlv if DEBUG_SHIM is set and mode is primary
func EnableDebugging() func() {

	if !DebugIsEnabled() {
		return func() {}
	}

	dlvPath, err := FindDlvPath()
	if err != nil {
		panic(err)
	}

	pid := os.Getpid()

	// find open port
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		slog.Debug("Failed to close listener", "error", err)
	}

	args := []string{
		"attach",
		strconv.Itoa(pid),
		"--headless",
		"--continue",
		"--listen=:" + strconv.Itoa(port),
		"--accept-multiclient",
		"--check-go-version=false",
		"--api-version=2",
		// "--",
	}

	debugCallback, err := exec.LookPath("debug-callback")
	if err != nil {
		slog.Error("Failed to find debug-callback", "error", err)
	}

	// args = append(args, os.Args[1:]...)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	pr, pw := io.Pipe()

	cm := exec.CommandContext(ctx, dlvPath, args...)
	cm.Env = append(os.Environ(), underDlvEnvVar+"=1")
	cm.Stdout = io.MultiWriter(logging.GetDefaultRawWriter(), pw)
	cm.Stderr = io.MultiWriter(logging.GetDefaultRawWriter(), pw)
	cm.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // make child the leader of a new PGID
	}

	if err = cm.Start(); err != nil {
		panic(err)
	}

	// Don't wait for delve process - let context cancellation handle cleanup

	sec := bufio.NewScanner(pr)

	for sec.Scan() {
		read := sec.Text()
		if strings.Contains(read, "API server listening at") {
			slog.Info("delve is listening on port", "port", port)
			break
		}
	}

	// if the debug-callback is set, call it and retry every 5 seconds until active DAP connection is detected
	if debugCallback != "" {
		if !retryDebugCallbackUntilActiveConnection(ctx, debugCallback, port) {
			slog.Warn("Failed to establish active DAP connection after all retries")
			return cancel
		}
	}

	// Signal that we're ready for debugging - debugger can set breakpoints and pause execution
	slog.Info("Process ready for debugging - set breakpoints and pause execution from your debugger")

	// Small delay to let debugger attach and set breakpoints
	time.Sleep(500 * time.Millisecond)

	// runtime.Breakpoint()

	return cancel
}
