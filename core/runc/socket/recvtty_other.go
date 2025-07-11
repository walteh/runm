//go:build !linux

package socket

import (
	"context"
	"net"

	"gitlab.com/tozd/go/errors"
)

func RecvttyProxy(ctx context.Context, path string, proxy net.Conn) error {
	return errors.New("not implemented")
}
