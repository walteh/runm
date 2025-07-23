package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"gitlab.com/tozd/go/errors"
)

func (v *vmfuseInit) executeGaneshaWithDetails(ctx context.Context) error {
	// Custom execution of Ganesha with detailed error tracking and logging
	binary := "/mbin/ganesha"
	// Try different argument combinations - the StaticX version might need different args
	args := []string{"-f", "/etc/ganesha/ganesha.conf", "-N", "NIV_DEBUG", "-F"} // -F = foreground mode

	slog.InfoContext(ctx, "executing Ganesha with detailed monitoring",
		"binary", binary,
		"fullCommand", fmt.Sprintf("%s %s", binary, strings.Join(args, " ")))

	cmd := exec.CommandContext(ctx, binary, args...)

	// Create buffers to capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set up environment
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH") + ":/mbin"}, os.Environ()...)

	// Record start time
	startTime := time.Now()

	// Run the command
	err := cmd.Run()
	duration := time.Since(startTime)

	// Log results with more detail
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelError
	}

	slog.Log(ctx, level, "Ganesha execution completed",
		"binary", binary,
		"args", args,
		"duration", duration,
		"stdout_size", stdout.Len(),
		"stderr_size", stderr.Len(),
		"stdout_content", stdout.String(),
		"stderr_content", stderr.String(),
		"error", err)

	// If there's any output, log it separately for visibility
	if stdout.Len() > 0 {
		slog.InfoContext(ctx, "Ganesha stdout output", "output", stdout.String())
	}

	if stderr.Len() > 0 {
		slog.WarnContext(ctx, "Ganesha stderr output", "output", stderr.String())
	}

	return err
}

func (v *vmfuseInit) verifyGaneshaBinary(ctx context.Context) error {
	binaryPath := "/mbin/ganesha"

	// Check if file exists
	info, err := os.Stat(binaryPath)
	if err != nil {
		return errors.Errorf("Ganesha binary not found at %s: %w", binaryPath, err)
	}

	// Check if it's executable
	mode := info.Mode()
	if mode&0111 == 0 {
		return errors.Errorf("Ganesha binary is not executable: mode=%v", mode)
	}

	slog.InfoContext(ctx, "Ganesha binary verified",
		"path", binaryPath,
		"size", info.Size(),
		"mode", fmt.Sprintf("%o", mode))

	// Try to get help using the correct flag format
	if err := ExecCmdForwardingStdio(ctx, binaryPath, "-h"); err != nil {
		slog.WarnContext(ctx, "Ganesha binary may not be functional", "help_error", err)
	}

	// Try to get version information using -v
	if err := ExecCmdForwardingStdio(ctx, binaryPath, "-v"); err != nil {
		slog.WarnContext(ctx, "Ganesha version check failed", "version_error", err)
	}

	// Check for library dependencies using ldd if available
	// v.checkLibraryDependencies(ctx, binaryPath)

	return nil
}

func (v *vmfuseInit) logGaneshaConfig(ctx context.Context) error {
	configPath := "/etc/ganesha/ganesha.conf"

	config, err := os.ReadFile(configPath)
	if err != nil {
		return errors.Errorf("reading Ganesha config: %w", err)
	}

	slog.InfoContext(ctx, "Ganesha configuration",
		"path", configPath,
		"size", len(config),
		"content", string(config))

	return nil
}

