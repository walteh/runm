//go:build !windows

package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containers/common/pkg/strongunits"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/virt/vf"
	"github.com/walteh/runm/core/virt/vmm"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/units"

	vmfusev1 "github.com/walteh/runm/proto/vmfuse/v1"
)

var (
	listenAddr = flag.String("listen", "unix:///var/run/vmfused.sock", "address to listen on")
)

func main() {
	flag.Parse()

	logger := setupLogger()
	ctx := slogctx.NewCtx(context.Background(), logger)

	if err := runMain(ctx); err != nil {
		slog.ErrorContext(ctx, "vmfused failed", "error", err)
		os.Exit(1)
	}
}

func setupLogger() *slog.Logger {
	return logging.NewDefaultDevLogger("vmfused", os.Stderr)
}

func runMain(ctx context.Context) error {
	// Create hypervisor
	hyper := vf.NewHypervisor()

	// Create the vmfused server
	server := &vmfusedServer{
		mounts:     make(map[string]*MountState),
		hypervisor: hyper,
	}

	// Setup gRPC server
	grpcServer := grpc.NewServer()
	vmfusev1.RegisterVmfuseServiceServer(grpcServer, server)

	// Setup listener
	listener, err := createListener(*listenAddr)
	if err != nil {
		return errors.Errorf("creating listener: %w", err)
	}
	defer listener.Close()

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "starting vmfused server", "address", *listenAddr)
		errChan <- grpcServer.Serve(listener)
	}()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return errors.Errorf("server error: %w", err)
	case sig := <-sigChan:
		slog.InfoContext(ctx, "received shutdown signal", "signal", sig)

		// Graceful shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Cleanup mounts
		server.cleanup(shutdownCtx)

		// Stop gRPC server
		grpcServer.GracefulStop()
		return nil
	}
}

func createListener(addr string) (net.Listener, error) {
	if addr[:7] == "unix://" {
		sockPath := addr[7:]
		// Remove existing socket file
		_ = os.Remove(sockPath)
		return net.Listen("unix", sockPath)
	}
	return net.Listen("tcp", addr)
}

// MountState tracks the state of an active mount
type MountState struct {
	ID        string
	VMID      string
	Config    *vmfusev1.MountRequest
	Status    vmfusev1.MountStatus
	CreatedAt time.Time
	Error     string

	// VM management fields
	vm       *vmm.RunningVM[*vf.VirtualMachine]
	cancelVM context.CancelFunc
	vmCtx    context.Context
	vmIP     string
}

type vmfusedServer struct {
	vmfusev1.UnimplementedVmfuseServiceServer

	mu         sync.RWMutex
	mounts     map[string]*MountState
	hypervisor vmm.Hypervisor[*vf.VirtualMachine]
}

func (s *vmfusedServer) Mount(ctx context.Context, req *vmfusev1.MountRequest) (*vmfusev1.MountResponse, error) {
	slog.InfoContext(ctx, "mount request received",
		"type", req.GetMountType(),
		"sources", req.GetSources(),
		"target", req.GetTarget())

	// Validate request
	if req.GetTarget() == "" {
		return nil, status.Error(codes.InvalidArgument, "target is required")
	}
	if len(req.GetSources()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one source is required")
	}

	// Generate unique IDs with random component to avoid cache conflicts
	timestamp := time.Now().UnixNano()
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomHex := fmt.Sprintf("%x", randomBytes)

	mountID := fmt.Sprintf("mount-%d-%s", timestamp, randomHex)
	vmID := fmt.Sprintf("vmfuse-%d-%s", timestamp, randomHex)

	// Create mount state
	mountState := &MountState{
		ID:        mountID,
		VMID:      vmID,
		Config:    req,
		Status:    vmfusev1.MountStatus_MOUNT_STATUS_CREATING,
		CreatedAt: time.Now(),
	}

	// Store mount state
	s.mu.Lock()
	s.mounts[req.GetTarget()] = mountState
	s.mu.Unlock()

	// Start mount process asynchronously
	go s.performMount(context.WithoutCancel(ctx), mountState)

	response := vmfusev1.NewMountResponse(&vmfusev1.MountResponse_builder{
		MountId: mountID,
		VmId:    vmID,
		Target:  req.GetTarget(),
		Status:  vmfusev1.MountStatus_MOUNT_STATUS_CREATING,
	})

	return response, nil
}

func (s *vmfusedServer) Unmount(ctx context.Context, req *vmfusev1.UnmountRequest) (*emptypb.Empty, error) {
	slog.InfoContext(ctx, "unmount request received", "target", req.GetTarget())

	s.mu.Lock()
	mountState, exists := s.mounts[req.GetTarget()]
	if !exists {
		s.mu.Unlock()
		return nil, status.Error(codes.NotFound, "mount not found")
	}

	// Set status to unmounting
	mountState.Status = vmfusev1.MountStatus_MOUNT_STATUS_UNMOUNTING
	s.mu.Unlock()

	// Perform unmount asynchronously
	go s.performUnmount(ctx, mountState)

	return &emptypb.Empty{}, nil
}

