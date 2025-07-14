package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type step struct {
	w, h int
	wait time.Duration
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: pty-resize-test SEQUENCE [COMMAND...]")
		fmt.Println("       pty-resize-test SEQUENCE -generate-shell-script")
		fmt.Println()
		fmt.Println("SEQUENCE: HxW@duration,HxW@duration,...")
		fmt.Println("Example:  24x80@3s,40x120@3s,24x80@3s")
		os.Exit(1)
	}

	sequence := os.Args[1]

	if len(os.Args) == 3 && os.Args[2] == "-generate-shell-script" {
		generateScript(sequence)
		return
	}

	if len(os.Args) < 3 {
		log.Fatal("error: no command provided")
	}

	runTest(sequence, os.Args[2:])
}

func generateScript(sequence string) {
	steps, err := parseSequence(sequence)
	if err != nil {
		log.Fatal(err)
	}

	// Generate a simple command that just loops and shows sizes
	totalSleep := 0
	for _, s := range steps {
		totalSleep += int(s.wait.Seconds())
	}

	fullCheck := `echo "guest check $i: $(stty size | xargs printf '%sx%s')" 2>/dev/null || echo no-tty`

	fmt.Printf(`for i in $(seq -w 1 %d); do %s; sleep 1; done`, totalSleep+(totalSleep/4), fullCheck)
}

func runTest(sequence string, command []string) {
	steps, err := parseSequence(sequence)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Fatal(err)
	}
	defer ptmx.Close()

	// Set initial size
	if len(steps) > 0 {
		_ = pty.Setsize(ptmx, &pty.Winsize{
			Rows: uint16(steps[0].h),
			Cols: uint16(steps[0].w),
		})
	}

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start resize sequence
	go func() {
		for i, s := range steps {
			select {
			case <-time.After(s.wait):
				err := pty.Setsize(ptmx, &pty.Winsize{
					Rows: uint16(s.h),
					Cols: uint16(s.w),
				})
				if err != nil {
					fmt.Printf("host resize %2d: %dx%d: %v\n", i+1, s.h, s.w, err)
				} else {
					fmt.Printf("host resize %2d: %dx%d\n", i+1, s.h, s.w)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// I/O
	go func() {
		defer cancel()
		_, _ = io.Copy(os.Stdout, ptmx)
	}()

	go func() {
		defer cancel()
		_, _ = io.Copy(ptmx, os.Stdin)
	}()

	// Wait
	select {
	case sig := <-sigCh:
		fmt.Println("host signal received:", sig)
		cancel()
	case <-ctx.Done():
	}

	_ = cmd.Wait()
}

func parseSequence(s string) ([]step, error) {
	if s == "" {
		return nil, fmt.Errorf("empty sequence")
	}

	parts := strings.Split(s, ",")
	steps := make([]step, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)

		if !strings.Contains(part, "@") {
			return nil, fmt.Errorf("missing @duration in '%s'", part)
		}

		split := strings.Split(part, "@")
		if len(split) != 2 {
			return nil, fmt.Errorf("invalid format '%s'", part)
		}

		// Parse size
		sizeParts := strings.Split(split[0], "x")
		if len(sizeParts) != 2 {
			return nil, fmt.Errorf("invalid size '%s'", split[0])
		}

		h, err := strconv.Atoi(sizeParts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid width '%s'", sizeParts[0])
		}

		w, err := strconv.Atoi(sizeParts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid height '%s'", sizeParts[1])
		}

		// Parse duration
		d, err := time.ParseDuration(split[1])
		if err != nil {
			return nil, fmt.Errorf("invalid duration '%s'", split[1])
		}

		steps = append(steps, step{w: w, h: h, wait: d})
	}

	return steps, nil
}
