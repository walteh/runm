module github.com/walteh/runm/exp/buildkit

go 1.25rc2

replace github.com/Code-Hex/vz/v3 => ../../../vz // managed by go-work-sync

replace github.com/arianvp/cgroup-exporter => ../../../cgroup-exporter // managed by go-work-sync

replace github.com/containerd/console => ../../../console // managed by go-work-sync

replace github.com/containerd/containerd/api => ../../../containerd/api // managed by go-work-sync

replace github.com/containerd/containerd/v2 => ../../../containerd // managed by go-work-sync

replace github.com/containerd/containerd/v2/pkg/sys => ../../../containerd/pkg/sys // managed by go-work-sync

replace github.com/containerd/go-runc => ../../../go-runc // managed by go-work-sync

replace github.com/containerd/nerdctl/mod/tigron => ../../../nerdctl/mod/tigron // managed by go-work-sync

replace github.com/containerd/nerdctl/v2 => ../../../nerdctl // managed by go-work-sync

replace github.com/containerd/stargz-snapshotter => ../../../stargz-snapshotter // managed by go-work-sync

replace github.com/containerd/stargz-snapshotter/estargz => ../../../stargz-snapshotter/estargz // managed by go-work-sync

replace github.com/containerd/ttrpc => ../../../ttrpc // managed by go-work-sync

replace github.com/containers/gvisor-tap-vsock => ../../../gvisor-tap-vsock // managed by go-work-sync

replace github.com/google/cadvisor => ../../../cadvisor // managed by go-work-sync

replace github.com/moby/buildkit => ../../../buildkit // managed by go-work-sync

replace github.com/opencontainers/runc => ../../../runc // managed by go-work-sync

replace github.com/pkg/errors => ../../../go-errors/pkg-errors // managed by go-work-sync

replace github.com/tonistiigi/fsutil => ../../../fsutil // managed by go-work-sync

replace gitlab.com/tozd/go/errors => ../../../go-errors // managed by go-work-sync

replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250807194038-c9af560a03d9 // managed by go-work-sync
