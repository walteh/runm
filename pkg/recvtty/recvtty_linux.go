//go:build linux

package recvtty

import (
	"os"

	"github.com/opencontainers/runc/libcontainer/utils"
)

func recvFile(socket *os.File) (_ *os.File, Err error) {
	return utils.RecvFile(socket)
}
