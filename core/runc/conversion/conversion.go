package conversion

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"gitlab.com/tozd/go/errors"

	gorunc "github.com/containerd/go-runc"

	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

func ConvertCgroupEventFromProto(event *runmv1.CgroupEvent) runtime.CgroupEvent {
	return runtime.CgroupEvent{
		High:    event.GetHigh(),
		Max:     event.GetMax(),
		OOM:     event.GetOom(),
		OOMKill: event.GetOomKill(),
		Low:     event.GetLow(),
	}
}

func ConvertStatsFromProto(stats *runmv1.RuncStats) (*gorunc.Stats, error) {
	var runcStats gorunc.Stats
	if err := json.Unmarshal(stats.GetRawJson(), &runcStats); err != nil {
		return nil, err
	}
	return &runcStats, nil
}

// convertStats converts runc.Stats to runmv1.RuncStats
func ConvertStatsToProto(stats *gorunc.Stats) (*runmv1.RuncStats, error) {
	rawJson, err := json.Marshal(stats)
	if err != nil {
		return nil, err
	}
	resp := &runmv1.RuncStats{}
	resp.SetRawJson(rawJson)
	return resp, nil
}

func ConvertCreateOptsFromProto(ctx context.Context, opts *runmv1.RuncCreateOptions, state runtime.ServerStateGetter) (*gorunc.CreateOpts, error) {
	var err error
	files := make([]*os.File, len(opts.GetExtraFiles()))
	for i, file := range opts.GetExtraFiles() {
		files[i], err = os.Open(file)
		if err != nil {
			return nil, err
		}
	}

	var io runtime.IO
	var cs runtime.ConsoleSocket
	var ok bool

	if opts.GetIoReferenceId() != "" {
		io, ok = state.GetOpenIO(opts.GetIoReferenceId())
		if !ok {
			return nil, errors.Errorf("io not found")
		}
	}

	if opts.GetConsoleReferenceId() != "" {
		cs, ok = state.GetOpenConsole(opts.GetConsoleReferenceId())
		if !ok {
			return nil, errors.Errorf("console not found")
		}
	}

	return &gorunc.CreateOpts{
		PidFile:       opts.GetPidFile(),
		IO:            io,
		NoPivot:       opts.GetNoPivot(),
		NoNewKeyring:  opts.GetNoNewKeyring(),
		ConsoleSocket: cs,
		Detach:        opts.GetDetach(),
		ExtraFiles:    files,
		Started:       make(chan int),
	}, nil
}

func ConvertCreateOptsToProto(ctx context.Context, opts *gorunc.CreateOpts) (*runmv1.RuncCreateOptions, error) {

	// for now panic if we see extra files, we shouldnt see any but they are not hanlded
	if len(opts.ExtraFiles) > 0 {
		panic("extra files not handled e2e") // removing this will pass them through but will not work
	}

	files := make([]string, len(opts.ExtraFiles))
	for i, file := range opts.ExtraFiles {
		files[i] = file.Name()
	}

	res := &runmv1.RuncCreateOptions{}
	res.SetPidFile(opts.PidFile)
	res.SetNoPivot(opts.NoPivot)
	res.SetNoNewKeyring(opts.NoNewKeyring)
	res.SetDetach(opts.Detach)
	res.SetExtraFiles(files)

	if opts.IO != nil {
		ioz, ok := opts.IO.(runtime.ReferableByReferenceId)
		if !ok {
			return nil, errors.Errorf("io '%T' is not a referable by reference id", opts.IO)
		}
		res.SetIoReferenceId(ioz.GetReferenceId())
	}

	if opts.ConsoleSocket != nil {
		csz, ok := opts.ConsoleSocket.(runtime.ReferableByReferenceId)
		if !ok {
			return nil, errors.Errorf("console socket '%T' is not a referable by reference id", opts.ConsoleSocket)
		}
		res.SetConsoleReferenceId(csz.GetReferenceId())
	}

	return res, nil
}

func ConvertExecOptsFromProto(opts *runmv1.RuncExecOptions, state runtime.ServerStateGetter) (*gorunc.ExecOpts, error) {

	var io runtime.IO
	var cs runtime.ConsoleSocket
	var ok bool

	if opts.GetIoReferenceId() != "" {
		io, ok = state.GetOpenIO(opts.GetIoReferenceId())
		if !ok {
			return nil, errors.Errorf("io not found")
		}
	}

	if opts.GetConsoleReferenceId() != "" {
		cs, ok = state.GetOpenConsole(opts.GetConsoleReferenceId())
		if !ok {
			return nil, errors.Errorf("console not found")
		}
	}

	return &gorunc.ExecOpts{
		PidFile:       opts.GetPidFile(),
		IO:            io,
		Detach:        opts.GetDetach(),
		ConsoleSocket: cs,
		ExtraArgs:     opts.GetExtraArgs(),
		// Started:       make(chan int),
	}, nil
}

