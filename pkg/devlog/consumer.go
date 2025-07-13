package devlog

import (
	"context"
	"sync"
)

// BasicConsumer provides a simple implementation of the Consumer interface
type BasicConsumer struct {
	handlers []Handler
	mu       sync.RWMutex
	closed   bool
}

// NewBasicConsumer creates a new BasicConsumer
func NewBasicConsumer() *BasicConsumer {
	return &BasicConsumer{
		handlers: make([]Handler, 0),
	}
}

// Consume processes entries and sends them to all registered handlers
func (c *BasicConsumer) Consume(ctx context.Context, entry *Entry) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return ErrConsumerClosed
	}

	// Send to all handlers concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, len(c.handlers))

	for _, handler := range c.handlers {
		wg.Add(1)
		go func(h Handler) {
			defer wg.Done()
			if err := h.Handle(ctx, entry); err != nil {
				errChan <- err
			}
		}(handler)
	}

	wg.Wait()
	close(errChan)

	// Return the first error if any occurred
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

// AddHandler adds a handler to receive entries
func (c *BasicConsumer) AddHandler(handler Handler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	c.handlers = append(c.handlers, handler)
}

// RemoveHandler removes a handler
func (c *BasicConsumer) RemoveHandler(handler Handler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, h := range c.handlers {
		if h == handler {
			c.handlers = append(c.handlers[:i], c.handlers[i+1:]...)
			break
		}
	}
}

// Close shuts down the consumer
func (c *BasicConsumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	c.handlers = nil
	return nil
}

// GetHandlerCount returns the number of registered handlers
func (c *BasicConsumer) GetHandlerCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.handlers)
}
