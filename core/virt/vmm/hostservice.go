//go:build !windows

package vmm

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"

	vmmv1 "github.com/walteh/runm/proto/vmm/v1"
)

func (rvm *RunningVM[VM]) ForkExecProxy(ctx context.Context, r *vmmv1.ForkExecProxyRequest) (*vmmv1.ForkExecProxyResponse, error) {
	slog.InfoContext(ctx, "forking exec proxy", "argc", r.GetArgc(), "argv", r.GetArgv())

	stdoutz := bytes.NewBuffer(nil)
	stderrz := bytes.NewBuffer(nil)
	cmd := exec.CommandContext(ctx, r.GetArgc(), r.GetArgv()...)
	cmd.Stdout = stdoutz
	cmd.Stderr = stderrz
	cmd.Stdin = bytes.NewBuffer(r.GetStdin())
	cmd.Env = r.GetEnv()

	resp := &vmmv1.ForkExecProxyResponse{}

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			resp.SetExitCode(int32(exitErr.ExitCode()))
			resp.SetStdout(stdoutz.Bytes())
			resp.SetStderr(stderrz.Bytes())
			return resp, nil
		}
		return nil, err
	}

	resp.SetStdout(stdoutz.Bytes())
	resp.SetStderr(stderrz.Bytes())
	resp.SetExitCode(int32(cmd.ProcessState.ExitCode()))

	return resp, nil
}
