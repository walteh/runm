package runtime

// var _ IO = &HostAllocatedStdioViaFifoProxy{}

// // HostAllocatedStdioViaFifoProxy represents stdio with a FIFO proxy layer between vsock and local pipes
// type HostAllocatedStdioViaFifoProxy struct {
// 	*HostAllocatedStdio
// 	fifoDir     string
// 	stdinPath   string
// 	stdoutPath  string
// 	stderrPath  string
// 	stdinRead   io.WriteCloser
// 	stdoutWrite io.ReadCloser
// 	stderrWrite io.ReadCloser
// 	closers     []io.Closer
// 	cancelFuncs []context.CancelFunc
// 	closeOnce   sync.Once
// }

// // NewHostAllocatedStdioViaFifoProxy creates a new stdio handler with FIFO proxy between vsock and local pipes
// func NewHostAllocatedStdioViaFifoProxy(ctx context.Context, referenceId string, stdinRef, stdoutRef, stderrRef AllocatedSocket) (*HostAllocatedStdioViaFifoProxy, error) {
// 	baseStdio := NewHostAllocatedStdio(ctx, referenceId, stdinRef, stdoutRef, stderrRef)
// 	if baseStdio == nil {
// 		return nil, fmt.Errorf("failed to create base host allocated stdio")
// 	}

// 	// Create temp directory for FIFOs
// 	fifoDir, err := os.MkdirTemp("", fmt.Sprintf("runm-fifo-%s-", strings.ReplaceAll(referenceId, ":", "-")))
// 	if err != nil {
// 		return nil, fmt.Errorf("creating fifo directory: %w", err)
// 	}

// 	proxy := &HostAllocatedStdioViaFifoProxy{
// 		HostAllocatedStdio: baseStdio,
// 		fifoDir:            fifoDir,
// 		stdinPath:          filepath.Join(fifoDir, "stdin"),
// 		stdoutPath:         filepath.Join(fifoDir, "stdout"),
// 		stderrPath:         filepath.Join(fifoDir, "stderr"),
// 		closers:            []io.Closer{},
// 		cancelFuncs:        []context.CancelFunc{},
// 	}

// 	// Setup cleanup on context cancellation
// 	proxyCtx, cancel := context.WithCancel(ctx)
// 	proxy.cancelFuncs = append(proxy.cancelFuncs, cancel)

// 	go func() {
// 		<-proxyCtx.Done()
// 		proxy.Close()
// 	}()

// 	// Setup stdin proxy
// 	if stdinRef != nil {
// 		stdinRead, err := proxy.setupStdinProxy(proxyCtx)
// 		if err != nil {
// 			proxy.Close()
// 			return nil, fmt.Errorf("setting up stdin proxy: %w", err)
// 		}
// 		proxy.stdinRead = stdinRead
// 	}

// 	// Setup stdout proxy
// 	if stdoutRef != nil {
// 		stdoutWrite, err := proxy.setupReadProxy(proxyCtx, proxy.stdoutPath, stdoutRef)
// 		if err != nil {
// 			proxy.Close()
// 			return nil, fmt.Errorf("setting up stdout proxy: %w", err)
// 		}
// 		proxy.stdoutWrite = stdoutWrite
// 	}

// 	// Setup stderr proxy
// 	if stderrRef != nil {
// 		stderrWrite, err := proxy.setupStderrProxy(proxyCtx)
// 		if err != nil {
// 			proxy.Close()
// 			return nil, fmt.Errorf("setting up stderr proxy: %w", err)
// 		}
// 		proxy.stderrWrite = stderrWrite
// 	}

// 	return proxy, nil
// }

// func (p *HostAllocatedStdioViaFifoProxy) setupStdinProxy(ctx context.Context) (io.WriteCloser, error) {
// 	// // Create FIFO for stdin
// 	// if err := fifo.Mkfifo(p.stdinPath, 0o600); err != nil {
// 	// 	return fmt.Errorf("creating stdin fifo: %w", err)
// 	// }

// 	// Open FIFO for reading (this will block until someone opens it for writing)
// 	stdinRead, err := fifo.OpenFifo(ctx, p.stdinPath, syscall.O_CREAT|syscall.O_WRONLY|syscall.O_NONBLOCK, 0o600)
// 	if err != nil {
// 		return nil, fmt.Errorf("opening stdin fifo for reading: %w", err)
// 	}
// 	p.closers = append(p.closers, stdinRead)

// 	// Setup proxy goroutine for stdin
// 	ctx, cancel := context.WithCancel(ctx)
// 	p.cancelFuncs = append(p.cancelFuncs, cancel)

// 	go func() {
// 		defer cancel()
// 		buf := make([]byte, 32*1024)
// 		// Read from FIFO, write to vsock
// 		for {
// 			nr, er := stdinRead.Read(buf)
// 			if nr > 0 {
// 				_, ew := p.HostAllocatedStdio.Stdin().Write(buf[0:nr])
// 				if ew != nil {
// 					slog.Debug("error writing to vsock stdin", "error", ew)
// 					break
// 				}
// 			}
// 			if er == io.EOF || er == fifo.ErrReadClosed {
// 				break
// 			}
// 			if er != nil {
// 				slog.Debug("error reading from stdin fifo", "error", er)
// 				break
// 			}
// 		}
// 		// Close the vsock connection when FIFO is closed
// 		if c, ok := p.HostAllocatedStdio.Stdin().(io.Closer); ok {
// 			c.Close()
// 		}
// 	}()

