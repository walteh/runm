//go:build !windows

package grpcruntime

import (
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runtime.GuestManagement = &GRPCClientRuntime{}

// Readiness implements runtime.GuestManagement.
func (me *GRPCClientRuntime) Readiness(ctx context.Context) error {
	_, err := me.guestManagmentService.GuestReadiness(ctx, &runmv1.GuestReadinessRequest{})
	if err != nil {
		return errors.Errorf("failed to get readiness: %w", err)
	}
	return nil
}

// RunCommand implements runtime.GuestManagement.
func (me *GRPCClientRuntime) RunCommand(ctx context.Context, cmd *exec.Cmd) error {
	cmdreq := &runmv1.GuestRunCommandRequest{}

	stdin, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		return errors.Errorf("failed to read stdin: %w", err)
	}
	cmdreq.SetStdin(stdin)
	cmdreq.SetArgc(strconv.Itoa(len(cmd.Args)))
	cmdreq.SetArgv(cmd.Args)
	envVars := make(map[string]string)
	for _, env := range cmd.Env {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			envVars[parts[0]] = parts[1]
		}
	}
	cmdreq.SetEnvVars(envVars)
	cmdreq.SetCwd(cmd.Dir)
	if cmd.SysProcAttr != nil {
		cmdreq.SetChroot(cmd.SysProcAttr.Chroot)
	}
	_, err = me.guestManagmentService.GuestRunCommand(ctx, cmdreq)
	if err != nil {
		return errors.Errorf("failed to run command: %w", err)
	}
	return nil
}

// TimeSync implements runtime.GuestManagement.
func (me *GRPCClientRuntime) TimeSync(ctx context.Context, unixTimeNs uint64, timezone string) error {
	tsreq := &runmv1.GuestTimeSyncRequest{}
	tsreq.SetUnixTimeNs(unixTimeNs)
	tsreq.SetTimezone(timezone)
	_, err := me.guestManagmentService.GuestTimeSync(ctx, tsreq)
	if err != nil {
		return errors.Errorf("failed to time sync: %w", err)
	}
	return nil
}
