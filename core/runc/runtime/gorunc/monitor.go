package goruncruntime

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"

	gorunc "github.com/containerd/go-runc"
)

// CustomMonitor intercepts exits from go-runc's default monitor instead of using reaper.Default. We'll create a custom monitor that wraps go-runc's default ProcessMonitor and forwards events to our subscribers.
type CustomMonitor struct {
	defaultMonitor   gorunc.ProcessMonitor
	subscribers      map[chan gorunc.Exit]struct{}
	subscribersMutex sync.Mutex
}

// Global instance of our custom monitor

// Start wraps the default monitor's Start method and intercepts exits
func (m *CustomMonitor) Start(c *exec.Cmd) (chan gorunc.Exit, error) {
	slog.Debug("custom monitor start", "path", c.Path)

	// Use go-runc's default monitor
	origCh, err := m.defaultMonitor.Start(c)
	if err != nil {
		slog.Error("default monitor start failed", "error", err)
		return nil, err
	}

	// Forward exits from the original channel to our channel
	copyCh := make(chan gorunc.Exit, 32)
	go func() {
		for exit := range origCh {
			copyCh <- exit
			m.broadcastExit(exit)
		}
	}()

	return origCh, nil
}

// StartLocked wraps the default monitor's StartLocked method
func (m *CustomMonitor) StartLocked(c *exec.Cmd) (chan gorunc.Exit, error) {
	slog.Debug("custom monitor start locked", "path", c.Path)

	// Use go-runc's default monitor
	origCh, err := m.defaultMonitor.StartLocked(c)
	if err != nil {
		slog.Error("default monitor start locked failed", "error", err)
		return nil, err
	}

	copyCh := make(chan gorunc.Exit, 32)

	// Forward exits from the original channel to our channel
	go func() {
		for exit := range origCh {
			copyCh <- exit
			m.broadcastExit(exit)
		}
	}()

	return copyCh, nil
}

// Wait wraps the default monitor's Wait method
func (m *CustomMonitor) Wait(c *exec.Cmd, ec chan gorunc.Exit) (int, error) {
	slog.Debug("custom monitor wait", "pid", c.Process.Pid)
	return m.defaultMonitor.Wait(c, ec)
}

// broadcastExit sends the exit event to all registered subscribers
func (m *CustomMonitor) broadcastExit(exit gorunc.Exit) {

	m.subscribersMutex.Lock()
	slog.Debug("broadcasting exit to all subscribers", "pid", exit.Pid, "status", exit.Status, "num_subscribers", len(m.subscribers))

	// Make a copy of subscribers to avoid holding the lock during channel sends
	subscribersCopy := make([]chan gorunc.Exit, 0, len(m.subscribers))
	for ch := range m.subscribers {
		subscribersCopy = append(subscribersCopy, ch)
	}
	m.subscribersMutex.Unlock()

	// Send to all subscribers
	for _, ch := range subscribersCopy {
		// Use goroutine to prevent blocking
		go func() {
			select {
			case ch <- exit:
				// Successfully sent
			default:
				// Channel buffer is full, log and continue
				slog.Warn("couldn't send exit notification - channel buffer full", "pid", exit.Pid)
			}
		}()
	}
}

// Subscribe registers a new channel to receive exit notifications
func (m *CustomMonitor) Subscribe(ctx context.Context) chan gorunc.Exit {
	slog.InfoContext(ctx, "subscribing to custom monitor exits")

	ch := make(chan gorunc.Exit, 32)
	m.subscribersMutex.Lock()
	m.subscribers[ch] = struct{}{}
	m.subscribersMutex.Unlock()

	return ch
}

// Unsubscribe removes a channel from receiving exit notifications
func (m *CustomMonitor) Unsubscribe(ctx context.Context, ch chan gorunc.Exit) {
	slog.InfoContext(ctx, "unsubscribing from custom monitor exits")

	m.subscribersMutex.Lock()
	delete(m.subscribers, ch)
	m.subscribersMutex.Unlock()
}