// 	return stdinRead, nil
// }

// func (p *HostAllocatedStdioViaFifoProxy) setupReadProxy(ctx context.Context, path string, read io.ReadCloser) (io.ReadCloser, error) {
// 	// Create FIFO for stdout
// 	// if err := fifo.Mkfifo(p.stdoutPath, 0o600); err != nil {
// 	// 	return fmt.Errorf("creating stdout fifo: %w", err)
// 	// }

// 	// Open FIFO for writing (this will block until someone opens it for reading)
// 	writer, err := fifo.OpenFifo(ctx, path, syscall.O_CREAT|syscall.O_WRONLY|syscall.O_NONBLOCK, 0o600)
// 	if err != nil {
// 		return nil, errors.Errorf("opening stdout fifo for writing: %w", err)
// 	}
// 	p.closers = append(p.closers, writer)

// 	// Setup proxy goroutine for stdout
// 	ctx, cancel := context.WithCancel(ctx)
// 	p.cancelFuncs = append(p.cancelFuncs, cancel)

// 	go func() {
// 		defer cancel()
// 		buf := make([]byte, 32*1024)
// 		// Read from vsock, write to FIFO
// 		for {
// 			nr, er := read.Read(buf)
// 			if nr > 0 {
// 				_, ew := writer.Write(buf[0:nr])
// 				if ew != nil {
// 					slog.Debug("error writing to stdout fifo", "error", ew)
// 					break
// 				}
// 			}
// 			if er == io.EOF {
// 				break
// 			}
// 			if er != nil {
// 				slog.Debug("error reading from vsock stdout", "error", er)
// 				break
// 			}
// 		}
// 		writer.Close()
// 	}()

// 	return writer, nil
// }

// func (p *HostAllocatedStdioViaFifoProxy) setupStderrProxy(ctx context.Context) (io.ReadCloser, error) {
// 	// Create FIFO for stderr
// 	// if err := fifo.Mkfifo(p.stderrPath, 0o600); err != nil {
// 	// 	return fmt.Errorf("creating stderr fifo: %w", err)
// 	// }

// 	// Open FIFO for writing (this will block until someone opens it for reading)
// 	stderrWrite, err := fifo.OpenFifo(ctx, p.stderrPath, syscall.O_CREAT|syscall.O_RDONLY|syscall.O_NONBLOCK, 0o600)
// 	if err != nil {
// 		return nil, errors.Errorf("opening stderr fifo for writing: %w", err)
// 	}
// 	p.closers = append(p.closers, stderrWrite)

// 	// Setup proxy goroutine for stderr
// 	ctx, cancel := context.WithCancel(ctx)
// 	p.cancelFuncs = append(p.cancelFuncs, cancel)

// 	go func() {
// 		defer cancel()
// 		buf := make([]byte, 32*1024)
// 		// Read from vsock, write to FIFO
// 		for {
// 			nr, er := p.HostAllocatedStdio.Stderr().Read(buf)
// 			if nr > 0 {
// 				_, ew := stderrWrite.Write(buf[0:nr])
// 				if ew != nil {
// 					slog.Debug("error writing to stderr fifo", "error", ew)
// 					break
// 				}
// 			}
// 			if er == io.EOF {
// 				break
// 			}
// 			if er != nil {
// 				slog.Debug("error reading from vsock stderr", "error", er)
// 				break
// 			}
// 		}
// 		stderrWrite.Close()
// 	}()

// 	return stderrWrite, nil
// }

// // Close implements io.Closer
// func (p *HostAllocatedStdioViaFifoProxy) Close() error {
// 	var err error
// 	p.closeOnce.Do(func() {
// 		// Cancel all proxy goroutines
// 		for _, cancel := range p.cancelFuncs {
// 			cancel()
// 		}

// 		// Close all open file descriptors
// 		for _, c := range p.closers {
// 			if cerr := c.Close(); cerr != nil && err == nil {
// 				err = cerr
// 			}
// 		}

// 		// Clean up FIFO directory
// 		if rerr := os.RemoveAll(p.fifoDir); rerr != nil && err == nil {
// 			err = rerr
// 		}

// 		// Close base stdio
// 		if p.HostAllocatedStdio != nil {
// 			if cerr := p.HostAllocatedStdio.Close(); cerr != nil && err == nil {
// 				err = cerr
// 			}
// 		}
// 	})
// 	return err
// }

// // Stdin returns the path to the stdin FIFO
// func (p *HostAllocatedStdioViaFifoProxy) Stdin() io.WriteCloser {
// 	return p.stdinRead
// }

// // Stdout returns the path to the stdout FIFO
// func (p *HostAllocatedStdioViaFifoProxy) Stdout() io.ReadCloser {
// 	return p.stdoutWrite
// }

// // Stderr returns the path to the stderr FIFO
// func (p *HostAllocatedStdioViaFifoProxy) Stderr() io.ReadCloser {
// 	return p.stderrWrite
// }

// // URI returns fifo URI format with the FIFO paths
// func (p *HostAllocatedStdioViaFifoProxy) URI() (string, error) {
// 	return fmt.Sprintf("fifo://%s", p.fifoDir), nil
// }
