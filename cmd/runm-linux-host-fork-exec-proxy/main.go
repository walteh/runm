package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/mdlayher/vsock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/walteh/runm/linux/constants"

	vmmv1 "github.com/walteh/runm/proto/vmm/v1"
)

func main() {

	conn, err := grpc.NewClient(
		"passthrough:target",
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return vsock.Dial(2, constants.RunmHostServerVsockPort, nil)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		slog.Error("problem creating grpc client", "error", err)
		return
	}

	hostService := vmmv1.NewHostCallbackServiceClient(conn)

	// read all from stdin
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		slog.Error("problem reading stdin", "error", err)
		return
	}

	req := &vmmv1.ForkExecProxyRequest{}
	req.SetArgc(os.Args[0])
	req.SetArgv(os.Args[1:])
	req.SetEnv(os.Environ())
	req.SetStdin(stdin)

	resp, err := hostService.ForkExecProxy(context.Background(), req)
	if err != nil {
		slog.Error("problem calling hostService.ForkExecProxy", "error", err)
		return
	}

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		os.Stdout.Write(resp.GetStdout())
		wg.Done()
	}()

	go func() {
		os.Stderr.Write(resp.GetStderr())
		wg.Done()
	}()

	wg.Wait()

	os.Exit(int(resp.GetExitCode()))
}
