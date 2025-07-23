//go:build !windows

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	slogctx "github.com/veqryn/slog-context"
	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/pkg/logging/otel"
	vmfusev1 "github.com/walteh/runm/proto/vmfuse/v1"
	"github.com/walteh/runm/test/integration/env"
)

var (
	serverAddr        string
	logAddr           string
	enableTestLogging bool
	help              bool
)

func init() {
	flag.StringVar(&serverAddr, "address", "", "vmfused server address")
	flag.StringVar(&logAddr, "log-address", "", "log address")
	flag.BoolVar(&enableTestLogging, "enable-test-logging", false, "enable test logging")
	flag.BoolVar(&help, "help", false, "show help")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [arguments]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nCommands:\n")
		fmt.Fprintf(os.Stderr, "  mount -t TYPE SOURCES TARGET    Mount sources to target\n")
		fmt.Fprintf(os.Stderr, "  umount TARGET                   Unmount target\n")
		fmt.Fprintf(os.Stderr, "  list                            List active mounts\n")
		fmt.Fprintf(os.Stderr, "  status TARGET                   Show status of mount\n")
		fmt.Fprintf(os.Stderr, "\nMount Types:\n")
		fmt.Fprintf(os.Stderr, "  bind                            Bind mount (single source)\n")
		fmt.Fprintf(os.Stderr, "  overlay                         Overlay mount (lower,upper[,work])\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Bind mount a directory\n")
		fmt.Fprintf(os.Stderr, "  %s mount -t bind /host/dir /Volumes/mounted\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Overlay mount with lower and upper dirs\n")
		fmt.Fprintf(os.Stderr, "  %s mount -t overlay /layers/lower,/layers/upper /Volumes/overlay\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Unmount\n")
		fmt.Fprintf(os.Stderr, "  %s umount /Volumes/mounted\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # List all mounts\n")
		fmt.Fprintf(os.Stderr, "  %s list\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Check status\n")
		fmt.Fprintf(os.Stderr, "  %s status /Volumes/mounted\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
	}

	flag.Parse()
}

func init() {
	if serverAddr == "" {
		serverAddr = os.Getenv("VMFUSED_ADDRESS")
	}
	if !enableTestLogging {
		enableTestLogging = os.Getenv("ENABLE_TEST_LOGGING") == "1"
	}
}

func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	if help {
		flag.Usage()
		exitCode = 0
		return
	}

	if serverAddr == "" {
		fmt.Fprintf(os.Stderr, "[vmfusectl] ERROR: server address is required\n\n")
		flag.Usage()
		exitCode = 1
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "[vmfusectl] ERROR: command required\n\n")
		flag.Usage()
		exitCode = 1
		return
	}

	ctx := context.Background()

	fmt.Fprintf(os.Stderr, "[vmfusectl] INFO: enableTestLogging: %v\n", enableTestLogging)

	if enableTestLogging {

		logger, cleanup, err := env.SetupLogForwardingToContainerd(ctx, "vmfusectl", false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[vmfusectl] ERROR: setting up log forwarding: %v\n", err)
			exitCode = 1
			return
		}
		defer cleanup()

		ctx = slogctx.NewCtx(ctx, logger)

		slog.InfoContext(ctx, "vmfusectl test logging enabled")
	} else {
		logger := logging.NewDefaultDevLogger("vmfusectl", os.Stdout)
		ctx = slogctx.NewCtx(ctx, logger)
	}

	// ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	// defer cancel()
	var dialFunc func(ctx context.Context, addr string) (net.Conn, error)

	grpcAddr := serverAddr
	if strings.HasPrefix(grpcAddr, "unix://") {

		dialFunc = func(ctx context.Context, addr string) (net.Conn, error) {
			unixAddr, err := net.ResolveUnixAddr("unix", strings.TrimPrefix(serverAddr, "unix://"))
			if err != nil {
				return nil, err
			}
			return net.DialUnix("unix", nil, unixAddr)
		}
		grpcAddr = "passthrough:target"

		slog.InfoContext(ctx, "using passthrough dialer", "grpcAddr", grpcAddr, "unixAddr", strings.TrimPrefix(serverAddr, "unix://"))
	}

	conn, err := grpc.NewClient(grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpcerr.GetGrpcClientOptsCtx(ctx),
		otel.GetGrpcClientOpts(),
		grpc.WithContextDialer(dialFunc),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[vmfusectl] ERROR: connecting to vmfused: %v\n", err)
		exitCode = 1
		return
	}

	client := vmfusev1.NewVmfuseServiceClient(conn)

	// // Connect to vmfused
	// client, conn, err := connectToVmfused(ctx)
	// if err != nil {
	// 	fmt.Fprintf(os.Stderr, "Error connecting to vmfused: %v\n", err)
	// 	fmt.Fprintf(os.Stderr, "Make sure vmfused is running: vmfused -listen %s\n", *serverAddr)
	// 	exitCode = 1
	// 	return
	// }
	defer conn.Close()

	cmd := args[0]

	switch cmd {
	case "mount":
		err = handleMount(ctx, client, args[1:])
	case "umount", "unmount":
		err = handleUmount(ctx, client, args[1:])
	case "list":
		err = handleList(ctx, client, args[1:])
	case "status":
		err = handleStatus(ctx, client, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "[vmfusectl] ERROR: unknown command '%s'\n\n", cmd)
		flag.Usage()
		exitCode = 1
		return
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "[vmfusectl] ERROR: %v\n", err)
		exitCode = 1
		return
	}
}