func (s *vmfusedServer) List(ctx context.Context, req *emptypb.Empty) (*vmfusev1.ListResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mounts := make([]*vmfusev1.MountInfo, 0, len(s.mounts))
	for _, mountState := range s.mounts {
		mounts = append(mounts, s.mountStateToInfo(mountState))
	}

	return vmfusev1.NewListResponse(&vmfusev1.ListResponse_builder{
		Mounts: mounts,
	}), nil
}

func (s *vmfusedServer) Status(ctx context.Context, req *vmfusev1.StatusRequest) (*vmfusev1.StatusResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Handle different identifier types using oneof
	var mountState *MountState
	var found bool

	switch req.WhichIdentifier() {
	case vmfusev1.StatusRequest_Target_case:
		mountState, found = s.mounts[req.GetTarget()]
	case vmfusev1.StatusRequest_MountId_case:
		// Search by mount ID
		for _, state := range s.mounts {
			if state.ID == req.GetMountId() {
				mountState = state
				found = true
				break
			}
		}
	default:
		return nil, status.Error(codes.InvalidArgument, "either mount_id or target must be specified")
	}

	if !found {
		return nil, status.Error(codes.NotFound, "mount not found")
	}

	return vmfusev1.NewStatusResponse(&vmfusev1.StatusResponse_builder{
		MountInfo: s.mountStateToInfo(mountState),
	}), nil
}

func (s *vmfusedServer) mountStateToInfo(state *MountState) *vmfusev1.MountInfo {
	return vmfusev1.NewMountInfo(&vmfusev1.MountInfo_builder{
		MountId:      state.ID,
		VmId:         state.VMID,
		MountType:    state.Config.GetMountType(),
		Sources:      state.Config.GetSources(),
		Target:       state.Config.GetTarget(),
		Status:       state.Status,
		CreatedAt:    state.CreatedAt.UnixNano(),
		ErrorMessage: state.Error,
		VmIp:         state.vmIP,
	})
}

func (s *vmfusedServer) performMount(ctx context.Context, mountState *MountState) {

	// the

	slog.InfoContext(ctx, "starting mount process",
		"mount_id", mountState.ID,
		"vm_id", mountState.VMID,
		"target", mountState.Config.GetTarget())

	// Update status through the stages
	s.updateStatus(mountState, vmfusev1.MountStatus_MOUNT_STATUS_STARTING_VM)

	// TODO: Create and start VM with vmfuse-init
	// This is where we would integrate with the existing VMM infrastructure
	// Similar to NewOCIVirtualMachine but with vmfuse-init instead
	if err := s.createAndStartVM(ctx, mountState); err != nil {
		s.setError(mountState, errors.Errorf("failed to start VM: %w", err))
		return
	}

	s.updateStatus(mountState, vmfusev1.MountStatus_MOUNT_STATUS_WAITING_FOR_NFS)

	// TODO: Wait for NFS server to be ready in VM
	if err := s.waitForNFS(ctx, mountState); err != nil {
		s.setError(mountState, errors.Errorf("NFS server failed to start: %w", err))
		return
	}

	s.updateStatus(mountState, vmfusev1.MountStatus_MOUNT_STATUS_MOUNTING_NFS)

	// TODO: Mount NFS from VM to host target
	if err := s.mountNFS(ctx, mountState); err != nil {
		s.setError(mountState, errors.Errorf("NFS mount failed: %w", err))
		return
	}

	s.updateStatus(mountState, vmfusev1.MountStatus_MOUNT_STATUS_ACTIVE)

	slog.InfoContext(ctx, "mount process completed",
		"mount_id", mountState.ID,
		"target", mountState.Config.GetTarget())
}

func (s *vmfusedServer) performUnmount(ctx context.Context, mountState *MountState) {
	slog.InfoContext(ctx, "starting unmount process",
		"mount_id", mountState.ID,
		"target", mountState.Config.GetTarget())

	// TODO: Unmount NFS and shutdown VM
	if err := s.unmountNFS(ctx, mountState); err != nil {
		slog.ErrorContext(ctx, "failed to unmount NFS", "error", err)
	}

	if err := s.shutdownVM(ctx, mountState); err != nil {
		slog.ErrorContext(ctx, "failed to shutdown VM", "error", err)
	}

	// Remove from active mounts
	s.mu.Lock()
	delete(s.mounts, mountState.Config.GetTarget())
	s.mu.Unlock()

	slog.InfoContext(ctx, "unmount process completed",
		"mount_id", mountState.ID,
		"target", mountState.Config.GetTarget())
}

