/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	goruntime "runtime"

	"golang.org/x/sys/unix"

	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/api/types/runc/options"
	"github.com/containerd/containerd/v2/core/mount"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/shim"
	"github.com/containerd/containerd/v2/version"
	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	"github.com/containerd/plugin"
	"github.com/containerd/plugin/registry"
	"github.com/containerd/typeurl/v2"

	"github.com/walteh/runm/cmd/containerd-shim-runm-v2/runm"
	"github.com/walteh/runm/core/runc/runtime"

	runmv1 "github.com/walteh/runm/proto/v1"
)

// NewShimManager returns an implementation of the shim manager
// using runc
func NewShimManager(name string) shim.Manager {

	grp := registry.Graph(func(r *plugin.Registration) bool {
		return r.ID != "runm-runtime-creator"
	})

	if len(grp) == 0 {
		panic("runm-runtime-creator not found")
	}

	ctx := context.Background()

	initctx := plugin.NewContext(ctx, nil, nil)

	creator, err := grp[0].InitFn(initctx)
	if err != nil {
		panic(err)
	}

	t, ok := creator.(runtime.RuntimeCreator)
	if !ok {
		panic("runm-runtime-creator is not a runtime.RuntimeCreator")
	}

	return &manager{
		name:    name,
		creator: t,
	}
}

// group labels specifies how the shim groups services.
// currently supports a runc.v2 specific .group label and the
// standard k8s pod label.  Order matters in this list
var groupLabels = []string{
	"io.containerd.runc.v2.group",
	"io.kubernetes.cri.sandbox-id",
}

// spec is a shallow version of [oci.Spec] containing only the
// fields we need for the hook. We use a shallow struct to reduce
// the overhead of unmarshaling.
type spec struct {
	// Annotations contains arbitrary metadata for the container.
	Annotations map[string]string `json:"annotations,omitempty"`
}

type manager struct {
	name            string
	shimGrpcService runmv1.ShimServiceClient
	creator         runtime.RuntimeCreator
}

func newCommand(ctx context.Context, id, containerdAddress, containerdTTRPCAddress string, debug bool) (*exec.Cmd, error) {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-namespace", ns,
		"-id", id,
		"-address", containerdAddress,
	}
	if debug {
		args = append(args, "-debug")
	}
	cmd := exec.Command(self, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GOMAXPROCS=4")
	cmd.Env = append(cmd.Env, "OTEL_SERVICE_NAME=containerd-shim-"+id)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	return cmd, nil
}

