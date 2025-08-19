//go:build !windows

package task

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/v2/pkg/shim"
	"github.com/containerd/log"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/pkg/grpcerr"
	"github.com/walteh/runm/pkg/logging/otel"
	runmv1 "github.com/walteh/runm/proto/v1"
)

var (
	_ runmv1.ShimServiceServer = (*service)(nil)
)

func (s *service) serveGrpc(ctx context.Context, cid string) (func() error, func() error, error) {

	opts := []grpc.ServerOption{}
	if os.Getenv("RUNM_USING_TEST_ENV") != "1" {
		opts = append(opts, otel.GetGrpcServerOpts()...)
		opts = append(opts, grpcerr.GetGrpcServerOptsCtx(ctx)...)
	}

	grpcServer := grpc.NewServer(
		opts...,
	)

	runmv1.RegisterShimServiceServer(grpcServer, s)

	runmSocketAddress := filepath.Join("tmp", "runm", cid[:16], "runm-shim.sock")

	os.Remove(runmSocketAddress)

	// if cl, err := os.Create(runmSocketAddress); err != nil {
	// 	return nil, nil, errors.Errorf("creating runm socket: %w", err)
	// } else {
	// 	cl.Close()
	// }

	os.MkdirAll(filepath.Dir(runmSocketAddress), 0755)

	listener, err := net.Listen("unix", runmSocketAddress)
	if err != nil {
		return nil, nil, errors.Errorf("listening on runm socket: %w", err)
	}

	s.shutdown.RegisterCallback(func(context.Context) error {
		if err := shim.RemoveSocket(runmSocketAddress); err != nil {
			slog.Error("failed to remove primary socket", "error", err)
		}

		return nil
	})

	return func() error {
			return grpcServer.Serve(listener)
		}, func() error {
			if err := listener.Close(); err != nil {
				return errors.Errorf("closing listener: %w", err)
			}
			return nil
		}, nil
}

// ShimFeatures implements runmv1.ShimServiceServer.
func (s *service) ShimFeatures(ctx context.Context, r *runmv1.ShimFeaturesRequest) (*runmv1.ShimFeaturesResponse, error) {
	features, err := s.creator.Features(ctx)
	if err != nil {
		return nil, err
	}
	rawJson, err := json.Marshal(features)
	if err != nil {
		return nil, err
	}
	resp := &runmv1.ShimFeaturesResponse{}
	resp.SetRawJson(rawJson)
	return resp, nil
}

func (s *service) ShimKill(ctx context.Context, r *runmv1.ShimKillRequest) (*runmv1.ShimKillResponse, error) {
	container, err := s.getContainer(s.primaryContainerId)
	if err != nil {
		return nil, err
	}

	init, err := container.Process("")
	if err != nil {
		return nil, err
	}

	if err := init.Runtime().Delete(ctx, "", &gorunc.DeleteOpts{
		Force: true,
	}); err != nil {
		log.G(ctx).WithError(err).Warn("failed to remove runc container")
	}

	resp := &runmv1.ShimKillResponse{}
	resp.SetInitPid(int64(init.Pid()))
	return resp, nil
}