func (v *vmfuseInit) diagnoseGaneshaFailure(ctx context.Context) {
	slog.InfoContext(ctx, "diagnosing Ganesha failure...")

	// Check if config file exists and is readable
	if _, err := os.ReadFile("/etc/ganesha/ganesha.conf"); err != nil {
		slog.ErrorContext(ctx, "config file issue", "error", err)
	}

	// Check if export path exists and is accessible
	if info, err := os.Stat("/mnt/target"); err != nil {
		slog.ErrorContext(ctx, "export path issue", "path", "/mnt/target", "error", err)
	} else {
		slog.InfoContext(ctx, "export path status", "path", "/mnt/target", "mode", info.Mode())
	}

	// Try running Ganesha with different flags to get more info
	slog.InfoContext(ctx, "trying Ganesha with debug flags...")
	if err := ExecCmdForwardingStdio(ctx, "/mbin/ganesha", "-f", "/etc/ganesha/ganesha.conf", "-N", "NIV_DEBUG"); err != nil {
		slog.ErrorContext(ctx, "debug run also failed", "error", err)
	}

	// Check system capabilities that Ganesha might need
	v.checkSystemRequirements(ctx)
}

func (v *vmfuseInit) executeGaneshaAlternative(ctx context.Context) error {
	// Try different argument combinations that might work
	alternatives := [][]string{
		{"-f", "/etc/ganesha/ganesha.conf"},                                             // Minimal args
		{"-f", "/etc/ganesha/ganesha.conf", "-d"},                                       // Debug mode
		{"-f", "/etc/ganesha/ganesha.conf", "-N", "NIV_DEBUG"},                          // Debug logging
		{"-f", "/etc/ganesha/ganesha.conf", "-L", "/dev/stdout"},                        // Log to stdout only
		{"-f", "/etc/ganesha/ganesha.conf", "-N", "NIV_INFO", "-L", "/tmp/ganesha.log"}, // Log to file
	}

	binary := "/mbin/ganesha"

	for i, args := range alternatives {
		slog.InfoContext(ctx, "trying Ganesha alternative",
			"attempt", i+1,
			"args", args)

		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH") + ":/mbin"}, os.Environ()...)

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		startTime := time.Now()
		err := cmd.Run()
		duration := time.Since(startTime)

		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelWarn
		}

		slog.Log(ctx, level, "Ganesha alternative attempt result",
			"attempt", i+1,
			"args", args,
			"duration", duration,
			"stdout", stdout.String(),
			"stderr", stderr.String(),
			"error", err)

		// If this one worked (no error), return success
		if err == nil {
			slog.InfoContext(ctx, "Ganesha alternative succeeded", "args", args)
			return nil
		}

		// Short delay between attempts
		time.Sleep(100 * time.Millisecond)
	}

	return errors.Errorf("all Ganesha alternative executions failed")
}

func (v *vmfuseInit) createGaneshaDirectories(ctx context.Context) error {

	// untar the ganesha plugins
	if err := os.MkdirAll("/ganesha-plugins", 0755); err != nil {
		return errors.Errorf("creating ganesha plugins directory: %w", err)
	}
	if err := ExecCmdForwardingStdio(ctx, "tar", "-xzf", "/mbin/ganesha-plugins.tar.gz", "-C", "/ganesha-plugins"); err != nil {
		return errors.Errorf("untaring ganesha plugins: %w", err)
	}

	// ls the plugins
	if err := ExecCmdForwardingStdio(ctx, "ls", "-lahrs", "/ganesha-plugins/ganesha-plugins"); err != nil {
		return errors.Errorf("listing ganesha plugins: %w", err)
	}

	// make the /usr/lib/ganesha directory
	if err := os.MkdirAll("/usr/lib/ganesha", 0755); err != nil {
		return errors.Errorf("creating ganesha directory: %w", err)
	}

	// symlink the /usr/lib/ganesha to /ganesha-plugins
	if err := os.Symlink("/ganesha-plugins/ganesha-plugins/libfsalvfs.so", "/usr/lib/ganesha/libfsalvfs.so"); err != nil {
		return errors.Errorf("symlinking ganesha plugins: %w", err)
	}

	// symlink [ -e /etc/mtab ] || ln -s /proc/mounts /etc/mtab
	if _, err := os.Stat("/etc/mtab"); os.IsNotExist(err) {
		if err := os.Symlink("/proc/self/mounts", "/etc/mtab"); err != nil {
			return errors.Errorf("symlinking mtab: %w", err)
		}
	}

	// untar the ganesha plugins
	// Create Ganesha configuration directory
	if err := os.MkdirAll("/etc/ganesha", 0755); err != nil {
		return errors.Errorf("creating ganesha config directory: %w", err)
	}

	// Create NFS recovery directories
	recoveryDirs := []string{
		"/var/lib/nfs/ganesha",
		"/var/lib/nfs/ganesha/v4recov",
		"/var/lib/nfs/ganesha/v4old",
		"/var/lib/nfs/ganesha/v4recov/node0",
		"/var/lib/nfs/ganesha/v4old/node0",
	}

	for _, dir := range recoveryDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return errors.Errorf("creating recovery directory %s: %w", dir, err)
		}
	}

	// Create basic /etc/netconfig for UDP support
	netconfig := `udp        tpi_clts      v     inet     udp     -       -
tcp        tpi_cots_ord  v     inet     tcp     -       -
udp6       tpi_clts      v     inet6    udp     -       -
tcp6       tpi_cots_ord  v     inet6    tcp     -       -
rawip      tpi_raw       -     inet      -      -       -
local      tpi_cots_ord  -     loopback  -      -       -
unix       tpi_cots_ord  -     loopback  -      -       -
`
	if err := os.WriteFile("/etc/netconfig", []byte(netconfig), 0644); err != nil {
		return errors.Errorf("creating netconfig file: %w", err)
	}

	slog.InfoContext(ctx, "created Ganesha directories and files",
		"recovery_dirs", len(recoveryDirs),
		"netconfig", "/etc/netconfig")

	return nil
}

