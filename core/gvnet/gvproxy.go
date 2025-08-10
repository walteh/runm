package gvnet

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"

	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
	"github.com/soheilhy/cmux"
	"gitlab.com/tozd/go/errors"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
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
	EnableMagicNFSForwarding   bool   // enable nfs forwarding

	MTU int // set the MTU, default is 1500

	WorkingDir                       string            // working directory
	ExtraHostToGuestTCPPortMappings  map[uint16]uint16 // extra port mappings to forward
	ExtraHostTCPPortsToExposeToGuest map[uint16]uint16 // extra tcp ports to expose to guest
}

func GvproxyVersion() string {
	return types.NewVersion("gvnet").String()
}

type gvproxy struct {
	netdev    *virtio.VirtioNet
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

	// Setup reverse port forwarding (host ports exposed to guest)
	if err := p.setupReversePortForwarding(tg, ctx, p.stack); err != nil {
		return errors.Errorf("setting up reverse port forwarding: %w", err)
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

	extraPortMappings := make(map[string]string)
	if cfg.ExtraHostToGuestTCPPortMappings != nil {
		for k, v := range cfg.ExtraHostToGuestTCPPortMappings {
			extraPortMappings[fmt.Sprintf("%s:%d", LOCAL_HOST_IP, k)] = fmt.Sprintf("%s:%d", VIRTUAL_GUEST_IP, v)
		}
	}

	slog.InfoContext(ctx, "extra port mappings", "extraPortMappings", extraPortMappings)

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
		Forwards:         extraPortMappings,
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

	if p.cfg.EnableMagicNFSForwarding {
		// NFS traffic gets everything else (must be last specific matcher before Any())
		err = m.ForwardCMUXMatchToGuestPort(ctx, stack, 2049, NFSMatcher())
		if err != nil {
			return nil, errors.Errorf("forwarding cmux nfs to guest port: %w", err)
		}
	}

	// route everything else to port 80 (only if NFS forwarding is disabled)
	err = m.ForwardCMUXMatchToGuestPort(ctx, stack, 80, cmux.Any())
	if err != nil {
		return nil, errors.Errorf("forwarding cmux match to guest port: %w", err)
	}

	return m, nil
}

// setupReversePortForwarding creates TCP listeners on guest stack that forward to host ports
func (p *gvproxy) setupReversePortForwarding(tg *taskgroup.TaskGroup, ctx context.Context, stack *stack.Stack) error {
	if p.cfg.ExtraHostTCPPortsToExposeToGuest == nil {
		return nil
	}

	for guestPort, hostPort := range p.cfg.ExtraHostTCPPortsToExposeToGuest {
		if err := p.setupReversePortForward(tg, ctx, stack, guestPort, hostPort); err != nil {
			return errors.Errorf("setting up reverse port forward %d->%d: %w", guestPort, hostPort, err)
		}
	}

	return nil
}

// setupReversePortForward sets up a single reverse port forward
func (p *gvproxy) setupReversePortForward(tg *taskgroup.TaskGroup, ctx context.Context, stack *stack.Stack, guestPort, hostPort uint16) error {
	hostAddr := fmt.Sprintf("127.0.0.1:%d", hostPort)

	// Create guest TCP address to bind to
	guestTCPAddr := tcpip.FullAddress{
		NIC:  1, // Default NIC ID
		Port: guestPort,
	}

	// Create gonet listener on guest stack
	listener, err := gonet.ListenTCP(stack, guestTCPAddr, ipv4.ProtocolNumber)
	if err != nil {
		return errors.Errorf("creating gonet TCP listener for port %d: %w", guestPort, err)
	}

	taskName := fmt.Sprintf("reverse-forward-%d-%d", guestPort, hostPort)
	
	// Register closer for cleanup
	tg.RegisterCloserWithName(taskName+"-listener", listener)

	// Start forwarding task
	tg.GoWithName(taskName, func(ctx context.Context) error {
		slog.InfoContext(ctx, "starting reverse port forward",
			"guest_port", guestPort,
			"host_port", hostPort,
			"host_addr", hostAddr)

		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return nil // graceful shutdown
				default:
					return errors.Errorf("accepting connection on guest port %d: %w", guestPort, err)
				}
			}

			// Handle connection in goroutine
			go func(guestConn net.Conn) {
				defer guestConn.Close()

				// Dial host port
				hostConn, err := net.Dial("tcp", hostAddr)
				if err != nil {
					slog.ErrorContext(ctx, "failed to dial host port",
						"host_addr", hostAddr,
						"error", err)
					return
				}
				defer hostConn.Close()

				// Bidirectional copy
				done := make(chan struct{}, 2)

				go func() {
					_, _ = io.Copy(hostConn, guestConn)
					done <- struct{}{}
				}()

				go func() {
					_, _ = io.Copy(guestConn, hostConn)
					done <- struct{}{}
				}()

				// Wait for either direction to finish
				<-done
			}(conn)
		}
	})

	return nil
}

const (
	rpcMsgTypeCall = 0
	rpcVersion     = 2
	nfsProg        = 100003
)

// NFS returns a cmux.Matcher that matches NFS (any version) ONC RPC calls.
func NFSMatcher() cmux.Matcher {
	return func(r io.Reader) bool {
		var rm [4]byte
		if _, err := io.ReadFull(r, rm[:]); err != nil {
			return false
		}
		fragLen := binary.BigEndian.Uint32(rm[:]) & 0x7FFFFFFF
		if fragLen < 24 { // xid(4)+type(4)+rpcver(4)+prog(4)+vers(4)+proc(4) = 24
			return false
		}

		hdr := make([]byte, 24)
		if _, err := io.ReadFull(r, hdr); err != nil {
			return false
		}

		msgType := binary.BigEndian.Uint32(hdr[4:8])
		if msgType != rpcMsgTypeCall {
			return false
		}
		if binary.BigEndian.Uint32(hdr[8:12]) != rpcVersion {
			return false
		}
		prog := binary.BigEndian.Uint32(hdr[12:16])
		return prog == nfsProg
	}
}