func connectToVmfused(ctx context.Context, serverAddr string) (vmfusev1.VmfuseServiceClient, *grpc.ClientConn, error) {
	// Handle different address formats
	dialAddr := serverAddr
	if strings.HasPrefix(serverAddr, "unix://") {
		dialAddr = strings.TrimPrefix(serverAddr, "unix://")
		// For Unix sockets, need to use the unix dialer
		conn, err := grpc.DialContext(ctx, "unix:"+dialAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return nil, nil, err
		}
		return vmfusev1.NewVmfuseServiceClient(conn), conn, nil
	}

	// TCP connection
	conn, err := grpc.DialContext(ctx, dialAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, err
	}
	return vmfusev1.NewVmfuseServiceClient(conn), conn, nil
}

func handleMount(ctx context.Context, client vmfusev1.VmfuseServiceClient, args []string) error {
	// Parse mount arguments
	var mountType string = "bind" // default
	var sources string
	var target string
	var memoryMib uint64 = 128
	var cpus uint32 = 2

	// Simple argument parsing for mount command
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-t":
			if i+1 >= len(args) {
				return fmt.Errorf("-t requires mount type")
			}
			mountType = args[i+1]
			i++ // skip next arg
		case "--memory":
			if i+1 >= len(args) {
				return fmt.Errorf("--memory requires value")
			}
			var memoryMibVal int
			if _, err := fmt.Sscanf(args[i+1], "%d", &memoryMibVal); err != nil {
				return fmt.Errorf("invalid --memory value: %v", err)
			}
			memoryMib = uint64(memoryMibVal)
			i++ // skip next arg
		case "--cpus":
			if i+1 >= len(args) {
				return fmt.Errorf("--cpus requires value")
			}
			var cpusVal int
			if _, err := fmt.Sscanf(args[i+1], "%d", &cpusVal); err != nil {
				return fmt.Errorf("invalid --cpus value: %v", err)
			}
			cpus = uint32(cpusVal)
			i++ // skip next arg
		case "-enable-test-logging":
			continue
		default:
			if sources == "" {
				sources = args[i]
			} else if target == "" {
				target = args[i]
			} else {
				return fmt.Errorf("unexpected argument '%s'", args[i])
			}
		}
	}

	if sources == "" || target == "" {
		return fmt.Errorf("mount requires sources and target\nUsage: vmfusectl mount [-t TYPE] [--memory MEM] [--cpus N] SOURCES TARGET")
	}

	// Parse sources
	sourceList := strings.Split(sources, ",")

	req := vmfusev1.NewMountRequest(&vmfusev1.MountRequest_builder{
		MountType: mountType,
		Sources:   sourceList,
		Target:    target,
		VmConfig: vmfusev1.NewVmConfig(&vmfusev1.VmConfig_builder{
			MemoryMib:      memoryMib,
			Cpus:           cpus,
			TimeoutSeconds: 300, // 5 minutes
		}),
	})

	fmt.Printf("Mounting %s (%s) to %s...\n", sources, mountType, target)

	resp, err := client.Mount(ctx, req)
	if err != nil {
		return fmt.Errorf("mount failed: %w", err)
	}

	fmt.Printf("Mount initiated successfully:\n")
	fmt.Printf("  Mount ID: %s\n", resp.GetMountId())
	fmt.Printf("  VM ID: %s\n", resp.GetVmId())
	fmt.Printf("  Status: %s\n", resp.GetStatus().String())
	fmt.Printf("\nUse 'vmfusectl status %s' to check progress\n", target)

	return nil
}

