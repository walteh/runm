package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"

	"gitlab.com/tozd/go/errors"
)

// redirectSyslogToStdout configures syslog to redirect to stdout for better log visibility
func (v *vmfuseInit) redirectSyslogToStdout(ctx context.Context) error {
	slog.InfoContext(ctx, "setting up syslog redirection to stdout")

	// Start rsyslog with a configuration that forwards everything to stdout
	// This creates a simple rsyslog.conf that sends all logs to stdout via a program
	rsyslogConf := `/etc/rsyslog.conf`
	confContent := `# Forward all syslog messages to stdout
$ModLoad imuxsock # provides support for local system logging
$ModLoad imklog   # provides kernel logging support

# Send all messages to stdout via logger command
*.* @@127.0.0.1:514

# Also create a rule to pipe to stdout directly
$template StdoutFormat,"%timegenerated% %HOSTNAME% %syslogtag%%msg:::sp-if-no-1st-sp%%msg:::drop-last-lf%\n"
*.* |/bin/sh -c 'cat >> /proc/1/fd/1'
`

	// Write rsyslog configuration
	if err := os.WriteFile(rsyslogConf, []byte(confContent), 0644); err != nil {
		return errors.Errorf("writing rsyslog config: %w", err)
	}

	slog.InfoContext(ctx, "created rsyslog configuration for stdout redirection")

	// Try to start rsyslog in the background - don't fail if it doesn't work
	go func() {
		cmd := exec.CommandContext(ctx, "rsyslogd", "-n", "-f", rsyslogConf)
		if err := cmd.Run(); err != nil {
			slog.WarnContext(ctx, "rsyslog failed to start", "error", err)
		}
	}()

	return nil
}

// redirectDevLogToStdout creates a simple log forwarder for /dev/log
func (v *vmfuseInit) redirectDevLogToStdout(ctx context.Context) error {
	// Create a simple forwarder that reads from /dev/log and writes to stdout
	// This is a best-effort approach
	logSocket := "/dev/log"

	// Remove existing socket if it exists
	_ = os.Remove(logSocket)

	// Create a Unix domain socket listener
	listener, err := net.Listen("unix", logSocket)
	if err != nil {
		return errors.Errorf("creating log socket listener: %w", err)
	}

	slog.InfoContext(ctx, "created /dev/log socket for stdout redirection")

	// Start accepting connections in the background
	go func() {
		defer listener.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn, err := listener.Accept()
				if err != nil {
					continue
				}

				// Handle each connection in a separate goroutine
				go v.handleLogConnection(ctx, conn)
			}
		}
	}()

	return nil
}

func (v *vmfuseInit) checkSystemRequirements(ctx context.Context) {
	// Check /proc/filesystems for NFS support
	if data, err := os.ReadFile("/proc/filesystems"); err == nil {
		if strings.Contains(string(data), "nfs") {
			slog.InfoContext(ctx, "NFS filesystem support detected")
		} else {
			slog.WarnContext(ctx, "NFS filesystem support not found in /proc/filesystems")
		}
	}

	// Check if we have the necessary directories
	dirs := []string{"/proc", "/sys", "/dev", "/tmp", "/var/run"}
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil {
			slog.WarnContext(ctx, "required directory missing", "dir", dir, "error", err)
		} else if !info.IsDir() {
			slog.WarnContext(ctx, "required path is not a directory", "dir", dir)
		}
	}
}

func (v *vmfuseInit) logActivePorts(ctx context.Context) error {
	// Read /proc/net/tcp to get active listening ports
	tcpData, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return errors.Errorf("reading /proc/net/tcp: %w", err)
	}

	// Read /proc/net/tcp6 for IPv6 ports
	tcp6Data, err := os.ReadFile("/proc/net/tcp6")
	if err != nil {
		slog.DebugContext(ctx, "could not read /proc/net/tcp6", "error", err)
		tcp6Data = nil
	}

	// Parse listening ports
	listeningPorts := v.parseListeningPorts(string(tcpData), string(tcp6Data))

	if len(listeningPorts) > 0 {
		slog.InfoContext(ctx, "active listening ports", "ports", listeningPorts)
	} else {
		slog.WarnContext(ctx, "no listening ports found")
	}

	// Also try using netstat if available for comparison
	if err := v.logNetstatInfo(ctx); err != nil {
		slog.DebugContext(ctx, "netstat not available or failed", "error", err)
	}

	return nil
}

func (v *vmfuseInit) parseListeningPorts(tcp, tcp6 string) []string {
	var ports []string

	// Parse IPv4 TCP ports
	ports = append(ports, v.parsePortsFromProcNet(tcp, "tcp4")...)

	// Parse IPv6 TCP ports if available
	if tcp6 != "" {
		ports = append(ports, v.parsePortsFromProcNet(tcp6, "tcp6")...)
	}

	return ports
}

func (v *vmfuseInit) parsePortsFromProcNet(data, protocol string) []string {
	var ports []string
	lines := strings.Split(data, "\n")

	for i, line := range lines {
		if i == 0 { // Skip header
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		localAddr := fields[1]
		state := fields[3]

		// State 0A = LISTEN (listening)
		if state == "0A" {
			parts := strings.Split(localAddr, ":")
			if len(parts) == 2 {
				// Convert hex port to decimal
				if port, err := parseHexPort(parts[1]); err == nil {
					ports = append(ports, fmt.Sprintf("%s:%d", protocol, port))
				}
			}
		}
	}

	return ports
}

func parseHexPort(hexPort string) (int, error) {
	port := int64(0)
	for _, char := range hexPort {
		port *= 16
		if char >= '0' && char <= '9' {
			port += int64(char - '0')
		} else if char >= 'A' && char <= 'F' {
			port += int64(char - 'A' + 10)
		} else if char >= 'a' && char <= 'f' {
			port += int64(char - 'a' + 10)
		} else {
			return 0, errors.Errorf("invalid hex character: %c", char)
		}
	}
	return int(port), nil
}

func (v *vmfuseInit) logNetstatInfo(ctx context.Context) error {
	// Try to use ss first (more modern and commonly available)
	// if err := ExecCmdForwardingStdio(ctx, "ss", "-tlnp"); err == nil {
	// 	slog.DebugContext(ctx, "ss output logged via ExecCmdForwardingStdio")
	// 	return nil
	// }

	// Fallback to netstat if ss is not available
	if err := ExecCmdForwardingStdio(ctx, "netstat", "-tlnp"); err == nil {
		slog.DebugContext(ctx, "netstat output logged via ExecCmdForwardingStdio")
		return nil
	}

	return errors.Errorf("neither ss nor netstat available")
}
