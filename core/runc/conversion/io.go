package conversion

import (
	"context"
	"net"

	"github.com/mdlayher/vsock"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/runtime"
	"github.com/walteh/runm/core/runc/stdio"

	runmv1 "github.com/walteh/runm/proto/v1"
)

func ConvertIOFromProto(ctx context.Context, req *runmv1.RuncIO) (runtime.IO, error) {
	return NewServerIOFromClient(ctx, req)
}

// func ConvertIOToProto(ctx context.Context, rvio runtime.IO) (*runmv1.RuncIO, error) {
// 	res := &runmv1.RuncIO{}

// 	switch iot := rvio.(type) {
// 	case *runtime.HostVsockProxyIo:
// 		rviod := &runmv1.RuncVsockIO{}
// 		rviod.SetStdinVsockPort(iot.StdinPort)
// 		rviod.SetStdoutVsockPort(iot.StdoutPort)
// 		rviod.SetStderrVsockPort(iot.StderrPort)
// 		rviod.SetVsockContextId(3)
// 		res.SetVsock(rviod)
// 	case *runtime.HostAllocatedStdio:
// 		rviod := &runmv1.RuncUnixIO{}
// 		rviod.SetStdinPath(iot.StdinPath)
// 		rviod.SetStdoutPath(iot.StdoutPath)
// 		rviod.SetStderrPath(iot.StderrPath)
// 		res.SetUnix(rviod)
// 	case *stdio.HostNullIo:
// 		res.SetNull(&runmv1.RuncNullIO{})
// 	default:
// 		return nil, errors.Errorf("io is not a vsock proxy io")
// 	}

// 	return res, nil
// }

func NewServerIOFromClient(ctx context.Context, rvio *runmv1.RuncIO) (runtime.IO, error) {
	var err error
	opts := []gorunc.IOOpt{
		func(o *gorunc.IOOption) {
			switch rvio.WhichIo() {
			case runmv1.RuncIO_Vsock_case:
				o.OpenStdin = rvio.GetVsock().GetStdinVsockPort() != 0
				o.OpenStdout = rvio.GetVsock().GetStdoutVsockPort() != 0
				o.OpenStderr = rvio.GetVsock().GetStderrVsockPort() != 0
			case runmv1.RuncIO_Unix_case:
				o.OpenStdin = rvio.GetUnix().GetStdinPath() != ""
				o.OpenStdout = rvio.GetUnix().GetStdoutPath() != ""
				o.OpenStderr = rvio.GetUnix().GetStderrPath() != ""
			}
		},
	}
	var forwarder stdio.ListenForwarders

	switch rvio.WhichIo() {
	case runmv1.RuncIO_Vsock_case:
		ctxId := rvio.GetVsock().GetVsockContextId()
		stdinPort := rvio.GetVsock().GetStdinVsockPort()
		stdoutPort := rvio.GetVsock().GetStdoutVsockPort()
		stderrPort := rvio.GetVsock().GetStderrVsockPort()
		dialFunc := func(_ context.Context, ctxId uint32, port uint64) (net.Conn, error) {
			return vsock.Dial(ctxId, uint32(port), nil)
		}
		listenFunc := func(_ context.Context, ctxId uint32, port uint64) (net.Listener, error) {
			return vsock.ListenContextID(ctxId, uint32(port), nil)
		}
		forwarder, err = stdio.NewVsockForwarder(ctx, ctxId, stdinPort, stdoutPort, stderrPort, dialFunc, listenFunc)
	case runmv1.RuncIO_Unix_case:
		stdinPath := rvio.GetUnix().GetStdinPath()
		stdoutPath := rvio.GetUnix().GetStdoutPath()
		stderrPath := rvio.GetUnix().GetStderrPath()
		forwarder, err = stdio.NewUnixForwarder(ctx, stdinPath, stdoutPath, stderrPath)
	case runmv1.RuncIO_Null_case:
		// no need to forward null io
		return gorunc.NewNullIO()
	default:
		return nil, errors.Errorf("unknown io type: %v", rvio.WhichIo())
	}
	if err != nil {
		return nil, err
	}

	guestIO, err := stdio.NewGuestIO(ctx, forwarder, opts...)
	if err != nil {
		return nil, err
	}

	return guestIO, nil
}