// DIRECTORY_SERVICES {
// 	DomainName = "localdomain";
// 	idmapped_user_time_validity  = 600;
// 	idmapped_group_time_validity = 600;
//   }

func (v *vmfuseInit) setupGaneshaNFS(ctx context.Context) error {
	// Create all necessary directories for Ganesha
	if err := v.createGaneshaDirectories(ctx); err != nil {
		return errors.Errorf("creating ganesha directories: %w", err)
	}

	// Create Ganesha configuration file with only supported parameters
	ganeshaConfig := fmt.Sprintf(`
NFS_Core_Param {  
  NFS_Port = 2049;  
  NFS_Protocols = 4;  
} 

EXPORT {
    Export_Id = 1;
    Path = %s;       # must exist
    Pseudo = /export;
    Access_Type = RW;
    Squash = No_Root_Squash;
    Protocols = 4;
    Transports = TCP;
    SecType = sys;
    FSAL { Name = VFS; }
}
`, mountTarget)

	if err := os.WriteFile("/etc/ganesha/ganesha.conf", []byte(ganeshaConfig), 0644); err != nil {
		return errors.Errorf("writing ganesha config: %w", err)
	}

	slog.InfoContext(ctx, "created Ganesha config", "path", "/etc/ganesha/ganesha.conf", "export_path", mountTarget)

	// Set up network interfaces
	if err := ExecCmdForwardingStdio(ctx, "ip", "link", "set", "lo", "up"); err != nil {
		return errors.Errorf("setting up loopback: %w", err)
	}

	// Log active ports for debugging
	if err := v.logActivePorts(ctx); err != nil {
		slog.WarnContext(ctx, "failed to log active ports", "error", err)
	}

	// Start Ganesha NFS server in background with extensive debugging
	go func() {
		// First, verify the Ganesha binary exists and is executable
		if err := v.verifyGaneshaBinary(ctx); err != nil {
			slog.ErrorContext(ctx, "Ganesha binary verification failed", "error", err)
			return
		}

		// Log configuration contents before starting
		if err := v.logGaneshaConfig(ctx); err != nil {
			slog.WarnContext(ctx, "failed to log Ganesha config", "error", err)
		}

		// First try to understand what arguments Ganesha accepts
		slog.InfoContext(ctx, "checking Ganesha usage before starting...")
		if err := ExecCmdForwardingStdio(ctx, "/mbin/ganesha", "-h"); err != nil {
			slog.WarnContext(ctx, "could not get Ganesha usage", "error", err)
		}

		// // Try to validate the configuration
		// slog.InfoContext(ctx, "validating Ganesha configuration...")
		// if err := ExecCmdForwardingStdio(ctx, "/mbin/ganesha", "-f", "/etc/ganesha/ganesha.conf", "-t"); err != nil {
		// 	slog.WarnContext(ctx, "Ganesha config validation failed (or -t flag not supported)", "error", err)
		// }

		// Try to start Ganesha with detailed error reporting
		slog.InfoContext(ctx, "starting Ganesha NFS server",
			"binary", "/mbin/ganesha",
			"config", "/etc/ganesha/ganesha.conf")

		if err := v.executeGaneshaWithDetails(ctx); err != nil {
			// slog.ErrorContext(ctx, "Ganesha NFS server failed", "error", err)

			// // Try alternative arguments
			// slog.InfoContext(ctx, "trying alternative Ganesha arguments...")
			// if err2 := v.executeGaneshaAlternative(ctx); err2 != nil {
			// 	slog.ErrorContext(ctx, "Ganesha alternative execution also failed", "error", err2)
			// }

			// Try to get more info about why it failed
			// v.diagnoseGaneshaFailure(ctx)

			slog.ErrorContext(ctx, "Ganesha NFS server failed", "error", err)
		} else {
			slog.InfoContext(ctx, "Ganesha NFS server exited cleanly")
		}

		// Log active ports after Ganesha attempt
		if err := v.logActivePorts(ctx); err != nil {
			slog.WarnContext(ctx, "failed to log active ports after Ganesha exit", "error", err)
		}
	}()

	// Wait for Ganesha to be ready by checking if port 2049 is listening
	if err := v.waitForGaneshaReady(ctx); err != nil {
		return errors.Errorf("waiting for Ganesha readiness: %w", err)
	}

	// Signal readiness
	if err := v.signalReady(ctx); err != nil {
		return errors.Errorf("signaling ready: %w", err)
	}

	// Log active ports for debugging
	if err := v.logActivePorts(ctx); err != nil {
		slog.WarnContext(ctx, "failed to log active ports", "error", err)
	}

	// Specifically verify NFS-related ports
	v.verifyNFSPorts(ctx)

	slog.InfoContext(ctx, "Ganesha NFS server started", "export_path", mountTarget, "config", "/etc/ganesha/ganesha.conf")

	return nil
}

