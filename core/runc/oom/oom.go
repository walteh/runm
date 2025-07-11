package oom

import (
	"context"
	"log/slog"

	"github.com/containerd/containerd/v2/core/events"
	"github.com/walteh/run"
	"gitlab.com/tozd/go/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	eventstypes "github.com/containerd/containerd/api/events"
	coreruntime "github.com/containerd/containerd/v2/core/runtime"

	"github.com/walteh/runm/core/runc/runtime"
)

var _ run.Runnable = (*Watcher)(nil)

// isExpectedClosureError checks if the error indicates an expected closure condition
// func isExpectedClosureError(err error) bool {
// 	if errors.Is(err, io.EOF) {
// 		return true
// 	}

// 	errStatus := status.Code(err)
// 	if errStatus == codes.Canceled {
// 		return true
// 	}

// 	// Check for gRPC connection closing errors
// 	errStr := err.Error()
// 	return strings.Contains(errStr, "grpc: the client connection is closing") ||
// 		strings.Contains(errStr, "transport is closing") ||
// 		strings.Contains(errStr, "connection is closing")
// }

type Watcher struct {
	alive         bool
	publisher     events.Publisher
	cgroupAdapter runtime.CgroupAdapter
}

// Alive implements run.Runnable.
func (w *Watcher) Alive() bool {
	return w.alive
}

// Fields implements run.Runnable.
func (w *Watcher) Fields() []slog.Attr {
	return []slog.Attr{}
}

// Name implements run.Runnable.
func (w *Watcher) Name() string {
	return "oom-watcher"
}

type item struct {
	id  string
	ev  runtime.CgroupEvent
	err error
}

func NewWatcher(publisher events.Publisher, cgroupAdapter runtime.CgroupAdapter) *Watcher {
	return &Watcher{
		publisher:     publisher,
		cgroupAdapter: cgroupAdapter,
	}
}

func (w *Watcher) Close(ctx context.Context) error {
	return nil
}

func (w *Watcher) Run(ctx context.Context) error {
	w.alive = true
	defer func() {
		w.alive = false
	}()

	eventCh, errCh, err := w.cgroupAdapter.OpenEventChan(ctx)
	if err != nil {
		return errors.Errorf("failed to open event channel: %w", err)
	}

	lastOOMMap := make(map[string]uint64) // key: id, value: ev.OOM
	itemCh := make(chan item)

	defer func() {

		close(itemCh)
	}()

	go func() {
		for {
			i := item{id: "root"}
			select {
			case ev := <-eventCh:
				slog.Debug("EVENT", "id", i.id, "ev", ev)
				i.ev = ev
				itemCh <- i
			case err := <-errCh:
				// channel is closed when cgroup gets deleted or connection closes
				if err != nil && status.Code(err) != codes.Canceled {
					i.err = err
					itemCh <- i
					// we no longer get any event/err when we got an err
					slog.Error("error from *cgroupsv2.Manager.EventChan", "error", err)
				}
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			close(itemCh)
			return ctx.Err()
		case i := <-itemCh:
			if i.err != nil {
				delete(lastOOMMap, i.id)
				continue
			}
			lastOOM := lastOOMMap[i.id]
			if i.ev.OOMKill > lastOOM {
				if err := w.publisher.Publish(ctx, coreruntime.TaskOOMEventTopic, &eventstypes.TaskOOM{
					ContainerID: i.id,
				}); err != nil {
					return errors.Errorf("failed to publish OOM event: %w", err)
				}
			}
			if i.ev.OOMKill > 0 {
				lastOOMMap[i.id] = i.ev.OOMKill
			}
		}
	}

}
