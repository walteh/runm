package dapmux

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	dapmuxv1 "github.com/walteh/runm/proto/dapmux/v1"
)

type Client struct {
	proxyAddr  string
	delvePort  int
	target     *dapmuxv1.Target
	delveCmd   *exec.Cmd
	grpcClient dapmuxv1.DAPMuxServiceClient
	grpcConn   *grpc.ClientConn
}

func NewClient(proxyAddr string, delvePort int, logger *slog.Logger) (*Client, error) {

	return &Client{
		proxyAddr: proxyAddr,
		delvePort: delvePort,
	}, nil
}

func (c *Client) connectGRPC() error {
	if c.grpcConn != nil {
		return nil
	}

	conn, err := grpc.NewClient(c.proxyAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}

	c.grpcConn = conn
	c.grpcClient = dapmuxv1.NewDAPMuxServiceClient(conn)
	return nil
}

func (c *Client) StartDelve(ctx context.Context) error {
	if c.delveCmd != nil && c.delveCmd.Process != nil {
		return nil // Already running
	}

	cmd := exec.CommandContext(ctx, "dlv",
		"attach", strconv.Itoa(os.Getpid()),
		"--headless",
		"--api-version=2",
		fmt.Sprintf("--listen=:%d", c.delvePort),
		"--accept-multiclient",
	)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Delve: %w", err)
	}

	c.delveCmd = cmd
	slog.Info("Delve started", "port", c.delvePort, "pid", cmd.Process.Pid)

	// Wait for Delve to be ready
	return c.waitForDelve(ctx)
}

func (c *Client) waitForDelve(ctx context.Context) error {
	addr := fmt.Sprintf("localhost:%d", c.delvePort)
	for i := 0; i < 30; i++ {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			slog.Debug("Delve ready", "addr", addr)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("Delve not ready on %s", addr)
}

func (c *Client) Register(ctx context.Context) (string, error) {
	if err := c.connectGRPC(); err != nil {
		return "", err
	}

	hostname, _ := os.Hostname()
	location := "host"
	if hostname != "localhost" && hostname != "" {
		location = hostname
	}

	req := &dapmuxv1.RegisterTargetRequest_builder{
		ProcessName: filepath.Base(os.Args[0]),
		DelveAddr:   fmt.Sprintf("localhost:%d", c.delvePort),
		Location:    location,
		ProcessId:   int32(os.Getpid()),
		Args:        os.Args,
	}

	resp, err := c.grpcClient.RegisterTarget(ctx, req.Build())
	if err != nil {
		return "", err
	}

	c.target = resp.GetTarget()
	slog.Info("Registered with proxy", "target_id", c.target.GetId())
	return c.target.GetId(), nil
}

func (c *Client) Unregister(ctx context.Context) error {
	if c.target == nil {
		return nil
	}

	if c.grpcClient != nil {
		req := &dapmuxv1.UnregisterTargetRequest_builder{
			TargetId: c.target.GetId(),
		}
		_, err := c.grpcClient.UnregisterTarget(ctx, req.Build())
		if err != nil {
			return err
		}
	}

	c.target = nil
	return nil
}

func (c *Client) StopDelve() error {
	if c.delveCmd == nil || c.delveCmd.Process == nil {
		return nil
	}

	if err := c.delveCmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- c.delveCmd.Wait()
	}()

	select {
	case <-done:
		slog.Info("Delve stopped gracefully")
	case <-time.After(5 * time.Second):
		slog.Warn("Force killing Delve")
		c.delveCmd.Process.Kill()
		<-done
	}

	c.delveCmd = nil
	return nil
}

func (c *Client) Close() error {
	if c.grpcConn != nil {
		c.grpcConn.Close()
	}
	return c.StopDelve()
}

func (c *Client) IsRegistered() bool {
	return c.target != nil
}

// Convenience functions
func AutoRegisterFromEnv() error {
	proxyAddr := os.Getenv("DAP_PROXY_ADDR")
	if proxyAddr == "" {
		return nil
	}

	port := 2345
	if portStr := os.Getenv("DAP_DELVE_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	client, err := NewClient(proxyAddr, port, nil)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.StartDelve(ctx); err != nil {
		return err
	}

	_, err = client.Register(ctx)
	return err
}

func InitHook() {
	if os.Getenv("DAP_AUTO_REGISTER") != "true" {
		return
	}

	go func() {
		time.Sleep(2 * time.Second)
		if err := AutoRegisterFromEnv(); err != nil {
			fmt.Fprintf(os.Stderr, "DAP auto-register failed: %v\n", err)
		}
	}()
}