func ConvertExecOptsToProto(opts *gorunc.ExecOpts) (*runmv1.RuncExecOptions, error) {

	res := &runmv1.RuncExecOptions{}
	res.SetPidFile(opts.PidFile)

	res.SetDetach(opts.Detach)
	res.SetExtraArgs(opts.ExtraArgs)

	if opts.IO != nil {
		ioz, ok := opts.IO.(runtime.ReferableByReferenceId)
		if !ok {
			return nil, errors.Errorf("io '%T' is not a referable by reference id", opts.IO)
		}
		res.SetIoReferenceId(ioz.GetReferenceId())
	}

	if opts.ConsoleSocket != nil {
		csz, ok := opts.ConsoleSocket.(runtime.ReferableByReferenceId)
		if !ok {
			return nil, errors.Errorf("console socket '%T' is not a referable by reference id", opts.ConsoleSocket)
		}
		res.SetConsoleReferenceId(csz.GetReferenceId())
	}
	return res, nil
}

func ConvertKillOptsFromProto(opts *runmv1.RuncKillOptions) *gorunc.KillOpts {
	out := &gorunc.KillOpts{
		All:       opts.GetAll(),
		ExtraArgs: opts.GetExtraArgs(),
	}
	return out
}

func ConvertKillOptsToProto(opts *gorunc.KillOpts) *runmv1.RuncKillOptions {
	out := &runmv1.RuncKillOptions{}
	out.SetAll(opts.All)
	out.SetExtraArgs(opts.ExtraArgs)
	return out
}

// checkpoint in out

// restore in out
func ConvertRestoreOptsFromProto(opts *runmv1.RuncRestoreOptions) *gorunc.RestoreOpts {
	panic(runtime.ReflectNotImplementedError())
}

func ConvertRestoreOptsToProto(opts *gorunc.RestoreOpts) *runmv1.RuncRestoreOptions {
	panic(runtime.ReflectNotImplementedError())
}

func ConvertContainerToProto(container *gorunc.Container) (*runmv1.RuncContainer, error) {
	output := &runmv1.RuncContainer{}
	output.SetId(container.ID)
	output.SetPid(int32(container.Pid))
	output.SetStatus(container.Status)
	output.SetBundle(container.Bundle)
	output.SetRootfs(container.Rootfs)
	output.SetCreatedTimestamp(container.Created.UnixNano())
	output.SetAnnotations(container.Annotations)
	return output, nil
}

func ConvertContainerFromProto(container *runmv1.RuncContainer) (*gorunc.Container, error) {
	return &gorunc.Container{
		ID:          container.GetId(),
		Pid:         int(container.GetPid()),
		Status:      container.GetStatus(),
		Bundle:      container.GetBundle(),
		Rootfs:      container.GetRootfs(),
		Created:     time.Unix(0, container.GetCreatedTimestamp()),
		Annotations: container.GetAnnotations(),
	}, nil
}

// checkpoint actions in out

// delete in out
func ConvertDeleteOptsFromProto(opts *runmv1.RuncDeleteOptions) *gorunc.DeleteOpts {
	out := &gorunc.DeleteOpts{
		Force:     opts.GetForce(),
		ExtraArgs: opts.GetExtraArgs(),
	}
	return out
}

func ConvertDeleteOptsToProto(opts *gorunc.DeleteOpts) *runmv1.RuncDeleteOptions {
	out := &runmv1.RuncDeleteOptions{}
	out.SetForce(opts.Force)
	if opts.ExtraArgs != nil {
		out.SetExtraArgs(opts.ExtraArgs)
	}
	return out
}

// linux resources in out

func ConvertLinuxResourcesFromProto(resources *runmv1.RuncLinuxResources) (*specs.LinuxResources, error) {
	var linuxResources specs.LinuxResources
	if err := json.Unmarshal(resources.GetRawJson(), &linuxResources); err != nil {
		return nil, err
	}
	return &linuxResources, nil
}

func ConvertLinuxResourcesToProto(resources *specs.LinuxResources) (*runmv1.RuncLinuxResources, error) {
	rawJson, err := json.Marshal(resources)
	if err != nil {
		return nil, err
	}
	resp := &runmv1.RuncLinuxResources{}
	resp.SetRawJson(rawJson)
	return resp, nil
}

func ConvertProcessSpecFromProto(resources *runmv1.RuncProcessSpec) (*specs.Process, error) {
	var ProcessSpec specs.Process
	if err := json.Unmarshal(resources.GetRawJson(), &ProcessSpec); err != nil {
		return nil, err
	}
	return &ProcessSpec, nil
}

