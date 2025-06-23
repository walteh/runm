package grpcruntime

import (
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/state"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var (
	_ runtime.Runtime         = (*GRPCClientRuntime)(nil)
	_ runtime.RuntimeExtras   = (*GRPCClientRuntime)(nil)
	_ runtime.CgroupAdapter   = (*GRPCClientRuntime)(nil)
	_ runtime.EventHandler    = (*GRPCClientRuntime)(nil)
	_ runtime.GuestManagement = (*GRPCClientRuntime)(nil)
)

// Client is a client for the runc service.

type GRPCClientRuntime struct {
	runtimeGrpcService        runmv1.RuncServiceClient
	runtimeExtrasGprcService  runmv1.RuncExtrasServiceClient
	guestManagmentService     runmv1.GuestManagementServiceClient
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
		opts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
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
		guestManagmentService:      runmv1.NewGuestManagementServiceClient(conn),
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

func (me *GRPCClientRuntime) Management() runmv1.GuestManagementServiceClient {
	return me.guestManagmentService
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
func (c *GRPCClientRuntime) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
