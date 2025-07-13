package env

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"

	"golang.org/x/sys/unix"

	"github.com/containerd/log"
	"github.com/sirupsen/logrus"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/linux/constants"
	"github.com/walteh/runm/pkg/logging"
)

func SetupOtelForNerdctl(ctx context.Context) (*slog.Logger, func() error, error) {
	return SetupLogForwardingToContainerd(ctx, "nerdctl", false)
}

// func redirectStderr(f *os.File) (func() error, error) {

// 	stderrFd := int(os.Stderr.Fd())
// 	oldfd, err := unix.Dup(stderrFd)
// 	if err != nil {
// 		return nil, errors.New("Failed to redirect stderr to file: " + err.Error())
// 	}

// 	err = unix.Dup2(int(f.Fd()), stderrFd)
// 	if err != nil {
// 		return nil, errors.New("Failed to redirect stderr to file: " + err.Error())
// 	}

// 	undo := func() error {
// 		undoErr := unix.Dup2(oldfd, stderrFd)
// 		unix.Close(oldfd)

// 		if undoErr != nil {
// 			return errors.New("Failed to reverse stderr redirection: " + err.Error())
// 		}

// 		return nil
// 	}

// 	return undo, nil
// }

// duplicateStderrToWriter duplicates stderr to both its original destination and the given writer
// This preserves containerd's ability to read stderr while also capturing panics
func DuplicateFdToWriter(fd int, writer io.Writer) (func() error, error) {
	if writer == nil {
		return func() error { return nil }, nil
	}

	// Create a pipe to capture stderr output
	r, w, err := os.Pipe()
	if err != nil {
		return nil, errors.New("Failed to create pipe: " + err.Error())
	}

	// Duplicate the original stderr so we can restore it later
	originalStderr, err := unix.Dup(fd)
	if err != nil {
		r.Close()
		w.Close()
		return nil, errors.New("Failed to duplicate original stderr: " + err.Error())
	}

	// Replace stderr with our pipe write end
	err = unix.Dup2(int(w.Fd()), fd)
	if err != nil {
		unix.Close(originalStderr)
		r.Close()
		w.Close()
		return nil, errors.New("Failed to redirect stderr to pipe: " + err.Error())
	}

	// Create a file from the original stderr fd
	originalStderrFile := os.NewFile(uintptr(originalStderr), fmt.Sprintf("original-fd-%d", fd))

	// Start a goroutine that tees the output to both destinations
	go func() {
		defer r.Close()
		defer w.Close()
		defer originalStderrFile.Close()

		// Create a multi-writer that writes to both original stderr and our writer
		multiWriter := io.MultiWriter(originalStderrFile, writer)

		// Copy everything from the pipe to both destinations
		io.Copy(multiWriter, r)
	}()

	undo := func() error {
		// Restore original stderr
		undoErr := unix.Dup2(originalStderr, fd)
		unix.Close(originalStderr)

		if undoErr != nil {
			return errors.New("Failed to restore original stderr: " + undoErr.Error())
		}

		return nil
	}

	return undo, nil
}

func SetupLogForwardingToContainerd(ctx context.Context, shimName string, redirectStderr bool) (*slog.Logger, func() error, error) {

	opts := []logging.LoggerOpt{
		logging.WithDelimiter(constants.VsockDelimitedLogProxyDelimiter),
		logging.WithEnableDelimiter(true),
	}

	rawWriterSock, err := net.Dial("unix", ShimRawWriterSockPath())
	if err != nil {
		slog.Error("Failed to dial log proxy socket", "error", err, "path", ShimRawWriterSockPath())
		return nil, nil, err
	}

	// Duplicate stderr to the raw writer socket to capture panic stack traces
	var stderrUndo func() error
	// DISABLED: stderr duplication breaks containerd communication, using containerd hack instead
	// if redirectStderr {
	// 	stderrUndo, err = duplicateStderrToWriter(rawWriterSock)
	// 	if err != nil {
	// 		slog.Error("Failed to duplicate stderr to raw writer socket", "error", err)
	// 	}
	// }

	opts = append(opts, logging.WithRawWriter(rawWriterSock))

	logProxySockDelim, err := net.Dial("unix", ShimDelimitedWriterSockPath())
	if err != nil {
		slog.Error("Failed to dial log proxy socket", "error", err, "path", ShimDelimitedWriterSockPath())
		return nil, nil, err
	}

	// opts = append(opts, logging.WithDelimitedLogWriter(logProxySockDelim))

	// attempt to listen on the port 5909
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", MagicHostOtlpGRPCPort()))
	if err == nil {
		listener.Close()
		l := logging.NewDefaultDevLogger(shimName, logProxySockDelim, opts...)
		l.Debug("logger created without otel, the host otel grpc port is free", "port", MagicHostOtlpGRPCPort())
		return l, func() error {
			if stderrUndo != nil {
				stderrUndo()
			}
			rawWriterSock.Close()
			logProxySockDelim.Close()
			return nil
		}, nil
	}

	otlpConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", MagicHostOtlpGRPCPort()))
	if err != nil {
		return nil, nil, err
	}

	otelInstances, err := logging.NewGRPCOtelInstances(ctx, otlpConn, shimName)
	if err != nil {
		return nil, nil, err
	}

	log.L = &logrus.Entry{
		Logger: logrus.StandardLogger(),
		Data:   make(log.Fields, 6),
	}

	return logging.NewDefaultDevLoggerWithOtel(ctx, shimName, logProxySockDelim, otelInstances, opts...), func() error {
		if stderrUndo != nil {
			stderrUndo()
		}
		rawWriterSock.Close()
		logProxySockDelim.Close()
		return otelInstances.Shutdown(ctx)
	}, nil
}