func (v *vmfuseInit) waitForGaneshaReady(ctx context.Context) error {
	// Wait for Ganesha to be ready by checking if port 2049 is listening
	for i := range 30 { // Try for up to 30 seconds
		conn, err := net.DialTimeout("tcp", ":2049", 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			slog.InfoContext(ctx, "Ganesha NFS server is ready", "attempts", i+1)
			return nil
		} else {
			slog.DebugContext(ctx, "Ganesha NFS server is not ready", "error", err, "attempts", i+1)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
			continue
		}
	}

	return errors.Errorf("Ganesha NFS server did not become ready within 30 seconds")
}

func (v *vmfuseInit) checkLibraryDependencies(ctx context.Context, binaryPath string) {
	// Try ldd to check shared library dependencies
	if err := ExecCmdForwardingStdio(ctx, "ldd", binaryPath); err != nil {
		slog.DebugContext(ctx, "ldd check failed - might be static binary", "error", err)

		// Try file command to get binary info
		if err := ExecCmdForwardingStdio(ctx, "file", binaryPath); err != nil {
			slog.DebugContext(ctx, "file command also failed", "error", err)
		}

		// Try readelf if available for ELF info
		if err := ExecCmdForwardingStdio(ctx, "readelf", "-h", binaryPath); err != nil {
			slog.DebugContext(ctx, "readelf not available or failed", "error", err)
		}
	}
}
