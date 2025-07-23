//go:build darwin

package vmfused

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/containers/common/pkg/strongunits"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/walteh/runm/core/virt/vf"
	"github.com/walteh/runm/core/virt/vmm"
	"github.com/walteh/runm/pkg/units"

	vmfusev1 "github.com/walteh/runm/proto/vmfuse/v1"
)

var _ vmfusev1.VmfuseServiceServer = &VmfusedServer[*vf.VirtualMachine]{}

type VmfusedServer[VM vmm.VirtualMachine] struct {
	mu         sync.RWMutex
	mounts     map[string]*MountState[VM]
	hypervisor vmm.Hypervisor[VM]
	// taskGroup  *taskgroup.TaskGroup
	listener net.Listener
}

func (s *VmfusedServer[VM]) Register(grpcServer *grpc.Server) {
	vmfusev1.RegisterVmfuseServiceServer(grpcServer, s)
}

func CreateListener(addr string) (net.Listener, error) {
	listener, err := createListener(addr)
	if err != nil {
		return nil, errors.Errorf("creating listener: %w", err)
	}
	return listener, nil
}

func NewVmfusedServer[VM vmm.VirtualMachine](ctx context.Context, listener net.Listener, hypervisor vmm.Hypervisor[VM]) (*VmfusedServer[VM], error) {
	// Create hypervisor

	// Create the vmfused server
	server := &VmfusedServer[VM]{
		mounts:     make(map[string]*MountState[VM]),
		hypervisor: hypervisor,
		listener:   listener,
	}

	// // Setup gRPC server
	// grpcServer := grpc.NewServer()
	// vmfusev1.RegisterVmfuseServiceServer(grpcServer, server)

	// Setup listener

	// // Start server in goroutine
	// errChan := make(chan error, 1)
	// go func() {
	// 	slog.InfoContext(ctx, "starting vmfused server", "address", *listenAddr)
	// 	errChan <- grpcServer.Serve(listener)
	// }()

	// // Handle shutdown signals
	// sigChan := make(chan os.Signal, 1)
	// signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// select {
	// case err := <-errChan:
	// 	return errors.Errorf("server error: %w", err)
	// case sig := <-sigChan:
	// 	slog.InfoContext(ctx, "received shutdown signal", "signal", sig)

	// 	// Graceful shutdown
	// 	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	// 	defer cancel()

	// 	// Cleanup mounts
	// 	server.cleanup(shutdownCtx)

	// 	// Stop gRPC server
	// 	grpcServer.GracefulStop()
	// 	return nil
	// }

	return server, nil
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
type MountState[VM vmm.VirtualMachine] struct {
	ID        string
	VMID      string
	Config    *vmfusev1.MountRequest
	Status    vmfusev1.MountStatus
	CreatedAt time.Time
	Error     string

	// VM management fields
	vm          *vmm.RunningVM[VM]
	cancelVM    context.CancelFunc
	vmCtx       context.Context
	nfsHostPort uint16
}

func (s *VmfusedServer[VM]) Mount(ctx context.Context, req *vmfusev1.MountRequest) (*vmfusev1.MountResponse, error) {
	slog.InfoContext(ctx, "mount request received", "request", req)

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
	mountState := &MountState[VM]{
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
	if err := s.performMount(context.WithoutCancel(ctx), mountState); err != nil {
		return nil, errors.Errorf("failed to start mount process: %w", err)
	}

	response := vmfusev1.NewMountResponse(&vmfusev1.MountResponse_builder{
		MountId: mountID,
		VmId:    vmID,
		Target:  req.GetTarget(),
		Status:  vmfusev1.MountStatus_MOUNT_STATUS_CREATING,
	})

	return response, nil
}

func (s *VmfusedServer[VM]) Unmount(ctx context.Context, req *vmfusev1.UnmountRequest) (*emptypb.Empty, error) {
	slog.InfoContext(ctx, "unmount request received", "request", req)

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

func (s *VmfusedServer[VM]) List(ctx context.Context, req *emptypb.Empty) (*vmfusev1.ListResponse, error) {
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

func (s *VmfusedServer[VM]) Status(ctx context.Context, req *vmfusev1.StatusRequest) (*vmfusev1.StatusResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Handle different identifier types using oneof
	var mountState *MountState[VM]
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

func (s *VmfusedServer[VM]) mountStateToInfo(state *MountState[VM]) *vmfusev1.MountInfo {
	return vmfusev1.NewMountInfo(&vmfusev1.MountInfo_builder{
		MountId:      state.ID,
		VmId:         state.VMID,
		MountType:    state.Config.GetMountType(),
		Sources:      state.Config.GetSources(),
		Target:       state.Config.GetTarget(),
		Status:       state.Status,
		CreatedAt:    state.CreatedAt.UnixNano(),
		ErrorMessage: state.Error,
		NfsHostPort:  uint32(state.nfsHostPort),
	})
}

func (s *VmfusedServer[VM]) performMount(ctx context.Context, mountState *MountState[VM]) error {

	slog.InfoContext(ctx, "starting mount process", "mount_state", mountState)

	// Update status through the stages
	s.updateStatus(mountState, vmfusev1.MountStatus_MOUNT_STATUS_STARTING_VM)

	// TODO: Create and start VM with vmfuse-init
	// This is where we would integrate with the existing VMM infrastructure
	// Similar to NewOCIVirtualMachine but with vmfuse-init instead
	if err := s.createAndStartVM(ctx, mountState); err != nil {
		errz := errors.Errorf("failed to start VM: %w", err)
		s.setError(mountState, errz)
		return errz
	}

	s.updateStatus(mountState, vmfusev1.MountStatus_MOUNT_STATUS_WAITING_FOR_NFS)

	// TODO: Wait for NFS server to be ready in VM
	if err := mountState.waitForNFS(ctx); err != nil {
		errz := errors.Errorf("NFS server failed to start: %w", err)
		s.setError(mountState, errz)
		return errz
	}

	s.updateStatus(mountState, vmfusev1.MountStatus_MOUNT_STATUS_MOUNTING_NFS)

	// TODO: Mount NFS from VM to host target
	if err := mountState.mountNFS(ctx); err != nil {
		errz := errors.Errorf("NFS mount failed: %w", err)
		s.setError(mountState, errz)
		return errz
	}

	s.updateStatus(mountState, vmfusev1.MountStatus_MOUNT_STATUS_ACTIVE)

	slog.InfoContext(ctx, "mount process completed", "mount_id", mountState.ID, "target", mountState.Config.GetTarget())

	return nil
}

func (s *VmfusedServer[VM]) performUnmount(ctx context.Context, mountState *MountState[VM]) {
	slog.InfoContext(ctx, "starting unmount process", "mount_state", mountState)

	// TODO: Unmount NFS and shutdown VM
	if err := mountState.unmountNFS(ctx); err != nil {
		slog.ErrorContext(ctx, "failed to unmount NFS", "error", err)
	}

	if err := mountState.shutdownVM(ctx); err != nil {
		slog.ErrorContext(ctx, "failed to shutdown VM", "error", err)
	}

	// Remove from active mounts
	s.mu.Lock()
	delete(s.mounts, mountState.Config.GetTarget())
	s.mu.Unlock()

	slog.InfoContext(ctx, "unmount process completed", "mount_state", mountState)
}

func (s *VmfusedServer[VM]) updateStatus(mountState *MountState[VM], status vmfusev1.MountStatus) {
	s.mu.Lock()
	mountState.Status = status
	s.mu.Unlock()
}

func (s *VmfusedServer[VM]) Cleanup(ctx context.Context) {
	slog.InfoContext(ctx, "cleaning up active mounts")

	s.mu.RLock()
	mounts := make([]*MountState[VM], 0, len(s.mounts))
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
func (s *VmfusedServer[VM]) createAndStartVM(ctx context.Context, mountState *MountState[VM]) error {
	slog.InfoContext(ctx, "creating VM for vmfuse mount", "mount_state", mountState)

	// Create VM context that can be cancelled
	vmCtx, cancel := context.WithCancel(ctx)
	mountState.vmCtx = vmCtx
	mountState.cancelVM = cancel

	// Parse VM configuration
	vmConfig := mountState.Config.GetVmConfig()
	if vmConfig == nil {
		vmConfig = vmfusev1.NewVmConfig(&vmfusev1.VmConfig_builder{
			MemoryMib:      256,
			Cpus:           2,
			TimeoutSeconds: 300,
		})
	}

	// Parse memory size - for now using a simple conversion
	// TODO: Proper memory parsing should be implemented
	memoryBytes := strongunits.MiB(vmConfig.GetMemoryMib()).ToBytes()
	if memoryBytes == 0 {
		memoryBytes = strongunits.MiB(256).ToBytes() // 64MB default
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

	slog.InfoContext(ctx, "starting vm with args", "args", initArgs, "memory", memoryBytes, "cpus", vmConfig.GetCpus())

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
	runningVM, nfsPort, err := vmm.NewVMFuseVirtualMachine(vmCtx, s.hypervisor, vmfuseConfig)
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

	slog.InfoContext(ctx, "VM started successfully", "mount_id", mountState.ID, "vm_id", mountState.VMID, "nfs_host_port", nfsPort, "vm_state", runningVM.VM().CurrentState())

	// Get VM network information
	// For now, use a default IP - in real implementation we'd get this from the VM
	mountState.nfsHostPort = nfsPort

	// wait for vm to be running
	if err := vmm.WaitForVMState(vmCtx, runningVM.VM(), vmm.VirtualMachineStateTypeRunning, time.After(10*time.Second)); err != nil {
		return errors.Errorf("VM not up and running: %v", err)
	}

	return nil
}

// func (s *VmfusedServer[VM]) monitorVM(ctx context.Context, mountState *MountState[VM]) (string, error) {

// }

func (s *VmfusedServer[VM]) setError(mountState *MountState[VM], errorMsg error) {
	s.mu.Lock()
	mountState.Status = vmfusev1.MountStatus_MOUNT_STATUS_ERROR
	mountState.Error = errorMsg.Error()
	s.mu.Unlock()
	slog.ErrorContext(context.Background(), "mount operation failed", "mount_id", mountState.ID, "error", errorMsg)
}