func (s *vmfusedServer) updateStatus(mountState *MountState, status vmfusev1.MountStatus) {
	s.mu.Lock()
	mountState.Status = status
	s.mu.Unlock()
}

func (s *vmfusedServer) cleanup(ctx context.Context) {
	slog.InfoContext(ctx, "cleaning up active mounts")

	s.mu.RLock()
	mounts := make([]*MountState, 0, len(s.mounts))
	for _, state := range s.mounts {
		mounts = append(mounts, state)
	}
	s.mu.RUnlock()

	// Cleanup each mount
	for _, mountState := range mounts {
		s.performUnmount(ctx, mountState)
	}
}

// VM lifecycle methods - integrated with actual VMM infrastructure
func (s *vmfusedServer) createAndStartVM(ctx context.Context, mountState *MountState) error {
	slog.InfoContext(ctx, "creating VM for vmfuse mount",
		"mount_id", mountState.ID,
		"vm_id", mountState.VMID,
		"mount_type", mountState.Config.GetMountType(),
		"sources", mountState.Config.GetSources(),
		"target", mountState.Config.GetTarget())

	// Create VM context that can be cancelled
	vmCtx, cancel := context.WithCancel(ctx)
	mountState.vmCtx = vmCtx
	mountState.cancelVM = cancel

	// Parse VM configuration
	vmConfig := mountState.Config.GetVmConfig()
	if vmConfig == nil {
		vmConfig = vmfusev1.NewVmConfig(&vmfusev1.VmConfig_builder{
			Memory:         "512M",
			Cpus:           2,
			TimeoutSeconds: 300,
		})
	}

	// Parse memory size - for now using a simple conversion
	// TODO: Proper memory parsing should be implemented
	memoryBytes := strongunits.B(512 * 1024 * 1024) // 512MB default
	if vmConfig.GetMemory() != "" {
		// Simple parsing - just use default for now
		memoryBytes = strongunits.B(512 * 1024 * 1024)
	}

	// Build vmfuse-init command arguments
	initArgs := []string{
		"-vmfuse-mount-type", mountState.Config.GetMountType(),
		"-vmfuse-mount-sources", strings.Join(mountState.Config.GetSources(), ","),
		"-vmfuse-mount-target", "/mnt/target",
		"-vmfuse-export-path", "/export",
		"-vmfuse-ready-file", "/virtiofs/ready",
		"-enable-otlp=false",
	}

	// Create vmfuse config
	vmfuseConfig := vmm.VMFuseVMConfig{
		ID:             mountState.VMID, // Use VM ID, not mount ID
		MountType:      mountState.Config.GetMountType(),
		Sources:        mountState.Config.GetSources(),
		Target:         mountState.Config.GetTarget(),
		StartingMemory: memoryBytes,
		VCPUs:          uint64(vmConfig.GetCpus()),
		Platform:       units.PlatformLinuxARM64,
		RawWriter:      os.Stdout,
		DelimWriter:    os.Stderr,
		InitArgs:       initArgs,
	}

	// Create virtual machine using the new vmfuse function
	runningVM, err := vmm.NewVMFuseVirtualMachine(vmCtx, s.hypervisor, vmfuseConfig)
	if err != nil {
		cancel()
		return errors.Errorf("creating vmfuse virtual machine: %w", err)
	}

	mountState.vm = runningVM

	// Start the VM
	if err := runningVM.Start(vmCtx); err != nil {
		cancel()
		runningVM.Close(vmCtx)
		return errors.Errorf("starting VM: %w", err)
	}

	slog.InfoContext(ctx, "VM started successfully",
		"mount_id", mountState.ID,
		"vm_id", mountState.VMID,
		"vm_state", runningVM.VM().CurrentState())

	// Get VM network information
	// For now, use a default IP - in real implementation we'd get this from the VM
	mountState.vmIP = "192.168.127.2"

	return nil
}

func (s *vmfusedServer) waitForNFS(ctx context.Context, mountState *MountState) error {
	slog.InfoContext(ctx, "waiting for NFS server to be ready in VM",
		"mount_id", mountState.ID,
		"vm_id", mountState.VMID)

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
			if mountState.vm != nil && mountState.vm.VM() != nil {
				vmState := mountState.vm.VM().CurrentState()
				if vmState != vmm.VirtualMachineStateTypeRunning {
					return errors.Errorf("VM stopped unexpectedly, state: %v", vmState)
				}
			}

			// In a real implementation, we would check for the ready file through VirtioFS
			// or use VSock communication to query the guest
			// For now, we'll use a simple timeout-based approach
			slog.DebugContext(ctx, "polling for NFS readiness",
				"mount_id", mountState.ID,
				"ready_file", readyFile)

			// TODO: Implement actual ready file checking or VSock communication
			// This could be done by:
			// 1. Using VSock to connect to guest and query status
			// 2. Checking for ready file via shared VirtioFS mount
			// 3. Attempting to connect to NFS port on VM IP

			// For now, simulate readiness after a short delay
			if time.Since(mountState.CreatedAt) > 5*time.Second {
				slog.InfoContext(ctx, "NFS server ready (simulated)",
					"mount_id", mountState.ID,
					"vm_ip", mountState.vmIP)
				return nil
			}
		}
	}
}

