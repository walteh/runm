package vmm

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/walteh/runm/core/gvnet"
	"github.com/walteh/runm/core/virt/host"
	"github.com/walteh/runm/core/virt/virtio"
	"github.com/walteh/runm/linux/constants"
	"gitlab.com/tozd/go/errors"
)

type DefaultVMConfig struct {
	WorkingDir          string
	Devices             []virtio.VirtioDevice
	MagicHostPort       uint16
	Timezone            string
	StartTime           time.Time
	GvProxy             gvnet.Proxy
	MshareDirTags       map[string]string
	MshareSockPorts     map[string]uint16
	HostRuntimeBuildDir string
}

func (me *DefaultVMConfig) MshareDirsWithTags() string {
	var tags []string
	for dir, tag := range me.MshareDirTags {
		tags = append(tags, tag+constants.MbindSeparator+dir)
	}
	return strings.Join(tags, ",")
}

func (me *DefaultVMConfig) MshareSocketsWithPorts() string {
	var ports []string
	for sock, port := range me.MshareSockPorts {
		ports = append(ports, sock+constants.MbindSeparator+strconv.Itoa(int(port)))
	}
	return strings.Join(ports, ",")
}

func (me *DefaultVMConfig) StartTimeUnixNanoString() string {
	return strconv.FormatInt(me.StartTime.UnixNano(), 10)
}

func newDefaultLinuxVM(ctx context.Context, vmid string, mbinFile string, mshareFiles map[string]any, mshareDirs []string, mshareSocks []string) (*DefaultVMConfig, error) {
	start := time.Now()

	workingDir, err := host.EmphiricalVMCacheDir(ctx, vmid)
	if err != nil {
		return nil, err
	}

	linuxRuntimeBuildDir := os.Getenv("LINUX_RUNTIME_BUILD_DIR")
	if linuxRuntimeBuildDir == "" {
		return nil, errors.New("LINUX_RUNTIME_BUILD_DIR is not set")
	}

	if err = os.CopyFS(filepath.Join(workingDir, "build"), os.DirFS(linuxRuntimeBuildDir)); err != nil {
		return nil, errors.Errorf("copying build directory: %w", err)
	}

	linuxRuntimeBuildDir = filepath.Join(workingDir, "build")

	tzDev, err := virtio.VirtioFsNew("/usr/share/zoneinfo", constants.ZoneInfoVirtioTag)
	if err != nil {
		return nil, errors.Errorf("creating zoneinfo virtio device: %w", err)
	}

	// Mount CA certificates for TLS verification (macOS has certs at /etc/ssl/)
	caCertsDev, err := virtio.VirtioFsNew("/etc/ssl", constants.CaCertsVirtioTag)
	if err != nil {
		return nil, errors.Errorf("creating ca certs virtio device: %w", err)
	}

	consoleDev := &virtio.VirtioSerialLogFile{
		Path:   filepath.Join(workingDir, "console.log"),
		Append: false,
	}

	netdev, hostIPPort, err := PrepareVirtualNetwork(ctx)
	if err != nil {
		return nil, errors.Errorf("creating net device: %w", err)
	}

	// Get the host timezone
	hostTimezone, err := getHostTimezone()
	if err != nil {
		return nil, errors.Errorf("getting host timezone: %w", err)
	}

	mbinDev, err := virtio.VirtioBlkNew(filepath.Join(linuxRuntimeBuildDir, mbinFile))
	if err != nil {
		return nil, errors.Errorf("creating mbin virtio device: %w", err)
	}

	mshareDev, err := makeMShareFilesDevice(ctx, workingDir, mshareFiles)
	if err != nil {
		return nil, errors.Errorf("creating mshare virtio device: %w", err)
	}

	devices := []virtio.VirtioDevice{
		netdev.VirtioNetDevice(),
		&virtio.VirtioBalloon{},
		caCertsDev,
		tzDev,
		consoleDev,
		mbinDev,
		mshareDev,
		&virtio.VirtioRng{},
		&virtio.VirtioVsock{},
	}

	defs := &DefaultVMConfig{
		WorkingDir:          workingDir,
		Devices:             devices,
		MagicHostPort:       hostIPPort,
		Timezone:            hostTimezone,
		GvProxy:             netdev,
		HostRuntimeBuildDir: linuxRuntimeBuildDir,
		StartTime:           start,
		MshareDirTags:       make(map[string]string),
		MshareSockPorts:     make(map[string]uint16),
	}

	for _, msharedir := range mshareDirs {
		tag := "mshare-dir-" + quickHash(msharedir)
		dev, err := virtio.VirtioFsNew(msharedir, tag)
		if err != nil {
			return nil, errors.Errorf("creating mshare virtio device for %s: %w", msharedir, err)
		}
		slog.InfoContext(ctx, "created mshare virtio device", "msharedir", msharedir, "tag", tag)
		defs.Devices = append(defs.Devices, dev)
		defs.MshareDirTags[msharedir] = tag
	}

	for i, msockbind := range mshareSocks {
		defs.MshareSockPorts[msockbind] = constants.MsockBasePort + uint16(i)
	}

	return defs, nil
}

func getHostTimezone() (string, error) {

	localTimeRef, err := os.Readlink("/etc/localtime")
	if err != nil {
		return "", errors.Errorf("reading localtime: %w", err)
	}

	zisplit := strings.Split(localTimeRef, "zoneinfo/")
	if len(zisplit) != 2 {
		return "", errors.Errorf("invalid localtime reference: %s", localTimeRef)
	}

	loc, err := time.LoadLocation(zisplit[1])
	if err != nil {
		return "", errors.Errorf("loading location: %w", err)
	}

	return loc.String(), nil
}

func makeMShareFilesDevice(ctx context.Context, wrkdir string, files map[string]any) (virtio.VirtioDevice, error) {
	mshareDataPath := filepath.Join(wrkdir, constants.MShareAbsPath)

	err := os.MkdirAll(mshareDataPath, 0755)
	if err != nil {
		return nil, errors.Errorf("creating block device directory: %w", err)
	}

	filebytes := make(map[string][]byte)
	for name, file := range files {
		switch v := file.(type) {
		case []byte:
			filebytes[name] = v
		case string:
			filebytes[name] = []byte(v)
		default:
			bytes, err := json.Marshal(v)
			if err != nil {
				return nil, errors.Errorf("marshalling file content: %w", err)
			}
			filebytes[name] = bytes
		}
	}

	// specBytes, err := json.Marshal(ctrconfig)
	// if err != nil {
	// 	return nil, nil, errors.Errorf("marshalling spec: %w", err)
	// }

	// mountBytes, err := json.Marshal(rootfsMounts)
	// if err != nil {
	// 	return nil, nil, errors.Errorf("marshalling rootfs mounts: %w", err)
	// }

	// files := map[string][]byte{
	// 	constants.ContainerSpecFile:         specBytes,
	// 	constants.ContainerRootfsMountsFile: mountBytes,
	// }

	if err := os.MkdirAll(mshareDataPath, 0755); err != nil {
		return nil, errors.Errorf("creating mshare data path: %w", err)
	}

	for name, file := range filebytes {
		filePath := filepath.Join(mshareDataPath, name)
		err = os.WriteFile(filePath, file, 0644)
		if err != nil {
			return nil, errors.Errorf("writing file to block device: %w", err)
		}
	}

	device, err := virtio.VirtioFsNew(mshareDataPath, constants.MShareVirtioTag)
	if err != nil {
		return nil, errors.Errorf("creating mshare virtio device: %w", err)
	}

	return device, nil
}