func ConvertProcessSpecToProto(resources *specs.Process) (*runmv1.RuncProcessSpec, error) {
	rawJson, err := json.Marshal(resources)
	if err != nil {
		return nil, err
	}
	resp := &runmv1.RuncProcessSpec{}
	resp.SetRawJson(rawJson)
	return resp, nil
}

func ConvertCheckpointOptsToProto(opts *gorunc.CheckpointOpts) *runmv1.RuncCheckpointOptions {
	output := &runmv1.RuncCheckpointOptions{}

	if opts.StatusFile != nil {
		panic("status file not handled e2e")
	}

	output.SetImagePath(opts.ImagePath)
	output.SetWorkDir(opts.WorkDir)
	output.SetParentPath(opts.ParentPath)
	output.SetAllowOpenTcp(opts.AllowOpenTCP)
	output.SetAllowExternalUnixSockets(opts.AllowExternalUnixSockets)
	output.SetAllowTerminal(opts.AllowTerminal)
	output.SetCriuPageServer(opts.CriuPageServer)
	output.SetFileLocks(opts.FileLocks)
	output.SetCgroups(string(opts.Cgroups))
	output.SetEmptyNamespaces(opts.EmptyNamespaces)
	output.SetLazyPages(opts.LazyPages)
	if opts.StatusFile != nil {
		output.SetStatusFile(opts.StatusFile.Name())
	}
	output.SetExtraArgs(opts.ExtraArgs)

	return output
}

func ConvertCheckpointOptsFromProto(opts *runmv1.RuncCheckpointOptions) (*gorunc.CheckpointOpts, error) {
	var err error

	var sfile *os.File
	if opts.GetStatusFile() != "" {
		sfile, err = os.Open(opts.GetStatusFile())
		if err != nil {
			return nil, err
		}
	}

	out := &gorunc.CheckpointOpts{
		ImagePath:                opts.GetImagePath(),
		WorkDir:                  opts.GetWorkDir(),
		ParentPath:               opts.GetParentPath(),
		AllowOpenTCP:             opts.GetAllowOpenTcp(),
		AllowExternalUnixSockets: opts.GetAllowExternalUnixSockets(),
		AllowTerminal:            opts.GetAllowTerminal(),
		CriuPageServer:           opts.GetCriuPageServer(),
		FileLocks:                opts.GetFileLocks(),
		Cgroups:                  gorunc.CgroupMode(opts.GetCgroups()),
		EmptyNamespaces:          opts.GetEmptyNamespaces(),
		LazyPages:                opts.GetLazyPages(),
		StatusFile:               sfile,
	}

	if opts.GetStatusFile() != "" {
		panic("status file not handled e2e")
	}

	return out, nil
}

func ConvertCheckpointActionsFromProto(actions []runmv1.RuncCheckpointAction) []gorunc.CheckpointAction {
	panic(runtime.ReflectNotImplementedError())
}

func ConvertCheckpointActionsToProto(actions ...gorunc.CheckpointAction) []*runmv1.RuncCheckpointAction {
	output := make([]*runmv1.RuncCheckpointAction, len(actions))
	for i, action := range actions {
		tmp := &runmv1.RuncCheckpointAction{}
		tmp.SetAction(action([]string{}))
		output[i] = tmp
	}
	return output
}

func ConvertEventFromProto(event *runmv1.RuncEvent) (*gorunc.Event, error) {
	var errz error
	if event.GetErr() != "" {
		errz = errors.New(event.GetErr())
	}

	stats, err := ConvertStatsFromProto(event.GetStats())
	if err != nil {
		return nil, err
	}

	return &gorunc.Event{
		Type:  event.GetType(),
		ID:    event.GetId(),
		Stats: stats,
		Err:   errz,
	}, nil
}

func ConvertTopResultsFromProto(results *runmv1.RuncTopResults) *gorunc.TopResults {
	output := &gorunc.TopResults{}
	headers := results.GetHeaders()
	processes := results.GetProcesses()

	processesz := make([][]string, len(processes))
	for i, process := range processes {
		processesz[i] = process.GetProcess()
	}

	output.Headers = headers
	output.Processes = processesz

	return output
}

func ConvertTopResultsToProto(results *gorunc.TopResults) *runmv1.RuncTopResults {
	headers := results.Headers
	processes := results.Processes

	processesz := make([]*runmv1.RuncTopProcesses, len(processes))
	for i, process := range processes {
		processesz[i] = &runmv1.RuncTopProcesses{}
		processesz[i].SetProcess(process)
	}

	output := &runmv1.RuncTopResults{}
	output.SetHeaders(headers)
	output.SetProcesses(processesz)

	return output
}