func handleUmount(ctx context.Context, client vmfusev1.VmfuseServiceClient, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("umount requires exactly one target\nUsage: vmfusectl umount TARGET")
	}

	target := args[0]

	req := vmfusev1.NewUnmountRequest(&vmfusev1.UnmountRequest_builder{
		Target: target,
	})

	fmt.Printf("Unmounting %s...\n", target)

	_, err := client.Unmount(ctx, req)
	if err != nil {
		return fmt.Errorf("unmount failed: %w", err)
	}

	fmt.Printf("Unmount initiated successfully\n")

	return nil
}

func handleList(ctx context.Context, client vmfusev1.VmfuseServiceClient, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("list command takes no arguments")
	}

	resp, err := client.List(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("list failed: %w", err)
	}

	if len(resp.GetMounts()) == 0 {
		fmt.Println("No active mounts")
		return nil
	}

	fmt.Printf("Active mounts:\n\n")
	for _, mount := range resp.GetMounts() {
		fmt.Printf("Target: %s\n", mount.GetTarget())
		fmt.Printf("  Type: %s\n", mount.GetMountType())
		fmt.Printf("  Sources: %s\n", strings.Join(mount.GetSources(), ", "))
		fmt.Printf("  Status: %s\n", mount.GetStatus().String())
		fmt.Printf("  Mount ID: %s\n", mount.GetMountId())
		fmt.Printf("  VM ID: %s\n", mount.GetVmId())
		if mount.GetErrorMessage() != "" {
			fmt.Printf("  Error: %s\n", mount.GetErrorMessage())
		}
		fmt.Printf("\n")
	}

	return nil
}

func handleStatus(ctx context.Context, client vmfusev1.VmfuseServiceClient, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("status requires exactly one target\nUsage: vmfusectl status TARGET")
	}

	target := args[0]

	req := vmfusev1.NewStatusRequest(&vmfusev1.StatusRequest_builder{
		Target: &target, // This will set the oneof field
	})

	resp, err := client.Status(ctx, req)
	if err != nil {
		return fmt.Errorf("status failed: %w", err)
	}

	mount := resp.GetMountInfo()
	fmt.Printf("Mount Status:\n")
	fmt.Printf("  Target: %s\n", mount.GetTarget())
	fmt.Printf("  Type: %s\n", mount.GetMountType())
	fmt.Printf("  Sources: %s\n", strings.Join(mount.GetSources(), ", "))
	fmt.Printf("  Status: %s\n", mount.GetStatus().String())
	fmt.Printf("  Mount ID: %s\n", mount.GetMountId())
	fmt.Printf("  VM ID: %s\n", mount.GetVmId())
	if mount.GetNfsHostPort() != 0 {
		fmt.Printf("  NFS Host Port: %d\n", mount.GetNfsHostPort())
	}
	if mount.GetErrorMessage() != "" {
		fmt.Printf("  Error: %s\n", mount.GetErrorMessage())
	}

	createdTime := time.Unix(0, mount.GetCreatedAt())
	fmt.Printf("  Created: %s\n", createdTime.Format(time.RFC3339))

	return nil
}
