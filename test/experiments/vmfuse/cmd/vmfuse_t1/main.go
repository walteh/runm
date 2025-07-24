package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/test/integration/env"
	"gitlab.com/tozd/go/errors"
)

type VMFuseTestSuite struct {
	vmfusedBin      string
	vmfusectlBin    string
	linuxRuntimeDir string
	tmpDir          string
	testDir         string
	mountTarget     string
	vmfusedSock     string
	vmfusedCmd      *exec.Cmd
	vmfusedPID      int
	cleanupOnce     sync.Once
	logger          *slog.Logger
}

func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	logger := logging.NewDefaultDevLogger("vmfuse-test", os.Stdout)

	suite, err := setupTestSuite(logger)
	if err != nil {
		logger.Error("Failed to setup test suite", "error", err)
		exitCode = 1
		return
	}
	defer suite.cleanup()

	logger.Info("Starting VMFuse integration test")

	if err := suite.runBasicMountTest(); err != nil {
		logger.Error("Basic mount test failed", "error", err)
		exitCode = 1
		return
	}

	if err := suite.runFileAccessTest(); err != nil {
		logger.Error("File access test failed", "error", err)
		exitCode = 1
		return
	}

	if err := suite.runUnmountTest(); err != nil {
		logger.Error("Unmount test failed", "error", err)
		exitCode = 1
		return
	}

	logger.Info("All tests passed successfully!")
}

func setupTestSuite(logger *slog.Logger) (*VMFuseTestSuite, error) {
	vmfusedBin := getEnvOrDefault("VMFUSED_BIN", "./gen/build/test-bin/vmfused")
	vmfusectlBin := getEnvOrDefault("VMFUSECTL_BIN", "./gen/build/test-bin/vmfusectl")
	linuxRuntimeDir := getEnvOrDefault("LINUX_RUNTIME_BUILD_DIR", "./gen/build/linux-runtime")

	if _, err := os.Stat(vmfusedBin); os.IsNotExist(err) {
		return nil, errors.Errorf("vmfused binary not found at %s", vmfusedBin)
	}
	if _, err := os.Stat(vmfusectlBin); os.IsNotExist(err) {
		return nil, errors.Errorf("vmfusectl binary not found at %s", vmfusectlBin)
	}
	if _, err := os.Stat(linuxRuntimeDir); os.IsNotExist(err) {
		return nil, errors.Errorf("Linux runtime directory not found at %s", linuxRuntimeDir)
	}

	tmpDir, err := os.MkdirTemp("", "vmfuse-test-*")
	if err != nil {
		return nil, errors.Errorf("failed to create temporary directory: %w", err)
	}

	testDir := filepath.Join(tmpDir, "test-source")
	mountTarget := filepath.Join(tmpDir, "test-mount")
	vmfusedSock := filepath.Join(tmpDir, "vmfused.sock")

	os.Setenv("VMFUSED_ADDRESS", fmt.Sprintf("unix://%s", vmfusedSock))
	os.Setenv("ENABLE_TEST_LOGGING", "1")
	os.Setenv("LINUX_RUNTIME_BUILD_DIR", linuxRuntimeDir)

	err = os.MkdirAll(testDir, 0755)
	if err != nil {
		return nil, errors.Errorf("failed to create test directory: %w", err)
	}

	err = os.MkdirAll(mountTarget, 0755)
	if err != nil {
		return nil, errors.Errorf("failed to create mount target directory: %w", err)
	}

	suite := &VMFuseTestSuite{
		vmfusedBin:      vmfusedBin,
		vmfusectlBin:    vmfusectlBin,
		linuxRuntimeDir: linuxRuntimeDir,
		tmpDir:          tmpDir,
		testDir:         testDir,
		mountTarget:     mountTarget,
		vmfusedSock:     vmfusedSock,
		logger:          logger,
	}

	if err := env.SetupContainerdLogReceiver(context.Background()); err != nil {
		return nil, errors.Errorf("setting up shim: %v", err)
	}

	if err := suite.setupTestFiles(); err != nil {
		return nil, errors.Errorf("failed to setup test files: %w", err)
	}

	if err := suite.startVMFused(); err != nil {
		return nil, errors.Errorf("failed to start vmfused: %w", err)
	}

	return suite, nil
}

func (s *VMFuseTestSuite) setupTestFiles() error {
	testContent := "Hello from vmfuse test!\nThis is a bind mount test via Linux VM and NFS\n"
	testFilePath := filepath.Join(s.testDir, "test-file.txt")

	err := os.WriteFile(testFilePath, []byte(testContent), 0644)
	if err != nil {
		return errors.Errorf("failed to create test file: %w", err)
	}

	s.logger.Info("Created test file", "file", testFilePath)
	return nil
}

func (s *VMFuseTestSuite) startVMFused() error {

	s.vmfusedCmd = exec.Command(s.vmfusedBin)
	s.vmfusedCmd.Env = os.Environ()
	s.vmfusedCmd.Stdout = os.Stdout
	s.vmfusedCmd.Stderr = os.Stderr

	s.vmfusedCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	err := s.vmfusedCmd.Start()
	if err != nil {
		return errors.Errorf("failed to start vmfused: %w", err)
	}

	go func() {
		if err := s.vmfusedCmd.Wait(); err != nil {
			s.logger.Error("vmfused process exited unexpectedly", "error", err)
		}
	}()

	s.vmfusedPID = s.vmfusedCmd.Process.Pid
	s.logger.Info("Started vmfused", "pid", s.vmfusedPID)

	time.Sleep(3 * time.Second)

	if s.vmfusedCmd.ProcessState != nil && s.vmfusedCmd.ProcessState.Exited() {
		return errors.Errorf("vmfused process exited unexpectedly")
	}

	return nil
}

