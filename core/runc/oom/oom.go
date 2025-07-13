package oom

import (
	"context"
	"log/slog"
	"strings"

	"github.com/containerd/containerd/v2/core/events"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	eventstypes "github.com/containerd/containerd/api/events"
	coreruntime "github.com/containerd/containerd/v2/core/runtime"

	"github.com/walteh/runm/core/runc/runtime"
)

type Watcher struct {
	alive         bool
	publisher     events.Publisher
	cgroupAdapter runtime.CgroupAdapter
}

func RunOOMWatcher(ctx context.Context, publisher events.Publisher, cgroupAdapter runtime.CgroupAdapter) error {
	eventCh, errCh, err := cgroupAdapter.OpenEventChan(ctx)
	if err != nil {
		return errors.Errorf("failed to open event channel: %w", err)
	}

	var lastOOMCount uint64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev := <-eventCh:
			slog.Debug("OOM-WATCHER[EVENT]", "event", ev)

			var containerID string
			if ev.GetCgroupPath() == "/" {
				containerID = "root"
			} else {
				containerID = strings.TrimPrefix(ev.GetCgroupPath(), "/")
			}

			if ev.GetOomKill() > lastOOMCount {
				if err := publisher.Publish(ctx, coreruntime.TaskOOMEventTopic, &eventstypes.TaskOOM{
					ContainerID: containerID,
				}); err != nil {
					return errors.Errorf("failed to publish OOM event: %w", err)
				}
				lastOOMCount = ev.GetOomKill()
			}

		case err := <-errCh:
			if err == nil {
				slog.Debug("OOM-WATCHER[DONE] no error")
				return nil
			}

			// Check for cancellation in nested errors (e.g., "Unavailable desc = ... Canceled")
			errStr := err.Error()
			if status.Code(err) == codes.Canceled ||
				status.Code(err) == codes.Unavailable ||
				strings.Contains(errStr, "Canceled") ||
				strings.Contains(errStr, "client connection is closing") {
				slog.Debug("OOM-WATCHER[DONE] ignoring error", "error", err)
				return nil
			}

			slog.Error("OOM-WATCHER[ERROR] error from cgroup event channel", "error", err)
			return errors.Errorf("error in cgroup event channel: %w", err)
		}
	}
}
