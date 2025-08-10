package constants

const (
	RealInitPath                      = "/iniz"
	RootfsVirtioTag                   = "rootfs"
	Ec1VirtioTag                      = "ec1"
	Ec1AbsPath                        = "/ec1"
	MShareVirtioTag                   = "mshare"
	MShareAbsPath                     = "/mshare"
	NewRootAbsPath                    = "/newroot"
	MbinAbsPath                       = "/mbin"
	MbinVirtioTag                     = "/dev/vda"
	MbinFileName                      = "mbin.squashfs"
	MbinVMFUSEFileName                = "mbin-vmfuse.squashfs"
	MbinFSType                        = "squashfs"
	ZoneInfoVirtioTag                 = "zoneinfo"
	RootfsMbindVirtioTag              = "rootfs-mbind"
	CaCertsVirtioTag                  = "cacerts"
	BundleVirtioTag                   = "bundle"
	SupplementalVirioFSMountsFile     = "/supplemental-virio-fs-mounts.json"
	VsockPidFile                      = "/ec1.vsock.pid"
	ContainerCmdlineFile              = "/container-cmdline.json"
	ContainerManifestFile             = "/container-manifest.json"
	ContainerSpecFile                 = "/container-oci-spec.json"
	ContainerMountsFile               = "/container-mounts.json"
	ContainerRootfsMountsFile         = "/container-rootfs-mounts.json"
	ContainerTimesyncFile             = "/timesync"
	ContainerReadyFile                = "/ready"
	TempVirtioTag                     = "temp"
	RunmGuestServerVsockPort          = 2019
	VsockStdinPort                    = 2020
	VsockStdoutPort                   = 2021
	VsockStderrPort                   = 2022
	VsockAllocationMinPort            = 6000
	VsockOtelPort                     = 3097
	VsockDelimitedWriterProxyPort     = 3098
	VsockRawWriterProxyPort           = 3099
	VsockDebugPort                    = 2018
	VsockPprofPort                    = 2017
	VsockCgroupExporterPort           = 2016
	MsockBasePort                     = 9000
	VsockDelimitedLogProxyDelimiter   = rune(0x1F) // ASCII Record Separator
	MbindSeparator                    = "|"
	DelimitedWriterProxyGuestUnixPath = "/tmp/runm-delim-writer-proxy.sock"
	RawWriterProxyGuestUnixPath       = "/tmp/runm-raw-writer-proxy.sock"
	DelimitedLogProxyGuestTCPAddress  = "0.0.0.0:3101"
	RawLogProxyGuestTCPAddress        = "0.0.0.0:3102"

	VsockJSONLogProxyPort   = 3100
	RunmHostServerVsockPort = 2023
)