func readSpec() (*spec, error) {
	const configFileName = "config.json"
	f, err := os.Open(configFileName)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var s spec
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (m manager) Name() string {
	return m.name
}

type shimSocket struct {
	addr string
	s    *net.UnixListener
	f    *os.File
}

func (s *shimSocket) Close() {
	if s.s != nil {
		s.s.Close()
	}
	if s.f != nil {
		s.f.Close()
	}
	_ = shim.RemoveSocket(s.addr)
}

func newShimSocket(ctx context.Context, path, id string, debug bool) (*shimSocket, error) {
	address, err := shim.SocketAddress(ctx, path, id, debug)
	if err != nil {
		return nil, err
	}
	socket, err := shim.NewSocket(address)
	if err != nil {
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !shim.SocketEaddrinuse(err) {
			return nil, fmt.Errorf("create new shim socket: %w", err)
		}
		if !debug && shim.CanConnect(address) {
			return &shimSocket{addr: address}, errdefs.ErrAlreadyExists
		}
		if err := shim.RemoveSocket(address); err != nil {
			return nil, fmt.Errorf("remove pre-existing socket: %w", err)
		}
		if socket, err = shim.NewSocket(address); err != nil {
			return nil, fmt.Errorf("try create new shim socket 2x: %w", err)
		}
	}
	s := &shimSocket{
		addr: address,
		s:    socket,
	}
	f, err := socket.File()
	if err != nil {
		s.Close()
		return nil, err
	}
	s.f = f
	return s, nil
}

func (manager) Start(ctx context.Context, id string, opts shim.StartOpts) (_ shim.BootstrapParams, retErr error) {
	var params shim.BootstrapParams
	params.Version = 3
	params.Protocol = "ttrpc"

	cmd, err := newCommand(ctx, id, opts.Address, opts.TTRPCAddress, opts.Debug)
	if err != nil {
		return params, err
	}
	grouping := id
	spec, err := readSpec()
	if err != nil {
		return params, err
	}
	for _, group := range groupLabels {
		if groupID, ok := spec.Annotations[group]; ok {
			grouping = groupID
			break
		}
	}

	var sockets []*shimSocket
	defer func() {
		if retErr != nil {
			for _, s := range sockets {
				s.Close()
			}
		}
	}()

	s, err := newShimSocket(ctx, opts.Address, grouping, false)
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			params.Address = s.addr
			return params, nil
		}
		return params, err
	}
	sockets = append(sockets, s)
	cmd.ExtraFiles = append(cmd.ExtraFiles, s.f)

	if opts.Debug {
		s, err = newShimSocket(ctx, opts.Address, grouping, true)
		if err != nil {
			return params, err
		}
		sockets = append(sockets, s)
		cmd.ExtraFiles = append(cmd.ExtraFiles, s.f)
	}

	goruntime.LockOSThread()
	if os.Getenv("SCHED_CORE") != "" {
		// not possible ouside of linux
		// if err := schedcore.Create(schedcore.ProcessGroup); err != nil {
		// 	return params, fmt.Errorf("enable sched core support: %w", err)
		// }
	}

	if err := cmd.Start(); err != nil {
		return params, err
	}

	goruntime.UnlockOSThread()

	defer func() {
		if retErr != nil {
			cmd.Process.Kill()
		}
	}()
	// make sure to wait after start
	go cmd.Wait()

	// if opts, err := shim.ReadRuntimeOptions[*options.Options](os.Stdin); err == nil {
	// 	if opts.ShimCgroup != "" {
	// 		if cgroups.Mode() == cgroups.Unified {
	// 			cg, err := cgroupsv2.Load(opts.ShimCgroup)
	// 			if err != nil {
	// 				return params, fmt.Errorf("failed to load cgroup %s: %w", opts.ShimCgroup, err)
	// 			}
	// 			if err := cg.AddProc(uint64(cmd.Process.Pid)); err != nil {
	// 				return params, fmt.Errorf("failed to join cgroup %s: %w", opts.ShimCgroup, err)
	// 			}
	// 		} else {
	// 			cg, err := cgroup1.Load(cgroup1.StaticPath(opts.ShimCgroup))
	// 			if err != nil {
	// 				return params, fmt.Errorf("failed to load cgroup %s: %w", opts.ShimCgroup, err)
	// 			}
	// 			if err := cg.AddProc(uint64(cmd.Process.Pid)); err != nil {
	// 				return params, fmt.Errorf("failed to join cgroup %s: %w", opts.ShimCgroup, err)
	// 			}
	// 		}
	// 	}
	// }

	if err := shim.AdjustOOMScore(cmd.Process.Pid); err != nil {
		return params, fmt.Errorf("failed to adjust OOM score for shim: %w", err)
	}

	params.Address = sockets[0].addr
	return params, nil
}

