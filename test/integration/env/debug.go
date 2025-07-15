package env

import (
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/walteh/runm/pkg/logging"
	"gitlab.com/tozd/go/errors"
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
func EnableDebugging() error {

	port, enabled := DebugEnabledOnPort()
	if !enabled {
		return nil
	}

	dlvPath, err := FindDlvPath()
	if err != nil {
		return errors.Errorf("failed to find dlv: %w", err)
	}

	pid := os.Getpid()

	args := []string{
		"attach",
		strconv.Itoa(pid),
		"--headless",
		// "--continue",
		"--listen=:" + strconv.Itoa(port),
		"--accept-multiclient",
		"--check-go-version=false",
		// "--",
	}

	// args = append(args, os.Args[1:]...)

	cm := exec.Command(dlvPath, args...)
	cm.Env = append(os.Environ(), underDlvEnvVar+"=1")
	cm.Stdout = logging.GetDefaultRawWriter()
	cm.Stderr = logging.GetDefaultRawWriter()

	err = cm.Start()
	if err != nil {
		return errors.Errorf("failed to start dlv: %w", err)
	}

	// write to the default raw write saying we are listening on a socket

	go func() {
		err = cm.Wait()
		if err != nil {
			slog.Error("Failed to wait for dlv", "error", err)
		}
	}()

	// sleep for 5 seconds, give delve some time to start
	time.Sleep(5 * time.Second)

	return nil
}
