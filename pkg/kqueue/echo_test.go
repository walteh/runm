//go:build darwin || freebsd || netbsd || openbsd
// +build darwin freebsd netbsd openbsd

package kqueue

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/containerd/console"
	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKqueueConsoleEchoDuplication reproduces the race condition where
// KqueueConsole.Read accumulates both input and echo in a single read call
func TestKqueueConsoleEchoDuplication(t *testing.T) {
	// Run multiple attempts to catch the race condition
	bugReproduced := false
	maxAttempts := 100 // Increase attempts

	for attempt := 1; attempt <= maxAttempts && !bugReproduced; attempt++ {
		t.Logf("Attempt %d/%d", attempt, maxAttempts)

		// Create a PTY pair with echo enabled (default behavior)
		ptyMaster, ptySlave, err := pty.Open()
		require.NoError(t, err)

		// Create a console from the master side
		cons, err := console.ConsoleFromFile(ptyMaster)
		require.NoError(t, err)

		// Create kqueue and add console
		kq, err := NewKqueuer()
		require.NoError(t, err)

		kqueueConsole, err := kq.Add(cons)
		require.NoError(t, err)

		// Start kqueue event loop
		go func() {
			kq.Wait()
		}()

		// Create a more aggressive scenario to trigger the race condition
		// Start multiple goroutines writing to PTY to increase the chances of timing collision
		testCommand := "hello world! this is a test\n"

		// Write data in rapid succession to increase likelihood of echo accumulation
		go func() {
			for i := 0; i < 3; i++ {
				ptySlave.Write([]byte(testCommand))
				time.Sleep(1 * time.Millisecond)
			}
		}()

		// Very small delay to catch timing window where both original and echo are available
		time.Sleep(2 * time.Millisecond)

		// Read from kqueue console - this is where the bug occurs
		buf := make([]byte, 1024)
		n, err := kqueueConsole.Read(buf)

		if err != nil {
			t.Logf("Attempt %d: Read error: %v", attempt, err)
			cons.Close()
			ptyMaster.Close()
			ptySlave.Close()
			kq.Close()
			continue
		}

		output := string(buf[:n])
		t.Logf("Attempt %d: Read %d bytes: %q", attempt, n, output)

		// Check for various forms of echo duplication
		// Look for any case where we get more than one line containing our test phrase
		lines := strings.Split(strings.TrimSpace(output), "\n")
		commandOccurrences := 0
		for _, line := range lines {
			if strings.Contains(line, "hello world! this is a test") {
				commandOccurrences++
			}
		}

		if commandOccurrences > 1 || n > 50 { // 50+ bytes suggests duplication
			bugReproduced = true
			t.Logf("BUG REPRODUCED on attempt %d!", attempt)
			t.Logf("Read %d bytes with %d command occurrences", n, commandOccurrences)
			t.Logf("Output: %q", output)
			t.Logf("Lines: %v", lines)
		} else if n <= 35 { // Normal single output
			t.Logf("Attempt %d: Normal behavior (%d bytes, %d occurrences)", attempt, n, commandOccurrences)
		} else {
			t.Logf("Attempt %d: Unusual size: %d bytes, %d occurrences", attempt, n, commandOccurrences)
		}

		// Cleanup
		cons.Close()
		ptyMaster.Close()
		ptySlave.Close()
		kq.Close()

		// Always verify we got some output
		assert.Greater(t, n, 0, "Should have read some output")
		assert.Contains(t, output, "hello world! this is a test", "Output should contain our test command")
	}

	if bugReproduced {
		t.Logf("SUCCESS: Echo duplication bug reproduced!")
		t.Fail() // Mark test as failed when bug is reproduced
	} else {
		t.Logf("Race condition not triggered in %d attempts", maxAttempts)
	}
}

// TestKqueueConsoleEchoFixed tests that the fix prevents echo duplication
func TestKqueueConsoleEchoFixed(t *testing.T) {
	// This test will be enabled after we implement the fix
	t.Skip("Will be enabled after implementing the fix")

	// Create PTY with echo prevention
	ptyMaster, ptySlave, err := pty.Open()
	require.NoError(t, err)
	defer ptyMaster.Close()
	defer ptySlave.Close()

	// Apply echo prevention (this will be implemented)
	cons, err := console.ConsoleFromFile(ptyMaster)
	require.NoError(t, err)
	defer cons.Close()

	// Create kqueue with fixed read behavior
	kq, err := NewKqueuer()
	require.NoError(t, err)
	defer kq.Close()

	kqueueConsole, err := kq.Add(cons)
	require.NoError(t, err)

	// Start kqueue event loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		kq.Wait()
	}()

	// Run the same test command
	testCommand := "hello world! this is a test"
	cmd := exec.CommandContext(ctx, "sh", "-c", "echo '"+testCommand+"'")
	cmd.Stdin = ptySlave
	cmd.Stdout = ptySlave
	cmd.Stderr = ptySlave

	err = cmd.Start()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Read from kqueue console
	buf := make([]byte, 1024)
	n, err := kqueueConsole.Read(buf)
	require.NoError(t, err)

	output := string(buf[:n])
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// After fix: should only see the command once
	commandCount := 0
	for _, line := range lines {
		if strings.Contains(line, testCommand) {
			commandCount++
		}
	}

	assert.Equal(t, 1, commandCount, "Command should appear exactly once (no echo duplication)")
	cmd.Wait()
}