func (m manager) Stop(ctx context.Context, id string) (shim.StopStatus, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return shim.StopStatus{}, err
	}

	path := filepath.Join(filepath.Dir(cwd), id)
	// ns, err := namespaces.NamespaceRequired(ctx)
	// if err != nil {
	// 	return shim.StopStatus{}, err
	// }
	// runtimez, err := runm.ReadRuntime(path)
	// if err != nil && !os.IsNotExist(err) {
	// 	return shim.StopStatus{}, err
	// }
	// opts, err := runm.ReadOptions(path)
	// if err != nil {
	// 	return shim.StopStatus{}, err
	// }
	// root := process.RuncRoot
	// if opts != nil && opts.Root != "" {
	// 	root = opts.Root
	// }

	// this needs to be taken care of in the sig-kill of the main shim process, not here
	pid, err := runm.ReadPid(path)
	if err != nil {
		return shim.StopStatus{}, err
	}

	_, err = m.shimGrpcService.ShimKill(ctx, &runmv1.ShimKillRequest{})
	if err != nil {
		return shim.StopStatus{}, err
	}
	// pid = int(killResponse.GetInitPid())

	// kill the pid
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		slog.Error("failed to kill pid", "error", err)
	}

	// r := m.creator.Create(ctx, &runtime.RuntimeOptions{
	// 	Root:          root,
	// 	Path:          path,
	// 	Namespace:     ns,
	// 	Runtime:       runtimez,
	// 	SystemdCgroup: opts.SystemdCgroup,
	// })

	if err := mount.UnmountRecursive(filepath.Join(path, "rootfs"), 0); err != nil {
		log.G(ctx).WithError(err).Warn("failed to cleanup rootfs mount")
	}
	// pid, err := gorunc.ReadPidFile(filepath.Join(path, process.InitPidFile))
	// if err != nil {
	// 	log.G(ctx).WithError(err).Warn("failed to read init pid file")
	// }
	return shim.StopStatus{
		ExitedAt:   time.Now(),
		ExitStatus: 128 + int(unix.SIGKILL),
		Pid:        pid,
	}, nil
}

func (m manager) Info(ctx context.Context, optionsR io.Reader) (*types.RuntimeInfo, error) {
	info := &types.RuntimeInfo{
		Name: m.name,
		Version: &types.RuntimeVersion{
			Version:  version.Version,
			Revision: version.Revision,
		},
		Annotations: nil,
	}

	opts, err := shim.ReadRuntimeOptions[*options.Options](optionsR)
	if err != nil {
		if !errors.Is(err, errdefs.ErrNotFound) {
			return nil, fmt.Errorf("failed to read runtime options (*options.Options): %w", err)
		}
	}
	if opts != nil {
		info.Options, err = typeurl.MarshalAnyToProto(opts)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %T: %w", opts, err)
		}
	}
	// if opts.BinaryName != "" {
	// 	binaryName = opts.BinaryName
	// }

	// }
	// absBinary, err := exec.LookPath(binaryName)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to look up the path of %q: %w", binaryName, err)
	// }
	// features, err := m.features(ctx, absBinary, opts)
	// if err != nil {
	// 	// youki does not implement `runc features` yet, at the time of writing this (Sep 2023)
	// 	// https://github.com/containers/youki/issues/815
	// 	log.G(ctx).WithError(err).Debug("Failed to get the runtime features. The runc binary does not implement `runc features` command?")
	// }

	// if m.features != nil {
	// 	info.Features, err = typeurl.MarshalAnyToProto(m.features)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("failed to marshal %T: %w", m.features, err)
	// 	}
	// } else {

	// 	fPayload, err := m.shimGrpcService.ShimFeatures(ctx, &runmv1.ShimFeaturesRequest{})
	// 	if err != nil {
	// 		return nil, fmt.Errorf("failed to get runtime features: %w", err)
	// 	}

	// 	// if f != nil {
	// 	f := &features.Features{}
	// 	if err := json.Unmarshal(fPayload.GetRawJson(), f); err != nil {
	// 		return nil, fmt.Errorf("failed to unmarshal runtime features: %w", err)
	// 	}
	// 	info.Features, err = typeurl.MarshalAnyToProto(f)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("failed to marshal %T: %w", f, err)
	// 	}
	// }

	features, err := m.creator.Features(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime features: %w", err)
	}
	info.Features, err = typeurl.MarshalAnyToProto(features)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %T: %w", features, err)
	}
	// }
	return info, nil
}
