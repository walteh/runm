package gvnet

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"

	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
	"github.com/soheilhy/cmux"
	"gitlab.com/tozd/go/errors"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/walteh/runm/core/gvnet/tapsock"
	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/pkg/taskgroup"
)

type GvproxyConfig struct {
	EnableDebug bool // if true, print debug info

	MagicHostPort              string // host port to access the guest virtual machine, must be between 1024 and 65535
	EnableMagicSSHForwarding   bool   // enable ssh forwarding
	EnableMagicHTTPForwarding  bool   // enable http forwarding
	EnableMagicHTTPSForwarding bool   // enable https forwarding

	MTU int // set the MTU, default is 1500

	WorkingDir string // working directory
}

func GvproxyVersion() string {
	return types.NewVersion("gvnet").String()
}

type gvproxy struct {
	netdev    *virtio.VirtioNet
	taskGroup *taskgroup.TaskGroup
	forwarder *tapsock.VirtualNetworkRunner
	stack     *stack.Stack
	magic     *MagicHostPort
	cfg       *GvproxyConfig
}

func (p *gvproxy) VirtioNetDevice() *virtio.VirtioNet {
	return p.netdev
}

type Proxy interface {
	Wait(ctx context.Context) error
	VirtioNetDevice() *virtio.VirtioNet
}

func NewProxy(ctx context.Context, cfg *GvproxyConfig) (Proxy, error) {

	if ctx.Err() != nil {
		return nil, errors.Errorf("cant start gvproxy, context cancelled: %w", ctx.Err())
	}

	// start the vmFileSocket
	device, forwarder, err := tapsock.NewDgramVirtioNet(ctx, VIRTUAL_GUEST_MAC)
	if err != nil {
		return nil, errors.Errorf("vmFileSocket listen: %w", err)
	}

	config, err := cfg.buildConfiguration(ctx)
	if err != nil {
		return nil, errors.Errorf("building configuration: %w", err)
	}

	vn, err := virtualnetwork.New(config)
	if err != nil {
		return nil, errors.Errorf("creating virtual network: %w", err)
	}

	if err := forwarder.ApplyVirtualNetwork(vn); err != nil {
		return nil, errors.Errorf("applying virtual network: %w", err)
	}

	stack, err := tapsock.IsolateNetworkStack(vn)
	if err != nil {
		return nil, errors.Errorf("isolating network stack: %w", err)
	}

	return &gvproxy{
		forwarder: forwarder,
		netdev:    device,
		stack:     stack,
		cfg:       cfg,
	}, nil
}

func (p *gvproxy) Wait(ctx context.Context) error {
	// Create taskgroup for managing all gvproxy tasks
	tg := taskgroup.NewTaskGroup(ctx,
		taskgroup.WithName("gvproxy"),
		taskgroup.WithLogStart(true),
		taskgroup.WithLogEnd(true),
		taskgroup.WithLogTaskStart(true),
		taskgroup.WithLogTaskEnd(true),
		taskgroup.WithSlogBaseContext(ctx),
	)

	tg.GoWithName("tapsock-runner", func(ctx context.Context) error {
		return p.forwarder.Run(ctx)
	})

	if p.magic != nil {
		m, err := p.setupMagicForwarding(tg, ctx, p.stack)
		if err != nil {
			return errors.Errorf("setting up magic forwarding: %w", err)
		}

		tg.GoWithName("magic-forwarding", func(ctx context.Context) error {
			return m.Run(ctx)
		})
	}

	// Add cleanup task that monitors context cancellation
	tg.GoWithName("cleanup-monitor", func(ctx context.Context) error {
		<-ctx.Done()
		tg.LogCancellationIfCancelled("cleanup-monitor")
		return nil
	})

	return tg.Wait()
}

// func (p *gvproxy) proxyListener(ctx context.Context, listener net.Listener) error {
// 	for {
// 		conn, err := listener.Accept()
// 		if err != nil {
// 			return errors.Errorf("accepting connection: %w", err)
// 		}

// 		go func() {
// 			defer conn.Close()

// 			if err := p.forwarder.Run(ctx); err != nil {
// 				slog.ErrorContext(ctx, "running forwarder", "error", err)
// 			}
// 		}()

