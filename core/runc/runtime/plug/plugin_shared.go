package plug

import (
	"context"
	"net/rpc"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/server"

	grpcruntime "github.com/walteh/runm/core/runc/runtime/grpc"
	runmv1 "github.com/walteh/runm/proto/v1"
)

// Handshake is a common handshake that is shared by plugin and host.
var Handshake = plugin.HandshakeConfig{
	// This isn't required when using VersionedPlugins
	ProtocolVersion:  1,
	MagicCookieKey:   "BASIC_PLUGIN",
	MagicCookieValue: "hello",
}

// PluginMap is the map of plugins we can dispense.
var PluginMap = plugin.PluginSet{
	"runc": &RuntimePlugin{},
}

const (
	PluginName = "runc"
)

func NewRuntimePluginSet(server *server.Server) plugin.PluginSet {
	return plugin.PluginSet{
		PluginName: &RuntimePlugin{
			srv: server,
		},
	}
}

var _ plugin.Plugin = (*RuntimePlugin)(nil)

var _ plugin.GRPCPlugin = (*RuntimePlugin)(nil)

// This is the implementation of plugin.GRPCPlugin so we can serve/consume this.
type RuntimePlugin struct {

	// Concrete implementation, written in Go. This is only used for plugins
	// that are written in Go.
	srv *server.Server
}

// GRPCPlugin must still implement the Plugin interface
func (p *RuntimePlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	panic(runtime.ReflectNotImplementedError())
}

func (p *RuntimePlugin) Server(broker *plugin.MuxBroker) (interface{}, error) {
	panic(runtime.ReflectNotImplementedError())
}

func (p *RuntimePlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	runmv1.RegisterRuncServiceServer(s, p.srv)
	runmv1.RegisterRuncExtrasServiceServer(s, p.srv)
	runmv1.RegisterSocketAllocatorServiceServer(s, p.srv)
	return nil
}

func (p *RuntimePlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return grpcruntime.NewGRPCClientRuntimeFromConn(c)
}
