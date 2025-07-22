package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"

	"gitlab.com/tozd/go/errors"
	"golang.org/x/sys/unix"
)

const (
	nfsdMountPoint   = "/proc/fs/nfsd"
	exportsCtlFile   = "/proc/fs/nfsd/exports"
	nfsdThreadsFile  = "/proc/fs/nfsd/threads"
	nfsdPortlistFile = "/proc/fs/nfsd/portlist"
)

// ensureNfsdPseudofs mounts the `nfsd` control filesystem if it is not
// already mounted, creating the mount-point as needed.
func ensureNfsdPseudofs(ctx context.Context) error {
	if _, err := os.Stat(nfsdMountPoint); os.IsNotExist(err) {
		if err := os.MkdirAll(nfsdMountPoint, 0o755); err != nil {
			return errors.Errorf("creating %s: %w", nfsdMountPoint, err)
		}
	}

	// device comes first, then target dir.
	err := ExecCmdForwardingStdio(ctx, "mount", "-t", "nfsd", "nfsd", nfsdMountPoint)
	if err != nil && !errors.Is(err, unix.EBUSY) {
		return errors.Errorf("mount nfsd pseudo-fs: %w", err)
	}
	if err := os.WriteFile("/proc/fs/nfsd/versions", []byte("-2 -3 +4\n"), 0644); err != nil {
		return errors.Errorf("writing /proc/fs/nfsd/versions: %w", err)
	}

	return nil
}

// writeExport appends one export line directly to the kernel table.
func writeExport(ctx context.Context, absPath string) error {
	fi, err := os.Stat(absPath)
	if err != nil || !fi.IsDir() {
		return errors.Errorf("export target invalid: %w", err)
	}

	// crude check: read mountinfo line and bail if fstype == "virtiofs"
	data, _ := os.ReadFile("/proc/self/mountinfo")
	if bytes.Contains(data, []byte(" "+absPath+" ")) && bytes.Contains(data, []byte(" - virtiofs ")) {
		return errors.Errorf("cannot export virtiofs path %s with kernel nfsd", absPath)
	}

	line := fmt.Sprintf("%s *(rw,sync,no_subtree_check,no_root_squash,fsid=0)\n", absPath)
	slog.InfoContext(ctx, "writing export", "path", absPath, "line", line, "file", exportsCtlFile)
	if err := os.WriteFile(exportsCtlFile, []byte(line), 0o644); err != nil {
		return errors.Errorf("writing %s: %w", exportsCtlFile, err)
	}
	return nil
}

// startNFS spins up the in-kernel NFS server by poking /proc files.
func startNFS(ctx context.Context, threads int) error {
	if err := ExecCmdForwardingStdio(ctx, "ip", "link", "set", "lo", "up"); err != nil {
		return errors.Errorf("setting up lo: %w", err)
	}

	// if err := os.WriteFile(nfsdPortlistFile, []byte("2049"), 0o644); err != nil {
	// 	return errors.Errorf("setting nfsd port: %w", err)
	// }

	if err := os.WriteFile(nfsdThreadsFile, []byte(fmt.Sprint(threads)), 0o644); err != nil {
		return errors.Errorf("setting nfsd threads: %w", err)
	}

	return nil
}

// // mountOverlay mounts a COW overlay suitable for NFS export.
// func mountOverlay(ctx context.Context, lower, upper, work, merged string) error {
// 	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s,index=on,nfs_export=on",
// 		lower, upper, work)

// 	// Correct order: device ("overlay") first, target dir last.
// 	if err := ExecCmdForwardingStdio(ctx, "mount", "-t", "overlay",
// 		"overlay", "-o", opts, merged); err != nil {
// 		return errors.Errorf("mounting overlay onto %s: %w", merged, err)
// 	}
// 	return nil
// }

// exportOverlay is a convenience that ties everything together.
func exportNfsDir(ctx context.Context, merged string, threads int) error {
	// if err := mountOverlay(ctx, lower, upper, work, merged); err != nil {
	// 	return errors.Errorf("mounting overlay: %w", err)
	// }
	if err := ensureNfsdPseudofs(ctx); err != nil {
		return errors.Errorf("ensure nfsd pseudofs: %w", err)
	}
	if err := startNFS(ctx, threads); err != nil {
		return errors.Errorf("starting NFS: %w", err)
	}
	if err := writeExport(ctx, merged); err != nil {
		return errors.Errorf("writing export: %w", err)
	}
	return nil
}