// 	}

// 	return nil
// }

func captureFile(cfg *GvproxyConfig) string {
	if !cfg.EnableDebug {
		return ""
	}
	return filepath.Join(cfg.WorkingDir, "capture.pcap")
}

func (cfg *GvproxyConfig) buildConfiguration(ctx context.Context) (*types.Configuration, error) {

	if cfg.MTU == 0 {
		cfg.MTU = 1500
	}

	dnss, err := searchDomains(ctx)
	if err != nil {
		slog.WarnContext(ctx, "searching domains", "error", err)
	}

	config := types.Configuration{
		Debug:             cfg.EnableDebug,
		CaptureFile:       captureFile(cfg),
		MTU:               cfg.MTU,
		Subnet:            VIRTUAL_SUBNET_CIDR,
		GatewayIP:         VIRTUAL_GATEWAY_IP,
		GatewayMacAddress: VIRTUAL_GATEWAY_MAC,
		DHCPStaticLeases: map[string]string{
			VIRTUAL_GUEST_IP: VIRTUAL_GUEST_MAC,
		},
		DNS: []types.Zone{
			{
				Name: "containers.internal.",
				Records: []types.Record{

					{
						Name: gateway,
						IP:   net.ParseIP(VIRTUAL_GATEWAY_IP),
					},
					{
						Name: host,
						IP:   net.ParseIP(VIRUTAL_HOST_IP),
					},
				},
			},
			{
				Name: "docker.internal.",
				Records: []types.Record{
					{
						Name: gateway,
						IP:   net.ParseIP(VIRTUAL_GATEWAY_IP),
					},
					{
						Name: host,
						IP:   net.ParseIP(VIRUTAL_HOST_IP),
					},
				},
			},
		},
		DNSSearchDomains: dnss,
		// Forwards:         virtualPortMap,
		// RawForwards: virtualPortMap,
		NAT: map[string]string{
			VIRUTAL_HOST_IP: LOCAL_HOST_IP,
		},
		GatewayVirtualIPs: []string{VIRUTAL_HOST_IP},
		// VpnKitUUIDMacAddresses: map[string]string{
		// 	"c3d68012-0208-11ea-9fd7-f2189899ab08": VIRTUAL_GUEST_MAC,
		// },
		Protocol: types.VfkitProtocol, // this is the exact same as 'bess', basically just means "not streaming"
	}

	return &config, nil
}

func (p *gvproxy) setupMagicForwarding(tg *taskgroup.TaskGroup, ctx context.Context, stack *stack.Stack) (*MagicHostPort, error) {
	m, err := NewMagicHostPortStream(ctx, p.cfg.MagicHostPort, tg)
	if err != nil {
		return nil, errors.Errorf("creating global host port: %w", err)
	}

	if p.cfg.EnableMagicSSHForwarding {
		err = m.ForwardCMUXMatchToGuestPort(ctx, stack, 22, cmux.PrefixMatcher("SSH-"))
		if err != nil {
			return nil, errors.Errorf("forwarding cmux ssh to guest port: %w", err)
		}
	}

	if p.cfg.EnableMagicHTTPSForwarding {
		err = m.ForwardCMUXMatchToGuestPort(ctx, stack, 443, cmux.TLS())
		if err != nil {
			return nil, errors.Errorf("forwarding cmux https to guest port: %w", err)
		}
	}

	if p.cfg.EnableMagicHTTPForwarding {
		err = m.ForwardCMUXMatchToGuestPort(ctx, stack, 80, cmux.HTTP1())
		if err != nil {
			return nil, errors.Errorf("forwarding cmux http to guest port: %w", err)
		}
		err = m.ForwardCMUXMatchToGuestPort(ctx, stack, 80, cmux.HTTP2())
		if err != nil {
			return nil, errors.Errorf("forwarding cmux http2 to guest port: %w", err)
		}
	}

	// route everything else to port 80
	err = m.ForwardCMUXMatchToGuestPort(ctx, stack, 80, cmux.Any())
	if err != nil {
		return nil, errors.Errorf("forwarding cmux match to guest port: %w", err)
	}

	return m, nil
}