func (s *VMFuseTestSuite) runVMFuseCtl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.vmfusectlBin, args...)
	cmd.Env = os.Environ()
	outputBuf := &bytes.Buffer{}
	cmd.Stdout = outputBuf
	cmd.Stderr = outputBuf
	// cmd.Stdout = io.MultiWriter(os.Stdout, outputBuf)
	// cmd.Stderr = io.MultiWriter(os.Stderr, outputBuf)

	err := cmd.Run()
	outputStr := strings.TrimSpace(outputBuf.String())

	if err != nil {
		s.logger.Error("vmfusectl command failed", "cmd", s.vmfusectlBin, "args", args, "output", outputStr, "error", err)
		return outputStr, err
	}

	s.logger.Info("vmfusectl succeeded", "cmd", s.vmfusectlBin, "args", args, "output", outputStr)
	return outputStr, nil
}

func (s *VMFuseTestSuite) runBasicMountTest() error {
	s.logger.Info("Running basic mount test")

	output, err := s.runVMFuseCtl("mount", "-t", "bind", s.testDir, s.mountTarget)
	if err != nil {
		return errors.Errorf("failed to mount directory: %w", err)
	}

	if !strings.Contains(output, "mounted") {
		return errors.Errorf("mount command did not indicate success: %s", output)
	}

	s.logger.Info("Basic mount test passed")
	return nil
}

func (s *VMFuseTestSuite) runFileAccessTest() error {
	s.logger.Info("Running file access test")

	time.Sleep(2 * time.Second)

	output, err := s.runVMFuseCtl("status", s.mountTarget)
	if err != nil {
		return errors.Errorf("failed to get mount status: %w", err)
	}
	s.logger.Info("Mount status", "output", output)

	output, err = s.runVMFuseCtl("list")
	if err != nil {
		return errors.Errorf("failed to list mounts: %w", err)
	}
	s.logger.Info("Mount list", "output", output)

	testFilePath := filepath.Join(s.mountTarget, "test-file.txt")

	var fileContent []byte
	for i := 0; i < 15; i++ {
		fileContent, err = os.ReadFile(testFilePath)
		if err == nil {
			break
		}
		s.logger.Info("Attempt to read file", "attempt", i+1, "file", testFilePath, "error", err)
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		return errors.Errorf("test file should be accessible through vmfuse mount: %w", err)
	}

	expectedContent := "Hello from vmfuse test!\nThis is a bind mount test via Linux VM and NFS\n"
	if string(fileContent) != expectedContent {
		return errors.Errorf("file content mismatch: expected %q, got %q", expectedContent, string(fileContent))
	}

	s.logger.Info("SUCCESS: Test file accessible through vmfuse mount!")
	return nil
}

func (s *VMFuseTestSuite) runUnmountTest() error {
	s.logger.Info("Running unmount test")

	output, err := s.runVMFuseCtl("umount", s.mountTarget)
	if err != nil {
		return errors.Errorf("failed to unmount directory: %w", err)
	}
	s.logger.Info("Unmount output", "output", output)

	time.Sleep(2 * time.Second)

	output, err = s.runVMFuseCtl("list")
	if err != nil {
		return errors.Errorf("failed to list mounts after unmount: %w", err)
	}
	s.logger.Info("Final mount list", "output", output)

	s.logger.Info("Unmount test passed")
	return nil
}

func (s *VMFuseTestSuite) cleanup() {
	s.logger.Info("Cleaning up vmfuse test...")
	s.cleanupOnce.Do(func() {
		s.logger.Info("Cleaning up vmfuse test...")

		if s.vmfusedCmd != nil && s.vmfusedCmd.Process != nil {
			pgid, err := syscall.Getpgid(s.vmfusedPID)
			if err == nil {
				syscall.Kill(-pgid, syscall.SIGTERM)
				s.logger.Info("Sent SIGTERM to process group", "pgid", pgid)
			} else {
				s.vmfusedCmd.Process.Signal(syscall.SIGTERM)
				s.logger.Info("Sent SIGTERM to process", "pid", s.vmfusedPID)
			}

			done := make(chan error, 1)
			go func() {
				done <- s.vmfusedCmd.Wait()
			}()

			select {
			case <-done:
				s.logger.Info("vmfused process terminated gracefully")
			case <-time.After(5 * time.Second):
				s.logger.Info("vmfused process did not terminate, forcing kill")
				if pgid, err := syscall.Getpgid(s.vmfusedPID); err == nil {
					syscall.Kill(-pgid, syscall.SIGKILL)
				} else {
					s.vmfusedCmd.Process.Kill()
				}
				s.vmfusedCmd.Wait()
			}
		}

		if s.vmfusectlBin != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, s.vmfusectlBin, "list")
			cmd.Env = os.Environ()
			cmd.Run()
		}

		if s.tmpDir != "" {
			time.Sleep(2 * time.Second)
			err := os.RemoveAll(s.tmpDir)
			if err != nil {
				s.logger.Warn("Failed to remove temporary directory", "dir", s.tmpDir, "error", err)
			} else {
				s.logger.Info("Cleaned up temporary directory", "dir", s.tmpDir)
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "pkill", "-f", "vmfused")
		cmd.Run()
	})
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
