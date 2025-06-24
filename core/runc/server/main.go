package server

import (
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/state"
	runmv1 "github.com/walteh/runm/proto/v1"
)

type Server struct {
	runtime         runtime.Runtime
	runtimeExtras   runtime.RuntimeExtras
	socketAllocator runtime.SocketAllocator
	eventHandler    runtime.EventHandler
	cgroupAdapter   runtime.CgroupAdapter
	bundleSource    string

	state *state.State
}

type ServerOpt func(*ServerOpts)

type ServerOpts struct {
	BundleSource string
}

func WithBundleSource(bundleSource string) ServerOpt {
	return func(opts *ServerOpts) {
		opts.BundleSource = bundleSource
	}
}

func NewServer(
	r runtime.Runtime,
	runtimeExtras runtime.RuntimeExtras,
	socketAllocator runtime.SocketAllocator,
	eventHandler runtime.EventHandler,
	cgroupAdapter runtime.CgroupAdapter,
	opts ...ServerOpt) *Server {

	optz := &ServerOpts{}
	for _, opt := range opts {
		opt(optz)
	}

	s := &Server{
		runtime:         r,
		runtimeExtras:   runtimeExtras,
		socketAllocator: socketAllocator,
		eventHandler:    eventHandler,
		cgroupAdapter:   cgroupAdapter,
		bundleSource:    optz.BundleSource,
		state:           state.NewState(),
	}

	return s
}

func (s *Server) RegisterGrpcServer(grpcServer *grpc.Server) {
	runmv1.RegisterRuncServiceServer(grpcServer, s)
	runmv1.RegisterRuncExtrasServiceServer(grpcServer, s)
	runmv1.RegisterSocketAllocatorServiceServer(grpcServer, s)
	runmv1.RegisterCgroupAdapterServiceServer(grpcServer, s)
	runmv1.RegisterEventServiceServer(grpcServer, s)
	runmv1.RegisterGuestManagementServiceServer(grpcServer, s)

}

// 	// Create gRPC server
// 	s := grpc.NewServer()

// 	srv := NewServer(config, nil)

// 	runmv1.RegisterRuncServiceServer(s, srv)

// 	// Start server in a goroutine
// 	go func() {
// 		if err := s.Serve(listener); err != nil {
// 			// Log error but don't crash - let the caller handle this
// 		}
// 	}()

// 	return s, nil
// }

// SetupSignalHandler sets up a signal handler for graceful shutdown
func SetupSignalHandler(srv *grpc.Server) chan struct{} {
	// Set up channel for handling signals
	sigs := make(chan os.Signal, 1)
	done := make(chan struct{})

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		srv.GracefulStop()
		close(done)
	}()

	return done
}
