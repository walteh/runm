package env

import (
	"context"
	"io"
)

type DynamicCleanup struct {
	deferDisabled bool
	cleanups      []func() error
}

func NewDynamicCleanup() *DynamicCleanup {
	return &DynamicCleanup{
		deferDisabled: false,
		cleanups:      []func() error{},
	}
}

func (d *DynamicCleanup) AddCloserFuncCtx(cleanup func(context.Context) error) {
	d.cleanups = append(d.cleanups, func() error {
		return cleanup(context.Background())
	})
}

func (d *DynamicCleanup) AddCloser(closer io.Closer) {
	d.cleanups = append(d.cleanups, func() error {
		return closer.Close()
	})
}

func (d *DynamicCleanup) AddCloserFunc(closer func() error) {
	d.cleanups = append(d.cleanups, closer)
}

func (d *DynamicCleanup) Defer() {
	if d.deferDisabled {
		return
	}

	d.cleanup()
}

func (d *DynamicCleanup) cleanup() error {
	for _, cleanup := range d.cleanups {
		if err := cleanup(); err != nil {
			return err
		}
	}
	return nil
}

func (d *DynamicCleanup) DetachDeferAsCloser() func() error {
	d.deferDisabled = true
	return d.cleanup
}
