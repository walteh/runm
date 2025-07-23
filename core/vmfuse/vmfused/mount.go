package vmfused

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/walteh/runm/core/virt/vmm"
	"gitlab.com/tozd/go/errors"
)

func (mountState *MountState[VM]) waitForNFS(ctx context.Context) error {
	slog.InfoContext(ctx, "waiting for NFS server to be ready in VM", "mount_id", mountState.ID, "vm_id", mountState.VMID)

	// Wait for the ready file to appear via VirtioFS
	readyFile := "/virtiofs/ready"
	maxWaitTime := 60 * time.Second
	pollInterval := 1 * time.Second

	timeoutCtx, cancel := context.WithTimeout(ctx, maxWaitTime)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return errors.Errorf("timeout waiting for NFS ready signal after %v", maxWaitTime)
		case <-ticker.C:
			// Check if VM is still running
			if mountState.vm != nil {
				vmState := mountState.vm.VM().CurrentState()
				if vmState != vmm.VirtualMachineStateTypeRunning {
					return errors.Errorf("VM stopped unexpectedly, state: %v", vmState)
				}
			}

			slog.DebugContext(ctx, "polling for NFS readiness", "mount_id", mountState.ID, "ready_file", readyFile)

			// Check if NFS server is ready by attempting to connect to port 2049
			if mountState.nfsHostPort != 0 {
				conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", mountState.nfsHostPort), 2*time.Second)
				if err == nil {
					_ = conn.Close()
					slog.InfoContext(ctx, "NFS server is ready",
						"mount_id", mountState.ID,
						"nfs_host_port", mountState.nfsHostPort)
					return nil
				}
				slog.DebugContext(ctx, "NFS server not yet ready", "mount_id", mountState.ID, "nfs_host_port", mountState.nfsHostPort, "error", err)
			} else {
				slog.DebugContext(ctx, "VM IP not yet available", "mount_id", mountState.ID)
			}
		}
	}
}

func (mountState *MountState[VM]) mountNFS(ctx context.Context) error {
	target := mountState.Config.GetTarget()
	nfsHostPort := mountState.nfsHostPort
	nfsExport := "/" // This is the mount target inside the VM that gets exported via NFS

	slog.InfoContext(ctx, "mounting NFS from VM to host", "mount_id", mountState.ID, "nfs_host_port", nfsHostPort, "nfs_export", nfsExport, "target", target)

	// Create target directory if it doesn't exist
	if err := os.MkdirAll(target, 0755); err != nil {
		return errors.Errorf("creating target directory %q: %w", target, err)
	}

	// Build NFS mount command
	// Format: mount_nfs -o nfsvers=4,tcp,port=<port> <vm_ip>:<export_path> <target>
	nfsSource := fmt.Sprintf("127.0.0.1:%s", nfsExport)
	mountOpts := fmt.Sprintf("nfsvers=4,tcp,port=%d,intr,rsize=65536,wsize=65536,timeo=14,soft", nfsHostPort)

	cmd := exec.CommandContext(ctx, "mount_nfs", "-o", mountOpts, nfsSource, target)

	// Set up logging for the mount command
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.InfoContext(ctx, "executing NFS mount command", "mount_id", mountState.ID, "command", cmd.String(), "source", nfsSource, "target", target)

	// Execute mount command
	if err := cmd.Run(); err != nil {
		return errors.Errorf("NFS mount command failed: %w", err)
	}

	// Verify mount was successful
	if err := mountState.verifyMount(ctx); err != nil {
		// Attempt cleanup if verification fails
		mountState.unmountNFS(ctx)
		return errors.Errorf("mount verification failed: %w", err)
	}

	slog.InfoContext(ctx, "NFS mount successful", "mount_id", mountState.ID, "source", nfsSource, "target", target)

	return nil
}

func (mountState *MountState[VM]) verifyMount(ctx context.Context) error {
	target := mountState.Config.GetTarget()
	// Check if the target directory is actually mounted
	// We can do this by checking if it's listed in the mount table
	cmd := exec.CommandContext(ctx, "mount")
	output, err := cmd.Output()
	if err != nil {
		return errors.Errorf("failed to get mount information: %w", err)
	}

	mountLines := strings.Split(string(output), "\n")
	for _, line := range mountLines {
		if strings.Contains(line, target) && strings.Contains(line, "nfs") {
			slog.DebugContext(ctx, "mount verified", "target", target, "mount_line", line)
			return nil
		}
	}

	return errors.Errorf("target %q not found in mount table", target)
}

func (mountState *MountState[VM]) unmountNFS(ctx context.Context) error {
	target := mountState.Config.GetTarget()

	slog.InfoContext(ctx, "unmounting NFS", "mount_id", mountState.ID, "target", target)

	// Use umount command to unmount the NFS share
	cmd := exec.CommandContext(ctx, "umount", target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		slog.WarnContext(ctx, "failed to unmount NFS", slog.String("mount_id", mountState.ID), slog.String("target", target), slog.Any("error", err))
		// Don't return error - we want cleanup to continue
	} else {
		slog.InfoContext(ctx, "NFS unmounted successfully", "mount_id", mountState.ID, "target", target)
	}

	return nil
}

func (mountState *MountState[VM]) shutdownVM(ctx context.Context) error {
	slog.InfoContext(ctx, "shutting down VM", "mount_id", mountState.ID, "vm_id", mountState.VMID)

	if mountState.vm == nil {
		slog.WarnContext(ctx, "no VM to shutdown", "mount_id", mountState.ID)
		return nil
	}

	// Cancel the VM context first to signal shutdown
	if mountState.cancelVM != nil {
		mountState.cancelVM()
	}

	// Close the running VM (this will handle graceful shutdown)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := mountState.vm.Close(shutdownCtx); err != nil {
		slog.ErrorContext(ctx, "failed to gracefully close VM, attempting hard stop", "mount_id", mountState.ID, "error", err)

		// If graceful close fails, try hard stop
		if hardStopErr := mountState.vm.VM().HardStop(shutdownCtx); hardStopErr != nil {
			slog.ErrorContext(ctx, "hard stop also failed", "mount_id", mountState.ID, "hard_stop_error", hardStopErr)
			// Still don't return error - we want cleanup to continue
		}
	}

	slog.InfoContext(ctx, "VM shutdown completed", "mount_id", mountState.ID, "vm_id", mountState.VMID)

	// Clear VM references
	mountState.vm = nil
	mountState.cancelVM = nil
	mountState.vmCtx = nil

	return nil
}
