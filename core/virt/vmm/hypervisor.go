package vmm

import (
	"context"
	"net"
	"path/filepath"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/linux/constants"
)

const (
	ExecVSockPort = 2019
)

//go:mock
type Hypervisor[VM VirtualMachine] interface {
	NewVirtualMachine(ctx context.Context, id string, opts *NewVMOptions, bootLoader virtio.Bootloader) (VM, error)
	OnCreate() <-chan VM
}

// func (r *RunningVM[VM]) guestService(ctx context.Context) harpoonv1.TTRPCGuestServiceClient {
// 	r.guestServiceConnectionMu.Lock()
// 	defer r.guestServiceConnectionMu.Unlock()

// 	if r.guestServiceConnection == nil {
// 		conn, err := r.vm.VSockConnect(ctx, uint32(constants.VsockPort))
// 		if err != nil {
// 			slog.Error("failed to dial vsock", "error", err)
// 			return nil
// 		}
// 		r.guestServiceConnection = harpoonv1.NewTTRPCGuestServiceClient(ttrpc.NewClient(conn))
// 	}

// 	return r.guestServiceConnection
// }

func NewEc1BlockDevice(ctx context.Context, wrkdir string) (virtio.VirtioDevice, specs.Mount, error) {
	ec1DataPath := filepath.Join(wrkdir, "harpoon-runtime-fs-device")

	outMount := specs.Mount{
		Type:        "virtiofs",
		Source:      constants.Ec1VirtioTag,
		Destination: constants.Ec1AbsPath,
		Options:     []string{},
	}

	ec1Dev, err := virtio.VirtioFsNew(ec1DataPath, constants.Ec1VirtioTag)
	if err != nil {
		return nil, outMount, errors.Errorf("creating ec1 virtio device: %w", err)
	}

	return ec1Dev, outMount, nil
}

func NewMbinBlockDevice(ctx context.Context, wrkdir string) (virtio.VirtioDevice, specs.Mount, error) {
	ec1DataPath := filepath.Join(wrkdir, constants.MbinFileName)

	outMount := specs.Mount{
		Type:        constants.MbinFSType,
		Source:      constants.MbinVirtioTag,
		Destination: constants.MbinAbsPath,
		Options:     []string{"ro"},
	}

	mbinDev, err := virtio.VirtioBlkNew(ec1DataPath)
	if err != nil {
		return nil, outMount, errors.Errorf("creating mbin virtio device: %w", err)
	}

	return mbinDev, outMount, nil
}

func connectToVsockWithRetry(ctx context.Context, vm VirtualMachine, port uint32) (net.Conn, error) {

	ticker := time.NewTicker(100 * time.Millisecond)
	timeout := time.NewTimer(3 * time.Second)
	defer ticker.Stop()
	defer timeout.Stop()

	lastError := error(errors.Errorf("initial error"))

	for {
		select {
		case <-ticker.C:
			conn, err := vm.VSockConnect(ctx, port)
			if err != nil {
				lastError = err
				continue
			}
			return conn, nil
		case <-timeout.C:
			return nil, errors.Errorf("timeout waiting for guest service connection: %w", lastError)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
