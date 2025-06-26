package gvnet

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"

	"golang.org/x/sync/errgroup"

	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
	"github.com/soheilhy/cmux"
	"github.com/walteh/run"
	"gitlab.com/tozd/go/errors"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	slogctx "github.com/veqryn/slog-context"

	"github.com/walteh/runm/core/gvnet/tapsock"
	"github.com/walteh/runm/core/virt/virtio"
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
	netdev *virtio.VirtioNet
	waiter func(ctx context.Context) error
}

func (p *gvproxy) VirtioNetDevice() *virtio.VirtioNet {
	return p.netdev
}

func (p *gvproxy) Wait(ctx context.Context) error {
	return p.waiter(ctx)
}

type Proxy interface {
	Wait(ctx context.Context) error
	VirtioNetDevice() *virtio.VirtioNet
}

func NewProxy(ctx context.Context, cfg *GvproxyConfig) (Proxy, error) {

	if ctx.Err() != nil {
		return nil, errors.Errorf("cant start gvproxy, context cancelled: %w", ctx.Err())
	}

	defer func() {
		slog.DebugContext(ctx, "gvproxy defer")
	}()

	ctx = slogctx.WithGroup(ctx, "gvnet")

	groupErrs, ctx := errgroup.WithContext(ctx)

	group := run.New(run.WithLogger(slog.Default()))

	go func() {
		<-ctx.Done()
		slog.InfoContext(ctx, "DONE, cleaning up gvproxy")
	}()

	// start the vmFileSocket
	device, runner, err := tapsock.NewDgramVirtioNet(ctx, VIRTUAL_GUEST_MAC)
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

	if err := runner.ApplyVirtualNetwork(vn); err != nil {
		return nil, errors.Errorf("applying virtual network: %w", err)
	}

	stack, err := tapsock.IsolateNetworkStack(vn)
	if err != nil {
		return nil, errors.Errorf("isolating network stack: %w", err)
	}

	if cfg.MagicHostPort == "" {
		slog.InfoContext(ctx, "setting up magic forwarding", "magicHostPort", cfg.MagicHostPort)
		m, err := cfg.setupMagicForwarding(ctx, stack)
		if err != nil {
			return nil, errors.Errorf("setting up magic forwarding: %w", err)
		}
		group.Always(m)

	}

	groupErrs.Go(func() error {
		if ctx.Err() != nil {
			slog.InfoContext(ctx, "context cancelled, not running runner")
			return nil
		}

		if err := runner.Run(ctx); err != nil {
			slog.ErrorContext(ctx, "running runner", "error", err)
		}
		return nil
	})

	groupErrs.Go(func() error {
		if ctx.Err() != nil {
			slog.InfoContext(ctx, "context cancelled, not running group")
			return nil
		}

		slog.InfoContext(ctx, "listening on gvproxy network")
		if err := group.RunContext(ctx); err != nil {
			return errors.Errorf("listening on gvproxy network: %w", err)
		}
		return nil
	})

	return &gvproxy{
		netdev: device,
		waiter: func(ctx context.Context) error {
			if err := groupErrs.Wait(); err != nil {
				if err == context.Canceled {
					return nil
				}
				return errors.Errorf("gvnet exiting: %v", err)
			}
			return nil
		},
	}, nil
}

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

func (cfg *GvproxyConfig) setupMagicForwarding(ctx context.Context, stack *stack.Stack) (*MagicHostPort, error) {
	m, err := NewMagicHostPortStream(ctx, cfg.MagicHostPort)
	if err != nil {
		return nil, errors.Errorf("creating global host port: %w", err)
	}

	if cfg.EnableMagicSSHForwarding {
		err = m.ForwardCMUXMatchToGuestPort(ctx, stack, 22, cmux.PrefixMatcher("SSH-"))
		if err != nil {
			return nil, errors.Errorf("forwarding cmux ssh to guest port: %w", err)
		}
	}

	if cfg.EnableMagicHTTPSForwarding {
		err = m.ForwardCMUXMatchToGuestPort(ctx, stack, 443, cmux.TLS())
		if err != nil {
			return nil, errors.Errorf("forwarding cmux https to guest port: %w", err)
		}
	}

	if cfg.EnableMagicHTTPForwarding {
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
