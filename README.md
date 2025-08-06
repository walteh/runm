# runm

experimental vm adaptor for runc, run containers natively on macOS

> [!WARNING]
> These docs are in progress

this project effectivly wraps the `containerd/go-runc` in a grpc service. the shim (running on macos) calls the grpc service (running on a linux vm) which directly invokes runc.

instead of creating containers directly on the host, we use a hypervisor to create a guest vm and run the container inside it.

the core functionality provided here is effectivly a modified version of the `cmd/containerd-shim-runc-v2` binary from the `containerd` project.

to work around the linux requirements of the shim, we proxy all linux dependencies to the guest vm where we run `runc` unmodified (aside from extra debug).

## supported `nerdctl run/exec` features

-   [x] exit status returned to the host
-   [x] bind mounts
-   [x] read-only mounts (via `ro=true`)
-   [x] `-d` detached mode
-   [x] internal internet access (ability to see google.com)
-   [x] `-it` interactive mode
-   [x] `-e` environment variables
-   [x] `-w` working directory
-   [x] `-v` volumes
-   [x] `-p` ports
-   [] `-u` user

## Linux

Runm offloads all required linux operations to a lightweight VM using the virtualization framework.

The linux kernel used is custom and entiely configured in `./linux/kernel`

Additionaly, a static `busybox` binary is also used. It is entiely built and configured in `./linux/busybox`.

Below is the structure of the linux file systems (excluding )

```
# initramfs
/init (symlink to `/runm-linux-mounter`)
/runm-linux-mounter
/bin/busybox
```

`runm-linux-mounter` is the only binary in the initramfs, it handles the logic requred to mount the `mbin` squashfs that containds the rest of the binaries needed to run this project.

```
# rootfs
/bin/busybox
/mbin/runm-linux-init
/mbin/runc-test
/mbin/runm-linux-host-fork-exec-proxy
```

## dependancy on macFUSE

This projects focus is entiely on seeing how far containerd can go running nativly on macOS. In other words, all the effort is/was put into things that don't work either nativly or via non-comercially free software.

Containerd's native snapshotter depends on bind mounts (`mount --bind`) that are not nativly supported on macOS. However, it can be accomplished by installing 3rd party software - specifially `bindfs` (oss, GPL-2) and either one of `macFUSE` or `fuse-t` (both are closed source but are free to use non-comercially).

### why the `native` snapshotter needs bind mounts

TODO: properly docuemnt this

### notes on `macFUSE` and `fuse-t`

Outside of licensing, the largest caveat with `macFUSE` is that it requires a kernel extension (kext) to function, something that requires any user to safe boot their mac to put it in `Reduced Security` mode. `fuse-t` does not have this requiement. In macOS 15, Apple added `FSKit` which functionally should allow `macFUSE` to do its thing entirly in the userspace and not touch the kernel. As of v5 released in 2025-06, `macFUSE` has added support for it. Although, I was not able to get it working out of the box with this project yet.

In my expericne with this project, `fuse-t` is MUCH more unstable. As an example, using `fuse-t` lead to many cases where random `glibc` files did not exist inside containers that depended on them - completly breaking dynamic linking. `macFUSE (kext)` on the other hand is incredibly stable - well, at least in comparison.

# forks

This project currently uses an aggressive amount of forks to function. Most of the changes are related to debugging and are not required.

Many of the chages are related to getting everything to run without `sudo` (rootless), so here I am seperating them

1. `github.com/containerd/containerd` - https://github.com/walteh/containerd/pull/2
2.

This section is an attempt to document the **required logical changes** that this project relies on.

### `containerd`

full diff (includes noise): https://github.com/walteh/containerd/pull/2

1. `darwin` build support for the `core/mount` package

-   diff:

-   effectivly it just requirs us to exec to bindfs when containerd wants to `mount --bind`

### rootless

1.
