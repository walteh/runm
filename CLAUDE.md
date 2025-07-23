# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**runm** is an experimental VM adaptor for runc that enables running Linux containers natively on macOS. Instead of creating containers directly on the host, it uses a hypervisor to create a guest Linux VM and runs containers inside it, effectively proxying all Linux dependencies to the guest VM where unmodified runc can operate.

## Essential Commands

### Development Commands

-   `task go:lint` - Run linters and code formatting (required before commits)
-   `task go:test` - Run all tests using `go tool gotestsum`
-   `task go:tidy` - Tidy go.mod files across all modules
-   `task gen:all` - Generate all code (options, mocks, protobuf)

### Build Commands

-   `task go-build:NAME:OS:ARCH` - Build specific binary (e.g., `task go-build:containerd-shim-runm-v2:darwin:arm64`)
-   `task test:binaries` - Build all test binaries
-   `task test:linux:runtime` - Build complete Linux runtime (kernel, initramfs, binaries)

### Testing Commands

-   `task dev:nerdctl-test:run` - Run container tests using nerdctl
-   `task containerd:2025-07-01:01` - Run containerd integration tests
-   `task test:cleanup-running-shims` - Clean up running test containers

### Code Generation

-   `task gen:options` - Generate options structs (for types with `//go:opts`)
-   `task gen:mockery` - Generate mocks (for interfaces with `//go:mock`)
-   `task gen:buf` - Generate protobuf files

## High-Level Architecture

### Core Components

1. **Containerd Shim** (`cmd/containerd-shim-runm-v2/`)

    - Modified containerd shim that acts as the main entry point
    - Implements containerd shim v2 protocol
    - Manages VM lifecycle and container operations

2. **VM Infrastructure** (`core/virt/`)

    - VirtualMachine interface for VM operations
    - VF (Virtualization Framework) implementation for macOS
    - VMM (Virtual Machine Manager) for orchestration

3. **runc Proxy Layer** (`core/runc/`)

    - Complete proxy system forwarding runc operations from macOS host to Linux guest
    - GRPC clients on host side, GRPC server on guest side
    - I/O proxying for stdin/stdout/stderr

4. **Linux Guest Init** (`cmd/runm-linux-init/`)

    - Specialized init process running in guest Linux VM
    - VSock proxy servers for communication
    - Mount management and runtime server

5. **Network Layer** (`core/gvnet/`)
    - Uses gvisor-tap-vsock for VM connectivity
    - TCP proxy and VSock communication
    - DNS, DHCP, and port forwarding

### Communication Flow

-   Host containerd → runm shim → Linux guest VM → unmodified runc
-   VSock channels for all host-guest communication
-   Event-driven architecture with proper event propagation

## Development Standards

### Go Toolchain

-   **Always use `go tool goshim`** instead of `go` directly when available
-   Use `go tool goshim test` for testing (not `go test`)
-   Use `go tool goshim run` for local testing (not `go build`)
-   **Build Error Checking**: If IDE info is unavailable or build errors occur, run `go tool task claude:callback:check-for-go-build-errors` to get build diagnostics

### Logging

-   Use `slog` for all logging (not zerolog)
-   Embed `slog.Logger` into `context.Context` early: `slog.NewContext(parentCtx, logger)`
-   Retrieve with `slog.FromContext(ctx)` or pass ctx to `slog.InfoContext(ctx, ...)`
-   All functions that log should take `context.Context` as first argument
-   Always use single lines for logging, do not worry about readability. Turn values into slog.LogValuer's if needed and append common values using slogctx.

### Error Handling

-   Use `gitlab.com/tozd/go/errors` for error handling
-   Use `errors.Errorf` to wrap errors (not `errors.Wrap`)
-   Error messages should describe what was being attempted: `errors.Errorf("reading file: %w", err)`

### Testing

-   Use `go tool goshim test -function-coverage` - must maintain >85% function coverage
-   Use `testify/assert` and `testify/require` for assertions
-   Use `testify/mock` for mocking
-   Some tests require build tags (e.g., `-tags libkrun_efi`)
-   Always add descriptive messages to assertions

### Code Organization

-   `pkg/` - reusable packages
-   `cmd/` - executable commands
-   `gen/` - generated code (don't edit manually)
-   `tools/` - development tools
-   Respect workspace boundaries in go.work

### Dependencies Proxied to Guest VM

The system specifically proxies these Linux-only features:

-   Mounts, OOM monitoring, Namespaces, Seccomp, Schedcore, Cgroups

## Protobuf and Code Generation

### Protobuf Usage

-   Use **buf** for all protobuf operations: `task gen:buf`
-   **Only use the new opaque API** - avoid legacy protobuf patterns
-   Generated protobuf files are in `proto/` directory
-   Protobuf definitions are in `proto/**/*.proto`
-   Validation using `buf.build/go/protovalidate` for schema validation

### References

-   [Buf documentation](https://buf.build/docs/)
-   [Protobuf Go API](https://protobuf.dev/reference/go/go-generated/)
-   [Opaque API guide](https://protobuf.dev/reference/go/opaque/)

## Active Forks

This project uses several forked dependencies (see `go.mod` replace directives):

### Core Container Runtime Forks

-   **containerd/containerd** → `../containerd` - Main container runtime modifications
-   **containerd/go-runc** → `../go-runc` - Go bindings for runc
-   **containerd/ttrpc** → `../ttrpc` - TTRPC protocol modifications
-   **containerd/nerdctl** → `../nerdctl` - Docker-compatible CLI
-   **containerd/console** → `../console` - Console handling

### Virtualization Forks

-   **Code-Hex/vz** → `../vz` - macOS Virtualization.framework bindings
-   **containers/gvisor-tap-vsock** → `../gvisor-tap-vsock` - VM networking

### Other Forks

-   **opencontainers/runc** → `../runc` - Container runtime
-   **gitlab.com/tozd/go/errors** → `../go-errors` - Error handling library

These forks contain modifications specific to the runm architecture and may not be compatible with upstream versions.

## Maintenance Notes

**⚠️ IMPORTANT**: Future Claude instances should keep this file updated with:

-   New commands and workflows as they're added
-   Changes to coding standards and best practices
-   Updates to the architecture as components evolve
-   New dependencies, tools, or build requirements
-   Changes to testing procedures or coverage requirements

When working on this codebase, always check if updates to CLAUDE.md are needed to reflect new patterns, tools, or architectural changes.

## Important Notes

-   The system maintains full API compatibility with standard runc
-   VSock is used for all host-guest communication for performance
-   The guest VM runs unmodified runc for maximum compatibility
-   Multiple forks of upstream projects are required (see Active Forks section)
