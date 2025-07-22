//go:build !windows

package managerserver

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vmmv1 "github.com/walteh/runm/proto/vmm/v1"
)

var _ vmmv1.GuestManagementServiceServer = (*Server)(nil)

type Server struct {
}

func Register(s *grpc.Server) {
	vmmv1.RegisterGuestManagementServiceServer(s, &Server{})
}

// GuestReadiness implements vmmv1.GuestManagementServiceServer.
func (s *Server) GuestReadiness(context.Context, *vmmv1.GuestReadinessRequest) (*vmmv1.GuestReadinessResponse, error) {
	res := &vmmv1.GuestReadinessResponse{}
	res.SetReady(true)
	return res, nil
}

// GuestRunCommand implements vmmv1.GuestManagementServiceServer.
func (s *Server) GuestRunCommand(ctx context.Context, req *vmmv1.GuestRunCommandRequest) (*vmmv1.GuestRunCommandResponse, error) {
	res := &vmmv1.GuestRunCommandResponse{}
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	cmd := exec.CommandContext(ctx, req.GetArgc(), req.GetArgv()...)
	cmd.Stdin = bytes.NewReader(req.GetStdin())
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Chroot: req.GetChroot(),
	}
	envdat := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	for key, value := range req.GetEnvVars() {
		envdat = append(envdat, fmt.Sprintf("%s=%s", key, value))
	}
	cmd.Env = append(cmd.Env, envdat...)

	cmd.Dir = req.GetCwd()

	cmd.Env = append(os.Environ(), envdat...)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.SetExitCode(int32(exitErr.ExitCode()))
		} else {
			res.SetExitCode(int32(1))
		}
		res.SetStderr(stderr.Bytes())
		res.SetStdout(stdout.Bytes())
	} else {
		res.SetExitCode(int32(cmd.ProcessState.ExitCode()))
		res.SetStderr(stderr.Bytes())
		res.SetStdout(stdout.Bytes())
	}

	return res, nil

}

func formatTimeDiff(after, before uint64) (int64, string) {
	diff := int64(after) - int64(before)
	var diffString string

	if diff > (int64(time.Hour) * 1) {
		diffString = ">1h"
	} else {
		diffString = time.Duration(diff).String()
	}

	if diff > 0 {
		diffString = "(+) " + diffString
	} else {
		diffString = "(-) " + diffString
	}

	return diff, diffString
}

// GuestTimeSync implements vmmv1.GuestManagementServiceServer.
func (s *Server) GuestTimeSync(ctx context.Context, req *vmmv1.GuestTimeSyncRequest) (*vmmv1.GuestTimeSyncResponse, error) {
	res := &vmmv1.GuestTimeSyncResponse{}
	beforeNano := uint64(time.Now().UnixNano())
	requestedNano := uint64(req.GetUnixTimeNs())

	tv := unix.NsecToTimeval(int64(requestedNano))

	if err := unix.Settimeofday(&tv); err != nil {
		return nil, status.Errorf(codes.Internal, "unix.Settimeofday failed: %v", err)
	}

	afterNano := uint64(time.Now().Local().UnixNano())

	_, diffString := formatTimeDiff(afterNano, beforeNano)

	attrs := []slog.Attr{slog.GroupAttrs("unix_time_sync", []slog.Attr{
		slog.String("requested_time_utc", time.Unix(0, int64(requestedNano)).UTC().Format(time.RFC3339)),
		slog.String("real_before_time_utc", time.Unix(0, int64(beforeNano)).UTC().Format(time.RFC3339)),
		slog.String("real_after_time_utc", time.Unix(0, int64(afterNano)).UTC().Format(time.RFC3339)),
		slog.String("adjustment", diffString),
	}...)}

	res.SetPreviousTimeNs(beforeNano)

	if req.GetTimezone() != "" {
		slog.WarnContext(ctx, "timezone sync requested, but not implemented", "timezone", req.GetTimezone())
	}

	slog.LogAttrs(ctx, slog.LevelInfo, "time sync completed", attrs...)

	return res, nil
}
