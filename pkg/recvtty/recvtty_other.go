//go:build !linux

package recvtty

import (
	"os"

	"gitlab.com/tozd/go/errors"
)

func recvFile(socket *os.File) (_ *os.File, Err error) {
	return nil, errors.New("not implemented")
}
