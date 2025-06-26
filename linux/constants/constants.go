package constants

const (
	RealInitPath                  = "/iniz"
	RootfsVirtioTag               = "rootfs"
	Ec1VirtioTag                  = "ec1"
	Ec1AbsPath                    = "/ec1"
	NewRootAbsPath                = "/newroot"
	MbinAbsPath                   = "/mbin"
	MbinVirtioTag                 = "/dev/vda"
	MbinFileName                  = "mbin.squashfs"
	MbinFSType                    = "squashfs"
	BundleVirtioTag               = "bundle"
	SupplementalVirioFSMountsFile = "/supplemental-virio-fs-mounts.json"
	VsockPidFile                  = "/ec1.vsock.pid"
	ContainerCmdlineFile          = "/container-cmdline.json"
	ContainerManifestFile         = "/container-manifest.json"
	ContainerSpecFile             = "/container-oci-spec.json"
	ContainerMountsFile           = "/container-mounts.json"
	ContainerTimesyncFile         = "/timesync"
	ContainerReadyFile            = "/ready"
	TempVirtioTag                 = "temp"
	RunmVsockPort                 = 2019
	VsockStdinPort                = 2020
	VsockStdoutPort               = 2021
	VsockStderrPort               = 2022
	VsockOtelPort                 = 3098
)
