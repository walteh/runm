package env

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"

	"gitlab.com/tozd/go/errors"
)

// duplicateStderrToWriter duplicates stderr to both its original destination and the given writer
// This preserves containerd's ability to read stderr while also capturing panics
func DuplicateFdToWriter(fd int, writer io.Writer) (func() error, error) {
	if writer == nil {
		return func() error { return nil }, nil
	}

	// Create a pipe to capture stderr output
	r, w, err := os.Pipe()
	if err != nil {
		return nil, errors.New("Failed to create pipe: " + err.Error())
	}

	// Duplicate the original stderr so we can restore it later
	originalStderr, err := unix.Dup(fd)
	if err != nil {
		r.Close()
		w.Close()
		return nil, errors.New("Failed to duplicate original stderr: " + err.Error())
	}

	// Replace stderr with our pipe write end
	err = unix.Dup2(int(w.Fd()), fd)
	if err != nil {
		unix.Close(originalStderr)
		r.Close()
		w.Close()
		return nil, errors.New("Failed to redirect stderr to pipe: " + err.Error())
	}

	// Create a file from the original stderr fd
	originalStderrFile := os.NewFile(uintptr(originalStderr), fmt.Sprintf("original-fd-%d", fd))

	// Start a goroutine that tees the output to both destinations
	go func() {
		defer r.Close()
		defer w.Close()
		defer originalStderrFile.Close()

		// Create a multi-writer that writes to both original stderr and our writer
		multiWriter := io.MultiWriter(originalStderrFile, writer)

		// Copy everything from the pipe to both destinations
		io.Copy(multiWriter, r)
	}()

	undo := func() error {
		// Restore original stderr
		undoErr := unix.Dup2(originalStderr, fd)
		unix.Close(originalStderr)

		if undoErr != nil {
			return errors.New("Failed to restore original stderr: " + undoErr.Error())
		}

		return nil
	}

	return undo, nil
}
