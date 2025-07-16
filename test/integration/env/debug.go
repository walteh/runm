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
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/walteh/runm/pkg/logging"
)

func DebugEnabledOnPort() (port int, enabled bool) {
	if os.Getenv(underDlvEnvVar) == "1" {
		return -1, false
	}

	envVar := resolveDebugEnvVar()
	if envVar == "" {
		return -1, false
	}
	portz, err := strconv.Atoi(envVar)
	if err != nil {
		return -1, false
	}
	return portz, true
}

// EnableDebugging sets up DAP debugging support for the shim
// It execs directly to dlv if DEBUG_SHIM is set and mode is primary
func EnableDebugging() func() {

	_, enabled := DebugEnabledOnPort()
	if !enabled {
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
	ln.Close()

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

	err = cm.Start()
	if err != nil {
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

	// if the debug-callback is set, call it and wait for it to complete
	if debugCallback != "" {
		callbackDone := make(chan error, 1)
		go func() {
			cm := exec.CommandContext(ctx, debugCallback, strconv.Itoa(port), "go")
			cm.Stdout = logging.GetDefaultRawWriter()
			cm.Stderr = logging.GetDefaultRawWriter()
			err := cm.Run()
			callbackDone <- err
		}()

		// Wait for callback to complete or timeout
		select {
		case err := <-callbackDone:
			if err != nil {
				slog.Error("Failed to run debug-callback", "error", err)
			} else {
				slog.Info("debug-callback completed successfully")
			}
		case <-ctx.Done():
			slog.Info("Context cancelled before debug-callback completed")
			return cancel
		}
	}

	// Signal that we're ready for debugging - debugger can set breakpoints and pause execution
	slog.Info("Process ready for debugging - set breakpoints and pause execution from your debugger")

	// Small delay to let debugger attach and set breakpoints
	time.Sleep(500 * time.Millisecond)

	runtime.Breakpoint()

	return cancel
}
