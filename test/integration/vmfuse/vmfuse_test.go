package vmfuse

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/walteh/runm/pkg/logging"
	"github.com/walteh/runm/test/integration/env"
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

func TestVMFuseIntegration(t *testing.T) {
	suite := setupTestSuite(t)
	defer suite.cleanup(t)

	t.Run("BasicMountTest", suite.testBasicMount)
	t.Run("FileAccessTest", suite.testFileAccess)
	t.Run("UnmountTest", suite.testUnmount)
}

func setupTestSuite(t *testing.T) *VMFuseTestSuite {
	t.Helper()

	vmfusedBin := os.Getenv("VMFUSED_BIN")
	vmfusectlBin := os.Getenv("VMFUSECTL_BIN")
	linuxRuntimeDir := os.Getenv("LINUX_RUNTIME_BUILD_DIR")

	require.NotEmpty(t, vmfusedBin, "VMFUSED_BIN environment variable is not set")
	require.NotEmpty(t, vmfusectlBin, "VMFUSECTL_BIN environment variable is not set")
	require.NotEmpty(t, linuxRuntimeDir, "LINUX_RUNTIME_BUILD_DIR environment variable is not set")

	require.FileExists(t, vmfusedBin, "vmfused binary not found")
	require.FileExists(t, vmfusectlBin, "vmfusectl binary not found")
	require.DirExists(t, linuxRuntimeDir, "Linux runtime directory not found")

	tmpDir, err := os.MkdirTemp("", "vmfuse-test-*")
	require.NoError(t, err, "Failed to create temporary directory")

	testDir := filepath.Join(tmpDir, "test-source")
	mountTarget := filepath.Join(tmpDir, "test-mount")
	vmfusedSock := filepath.Join(tmpDir, "vmfused.sock")

	err = os.MkdirAll(testDir, 0755)
	require.NoError(t, err, "Failed to create test directory")

	err = os.MkdirAll(mountTarget, 0755)
	require.NoError(t, err, "Failed to create mount target directory")

	suite := &VMFuseTestSuite{
		vmfusedBin:      vmfusedBin,
		vmfusectlBin:    vmfusectlBin,
		linuxRuntimeDir: linuxRuntimeDir,
		tmpDir:          tmpDir,
		testDir:         testDir,
		mountTarget:     mountTarget,
		vmfusedSock:     vmfusedSock,
	}

	if err := env.SetupContainerdLogReceiver(t.Context()); err != nil {
		t.Fatalf("setting up shim: %v", err)
	}

	suite.logger = logging.NewDefaultDevLogger("vmfuse-test", os.Stdout)

	suite.setupTestFiles(t)
	suite.startVMFused(t)

	return suite
}

func (s *VMFuseTestSuite) setupTestFiles(t *testing.T) {
	t.Helper()

	testContent := "Hello from vmfuse test!\nThis is a bind mount test via Linux VM and NFS\n"
	testFilePath := filepath.Join(s.testDir, "test-file.txt")

	err := os.WriteFile(testFilePath, []byte(testContent), 0644)
	require.NoError(t, err, "Failed to create test file")

	s.logger.Info("Created test file", "file", testFilePath)
}

func (s *VMFuseTestSuite) startVMFused(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.vmfusedCmd = exec.CommandContext(ctx, s.vmfusedBin)
	s.vmfusedCmd.Env = append(os.Environ(),
		fmt.Sprintf("LINUX_RUNTIME_BUILD_DIR=%s", s.linuxRuntimeDir),
		fmt.Sprintf("VMFUSED_ADDRESS=unix://%s", s.vmfusedSock),
	)
	s.vmfusedCmd.Stdout = os.Stdout
	s.vmfusedCmd.Stderr = os.Stderr

	s.vmfusedCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	err := s.vmfusedCmd.Start()
	require.NoError(t, err, "Failed to start vmfused")

	s.vmfusedPID = s.vmfusedCmd.Process.Pid
	s.logger.Info("Started vmfused", "pid", s.vmfusedPID)

	time.Sleep(3 * time.Second)

	if s.vmfusedCmd.ProcessState != nil && s.vmfusedCmd.ProcessState.Exited() {
		t.Fatalf("vmfused process exited unexpectedly")
	}
}

