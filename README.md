# runm

experimental vm adaptor for runc, run containers natively on macOS

## supported `nerdctl run` features

-   [x] exit status returned to the host
-   [x] bind mounts
-   [x] read-only mounts (via `ro=true`)
-   [] `-d` detached mode
-   [] internal internet access (ability to see google.com)
-   [] `-it` interactive mode
-   [] `-e` environment variables
-   [] `-w` working directory
-   [] `-v` volumes
-   [] `-p` ports
-   [] `-u` user

## background

instead of creating containers directly on the host, we use a hypervisor to create a guest vm and run the container inside it.

the core functionality provided here is effectivly a modified version of the `cmd/containerd-shim-runc-v2` binary from the `containerd` project.

to work around the linux requirements of the shim, we proxy all linux dependencies to the guest vm where we run `runc` unmodified (hopfully).

## linux dependencies proxied to the guest vm

-   `mounts`
-   `oom`
-   `namespaces`
-   `seccomp`
-   `schedcore`

# forks

this project requires various forks of other projects which are not yet merged.

-   `containerd/ttrpc`
-   `containerd/containerd`

## `containerd/ttrpc`

target: https://github.com/containerd/ttrpc
fork: https://github.com/walteh/ttrpc

active diff:

### description of changes
