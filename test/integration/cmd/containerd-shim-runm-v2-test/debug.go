package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const (
	// Hard-coded debug port for now
	DEBUG_PORT = 2346
)

func isInDebugSession() bool {
	return os.Getppid() != 1
}

// EnableDebugging sets up DAP debugging support for the shim
// It execs directly to dlv if DEBUG_SHIM is set and mode is primary
func EnableDebugging() {

	slog.Info("DEBUG_SHIM enabled, setting up debugging", "ppid", os.Getppid())

	if os.Getppid() != 1 {
		// we are not an orphan so we are being debuggined, return
		slog.Info("DEBUG_SHIM enabled, but we are not an orphan, returning")
		return
	}

	// Only debug primary mode (containerd doesn't let us pass env vars)
	if mode != "primary" {
		slog.Info("DEBUG_SHIM enabled, but mode is not primary, returning")
		return
	}

	slog.Info("DEBUG_SHIM enabled, setting up debugging")

	// Add a delay to give the primary process more time to start up before debugging
	slog.Info("Sleeping 2 seconds to let primary process stabilize...")

	// Check if dlv is available
	dlvPath, err := exec.LookPath("dlv")
	if err != nil {
		slog.Error("dlv (Delve debugger) not found in PATH")
		slog.Error("Install with: go install github.com/go-delve/delve/cmd/dlv@latest")
		os.Exit(1)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		slog.Error("Failed to get executable path for debugging", "error", err)
		os.Exit(1)
	}

	slog.Info("Starting dlv debug session", "port", DEBUG_PORT, "executable", execPath)
	fmt.Printf("Waiting for debugger to attach on port %d\n", DEBUG_PORT)
	fmt.Printf("In VS Code, use the 'Attach to Shim' launch configuration\n")

	dbg := fmt.Sprintf(dbgScript, execPath, DEBUG_PORT, dlvPath, strings.Join(os.Args[1:], " "))

	// save to file
	dbgFile := "/tmp/shim_debug.sh"
	err = os.WriteFile(dbgFile, []byte(dbg), 0755)
	if err != nil {
		slog.Error("Failed to write debug script", "error", err)
		os.Exit(1)
	}

	// // Build dlv arguments
	// dlvArgs := []string{
	// 	"dlv",
	// 	"exec",
	// 	"--headless",
	// 	fmt.Sprintf("--listen=:%d", DEBUG_PORT),
	// 	"--api-version=2",
	// 	"--accept-multiclient",
	// 	execPath,
	// 	"--",
	// }
	// dlvArgs = append(dlvArgs, os.Args[1:]...)

	// Exec directly to dlv
	if err := syscall.Exec(dbgFile, []string{dbgFile}, os.Environ()); err != nil {
		slog.Error("Failed to exec dlv", "error", err)
		os.Exit(1)
	}
}

var dbgScript = `#!/bin/bash
#
# Copyright (c) 2024 Red Hat, Inc.
#
# SPDX-License-Identifier: Apache-2.0
#
# This script allows debugging the GO kata shim using Delve.
# It will start the delve debugger in a way where it runs the program and waits
# for connections from your client interface (dlv commandline, vscode, etc).
#
# You need to configure crio or containerd to use this script in place of the
# regular kata shim binary.
# For cri-o, that would be in the runtime configuration, under
# /etc/crio/crio.conf.d/
#

# Use this for quick-testing the shim binary without a debugger
#NO_DEBUG=1

# Edit this to point to the actual shim binary that needs to be debugged
# Make sure you build it with the following flags:
# -gcflags=all=-N -l
SHIM_BINARY=%[1]s

DLV_PORT=%[2]d

DLV_PATH=%[3]s

# The shim can be called multiple times for the same container.
# If it is already running, subsequent calls just return the socket address that
# crio/containerd need to connect to.
# This is useful for recovery, if crio/contaienrd is restarted and loses context.
#
# We usually don't want to debug those additional calls while we're already
# debugging the actual server process.
# To avoid running additional debuggers and blocking on them, we use a lock file.
LOCK_FILE=/tmp/shim_debug.lock
if [ -e $LOCK_FILE ]; then
    NO_DEBUG=1
fi

# crio can try to call the shim with the "features" or "--version" parameters
# to get capabilities from the runtime (assuming it's an OCI compatible runtime).
# No need to debug that, so just run the regular shim.
case "$1" in
  "features" | "--version")
    NO_DEBUG=1
  ;;
esac


if [ "$NO_DEBUG" == "1" ]; then
    $SHIM_BINARY "$@"
    exit $?
fi


# dlv commandline
#
# --headless: dlv run as a server, waiting for a connection
#
# --listen: port to listen to
#
# --accept-multiclient: allow multiple dlv client connections
#   Allows having both a commandline and a GUI
#
# -r: have the output of the shim redirected to a separate file.
#   This script will retrieve the output and return it to the
#   caller, while letting dlv run in the background for debugging.
#
# -- $@ => give the shim all the parameters this script was given 
#

SHIMOUTPUT=$(mktemp /tmp/shim_output_XXXXXX)

cat > $LOCK_FILE << EOF
#!/bin/bash
${DLV_PATH} exec ${SHIM_BINARY} --headless --listen=:$DLV_PORT --accept-multiclient -r stdout:$SHIMOUTPUT -r stderr:$SHIMOUTPUT --check-go-version=false -- %[4]s
rm $LOCK_FILE
EOF
chmod +x $LOCK_FILE

# We're starting dlv as a background task, so that it continues to run while
# this script returns, letting the caller resume its execution.
#
# We're redirecting the outputs of dlv itself to a separate file so that the
# only output the caller will have is the one from this script, giving it the
# address of the socket to connect to.
#
${LOCK_FILE} "$@" > /tmp/dlv_output 2>&1 &


# wait for the output file of the shim process to be filled with the address.
while [ ! -s $SHIMOUTPUT ]; do
    sleep 1
done

# write the adress to stdout
cat $SHIMOUTPUT
exit 0`
