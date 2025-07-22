//go:build !windows

package grpcruntime

import (
	"context"
	"log/slog"

	"gitlab.com/tozd/go/errors"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/state"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var (
	_ runtime.Runtime       = (*GRPCClientRuntime)(nil)
	_ runtime.RuntimeExtras = (*GRPCClientRuntime)(nil)
	_ runtime.CgroupAdapter = (*GRPCClientRuntime)(nil)
	_ runtime.EventHandler  = (*GRPCClientRuntime)(nil)
)

// Client is a client for the runc service.

type GRPCClientRuntime struct {
	runtimeGrpcService        runmv1.RuncServiceClient
	runtimeExtrasGprcService  runmv1.RuncExtrasServiceClient
	guestCgroupAdapterService runmv1.CgroupAdapterServiceClient
	eventService              runmv1.EventServiceClient

	// used internally, no neeed to implement it
	socketAllocatorGrpcService runmv1.SocketAllocatorServiceClient

	vsockProxier runtime.VsockProxier

	sharedDirPathPrefix string

	conn *grpc.ClientConn

	state *state.State
}

// NewRuncClient creates a new client for the runc service.
func NewGRPCClientRuntime(target string, opts ...grpc.DialOption) (*GRPCClientRuntime, error) {
	if len(opts) == 0 {
		opts = []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			// grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1024 * 1024 * 10)),
		}
	}

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, errors.Errorf("failed to connect to runc service: %w", err)
	}

	return NewGRPCClientRuntimeFromConn(conn)
}

// NewClientFromConn creates a new client from an existing connection.
func NewGRPCClientRuntimeFromConn(conn *grpc.ClientConn) (*GRPCClientRuntime, error) {

	client := &GRPCClientRuntime{
		runtimeGrpcService:         runmv1.NewRuncServiceClient(conn),
		runtimeExtrasGprcService:   runmv1.NewRuncExtrasServiceClient(conn),
		socketAllocatorGrpcService: runmv1.NewSocketAllocatorServiceClient(conn),
		guestCgroupAdapterService:  runmv1.NewCgroupAdapterServiceClient(conn),
		eventService:               runmv1.NewEventServiceClient(conn),
		conn:                       conn,
		state:                      state.NewState(),
	}

	return client, nil
}

func (me *GRPCClientRuntime) SetVsockProxier(proxier runtime.VsockProxier) {
	me.vsockProxier = proxier
}

func (me *GRPCClientRuntime) Runtime() runmv1.RuncServiceClient {
	return me.runtimeGrpcService
}

func (me *GRPCClientRuntime) RuntimeExtras() runmv1.RuncExtrasServiceClient {
	return me.runtimeExtrasGprcService
}

func (me *GRPCClientRuntime) SocketAllocator() runmv1.SocketAllocatorServiceClient {
	return me.socketAllocatorGrpcService
}

func (me *GRPCClientRuntime) CgroupAdapter() runmv1.CgroupAdapterServiceClient {
	return me.guestCgroupAdapterService
}

func (me *GRPCClientRuntime) EventPublisher() runmv1.EventServiceClient {
	return me.eventService
}

// Close closes the client connection.
func (c *GRPCClientRuntime) Close(ctx context.Context) error {
	slog.Debug("closing grpc runtime")
	for name, io := range c.state.OpenIOs().Range {
		err := io.Close()
		slog.Debug("closed io", "name", name, "err", err)
	}
	slog.Debug("closing consoles")
	for name, c := range c.state.OpenConsoles().Range {
		err := c.Close()
		slog.Debug("closed console", "name", name, "err", err)
	}
	slog.Debug("closing vsock connections")
	for name, v := range c.state.OpenVsockConnections().Range {
		err := v.Close()
		slog.Debug("closed vsock", "name", name, "err", err)
	}
	slog.Debug("closing unix connections")
	for name, v := range c.state.OpenUnixConnections().Range {
		err := v.Close()
		slog.Debug("closed unix", "name", name, "err", err)
	}

	if c.conn != nil {
		slog.Debug("closing grpc conn")
		err := c.conn.Close()
		slog.Debug("closed grpc conn", "err", err)
	}
	return nil
}