func (s *vmfusedServer) mountNFS(ctx context.Context, mountState *MountState) error {
	target := mountState.Config.GetTarget()
	vmIP := mountState.vmIP
	nfsExport := "/mnt/target" // This is the mount target inside the VM that gets exported via NFS

	slog.InfoContext(ctx, "mounting NFS from VM to host",
		"mount_id", mountState.ID,
		"vm_ip", vmIP,
		"nfs_export", nfsExport,
		"target", target)

	// Create target directory if it doesn't exist
	if err := os.MkdirAll(target, 0755); err != nil {
		return errors.Errorf("creating target directory %q: %w", target, err)
	}

	// Build NFS mount command
	// Format: mount_nfs -o nfsvers=3,tcp,intr <vm_ip>:<export_path> <target>
	nfsSource := fmt.Sprintf("%s:%s", vmIP, nfsExport)

	cmd := exec.CommandContext(ctx, "mount_nfs",
		"-o", "nfsvers=3,tcp,intr,rsize=65536,wsize=65536,timeo=14,soft",
		nfsSource,
		target)

	// Set up logging for the mount command
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.InfoContext(ctx, "executing NFS mount command",
		"mount_id", mountState.ID,
		"command", cmd.String(),
		"source", nfsSource,
		"target", target)

	// Execute mount command
	if err := cmd.Run(); err != nil {
		return errors.Errorf("NFS mount command failed: %w", err)
	}

	// Verify mount was successful
	if err := s.verifyMount(ctx, target); err != nil {
		// Attempt cleanup if verification fails
		s.unmountNFS(ctx, mountState)
		return errors.Errorf("mount verification failed: %w", err)
	}

	slog.InfoContext(ctx, "NFS mount successful",
		"mount_id", mountState.ID,
		"source", nfsSource,
		"target", target)

	return nil
}

func (s *vmfusedServer) verifyMount(ctx context.Context, target string) error {
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

func (s *vmfusedServer) unmountNFS(ctx context.Context, mountState *MountState) error {
	target := mountState.Config.GetTarget()

	slog.InfoContext(ctx, "unmounting NFS",
		"mount_id", mountState.ID,
		"target", target)

	// Use umount command to unmount the NFS share
	cmd := exec.CommandContext(ctx, "umount", target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		slog.WarnContext(ctx, "failed to unmount NFS",
			"mount_id", mountState.ID,
			"target", target,
			"error", err)
		// Don't return error - we want cleanup to continue
	} else {
		slog.InfoContext(ctx, "NFS unmounted successfully",
			"mount_id", mountState.ID,
			"target", target)
	}

	return nil
}

func (s *vmfusedServer) shutdownVM(ctx context.Context, mountState *MountState) error {
	slog.InfoContext(ctx, "shutting down VM",
		"mount_id", mountState.ID,
		"vm_id", mountState.VMID)

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
		slog.ErrorContext(ctx, "failed to gracefully close VM, attempting hard stop",
			"mount_id", mountState.ID,
			"error", err)

		// If graceful close fails, try hard stop
		if hardStopErr := mountState.vm.VM().HardStop(shutdownCtx); hardStopErr != nil {
			slog.ErrorContext(ctx, "hard stop also failed",
				"mount_id", mountState.ID,
				"hard_stop_error", hardStopErr)
			// Still don't return error - we want cleanup to continue
		}
	}

	slog.InfoContext(ctx, "VM shutdown completed",
		"mount_id", mountState.ID,
		"vm_id", mountState.VMID)

	// Clear VM references
	mountState.vm = nil
	mountState.cancelVM = nil
	mountState.vmCtx = nil

	return nil
}

func (s *vmfusedServer) setError(mountState *MountState, errorMsg error) {
	s.mu.Lock()
	mountState.Status = vmfusev1.MountStatus_MOUNT_STATUS_ERROR
	mountState.Error = errorMsg.Error()
	s.mu.Unlock()
	slog.ErrorContext(context.Background(), "mount operation failed",
		"mount_id", mountState.ID,
		"error", errorMsg)
}