func (s *VMFuseTestSuite) runVMFuseCtl(t *testing.T, args ...string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.vmfusectlBin, args...)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("VMFUSED_ADDRESS=unix://%s", s.vmfusedSock),
	)
	outputBuf := &bytes.Buffer{}
	cmd.Stdout = io.MultiWriter(os.Stdout, outputBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, outputBuf)

	err := cmd.Run()
	outputStr := strings.TrimSpace(outputBuf.String())

	if err != nil {
		s.logger.Error("vmfusectl command failed", "cmd", s.vmfusectlBin, "args", args, "output", outputStr, "error", err)
		return outputStr, err
	}

	s.logger.Info("vmfusectl succeeded", "cmd", s.vmfusectlBin, "args", args, "output", outputStr)
	return outputStr, nil
}

func (s *VMFuseTestSuite) testBasicMount(t *testing.T) {
	output, err := s.runVMFuseCtl(t, "mount", "-t", "bind", s.testDir, s.mountTarget)
	require.NoError(t, err, "Failed to mount directory")
	assert.Contains(t, output, "mounted", "Mount command should indicate success")
}

func (s *VMFuseTestSuite) testFileAccess(t *testing.T) {
	time.Sleep(2 * time.Second)

	output, err := s.runVMFuseCtl(t, "status", s.mountTarget)
	require.NoError(t, err, "Failed to get mount status")
	s.logger.Info("Mount status", "output", output)

	output, err = s.runVMFuseCtl(t, "list")
	require.NoError(t, err, "Failed to list mounts")
	s.logger.Info("Mount list", "output", output)

	testFilePath := filepath.Join(s.mountTarget, "test-file.txt")

	var fileContent []byte
	for i := 0; i < 15; i++ {
		fileContent, err = os.ReadFile(testFilePath)
		if err == nil {
			break
		}
		s.logger.Info("Attempt to read file", "attempt", i+1, "file", testFilePath)
		time.Sleep(1 * time.Second)
	}

	require.NoError(t, err, "Test file should be accessible through vmfuse mount")
	expectedContent := "Hello from vmfuse test!\nThis is a bind mount test via Linux VM and NFS\n"
	assert.Equal(t, expectedContent, string(fileContent), "File content should match")

	s.logger.Info("Test file accessible through vmfuse mount")
}

func (s *VMFuseTestSuite) testUnmount(t *testing.T) {
	output, err := s.runVMFuseCtl(t, "umount", s.mountTarget)
	require.NoError(t, err, "Failed to unmount directory")
	s.logger.Info("Unmount output", "output", output)

	time.Sleep(2 * time.Second)

	output, err = s.runVMFuseCtl(t, "list")
	require.NoError(t, err, "Failed to list mounts after unmount")
	s.logger.Info("Final mount list", "output", output)
}

func (s *VMFuseTestSuite) cleanup(t *testing.T) {
	s.cleanupOnce.Do(func() {
		t.Log("Cleaning up vmfuse test...")

		if s.vmfusedCmd != nil && s.vmfusedCmd.Process != nil {
			pgid, err := syscall.Getpgid(s.vmfusedPID)
			if err == nil {
				syscall.Kill(-pgid, syscall.SIGTERM)
				s.logger.Info("Sent SIGTERM to process group", "pid", pgid)
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
				t.Log("vmfused process terminated gracefully")
			case <-time.After(5 * time.Second):
				t.Log("vmfused process did not terminate, forcing kill")
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
			cmd.Env = append(os.Environ(), fmt.Sprintf("VMFUSED_ADDRESS=unix://%s", s.vmfusedSock))
			cmd.Run()
		}

		if s.tmpDir != "" {
			time.Sleep(2 * time.Second)
			err := os.RemoveAll(s.tmpDir)
			if err != nil {
				t.Logf("Warning: failed to remove temporary directory %s: %v", s.tmpDir, err)
			} else {
				t.Logf("Cleaned up temporary directory: %s", s.tmpDir)
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

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
}
