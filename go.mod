module github.com/walteh/runm

go 1.25rc2

exclude github.com/containerd/nerdctl/mod/tigron v0.0.0

// forks
replace (
	// forks used for debugging
	github.com/Code-Hex/vz/v3 => ../vz
	github.com/arianvp/cgroup-exporter => ../cgroup-exporter
	github.com/containerd/console => ../console
	// forks with required logical changes
	github.com/containerd/containerd/api => ../containerd/api
	github.com/containerd/containerd/v2 => ../containerd
	github.com/containerd/containerd/v2/pkg/sys => ../containerd/pkg/sys
	github.com/containerd/go-runc => ../go-runc
	github.com/containerd/nerdctl/mod/tigron => ../nerdctl/mod/tigron
	github.com/containerd/nerdctl/v2 => ../nerdctl
	github.com/containerd/stargz-snapshotter => ../stargz-snapshotter
	github.com/containerd/stargz-snapshotter/estargz => ../stargz-snapshotter/estargz
	github.com/containerd/ttrpc => ../ttrpc
	github.com/containers/gvisor-tap-vsock => ../gvisor-tap-vsock
	github.com/google/cadvisor => ../cadvisor
	github.com/moby/buildkit => ../buildkit
	github.com/opencontainers/runc => ../runc
	github.com/pkg/errors => ../go-errors/pkg-errors
	github.com/tonistiigi/fsutil => ../fsutil
	gitlab.com/tozd/go/errors => ../go-errors
)

replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250807194038-c9af560a03d9 // this needs to be updated manually (because the 'go' branch is the only one that works)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.6-20250717185734-6c6e0d3c608e.1
	buf.build/go/protovalidate v0.14.0
	github.com/Code-Hex/vz/v3 v3.7.0
	github.com/abbot/go-http-auth v0.4.0
	github.com/arianvp/cgroup-exporter v0.0.0-00010101000000-000000000000
	github.com/cavaliergopher/grab/v3 v3.0.1
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/containerd/cgroups/v3 v3.0.5
	github.com/containerd/console v1.0.5
	github.com/containerd/containerd v1.7.27
	github.com/containerd/containerd/api v1.9.0
	github.com/containerd/containerd/v2 v2.1.4
	github.com/containerd/errdefs v1.0.0
	github.com/containerd/errdefs/pkg v0.3.0
	github.com/containerd/fifo v1.1.0
	github.com/containerd/go-runc v1.1.0
	github.com/containerd/log v0.1.0
	github.com/containerd/nerdctl/v2 v2.0.0-00010101000000-000000000000
	github.com/containerd/plugin v1.0.0
	github.com/containerd/stargz-snapshotter v0.17.0
	github.com/containerd/stargz-snapshotter/cmd v0.17.0
	github.com/containerd/ttrpc v1.2.7
	github.com/containerd/typeurl/v2 v2.2.3
	github.com/containers/common v0.64.0
	github.com/containers/gvisor-tap-vsock v0.8.6
	github.com/crc-org/vfkit v0.6.2-0.20250415145558-4b7cae94e86a
	github.com/creack/pty v1.1.24
	github.com/docker/go-metrics v0.0.1
	github.com/fatih/color v1.18.0
	github.com/goforj/godump v1.5.0
	github.com/google/cadvisor v0.53.0
	github.com/hashicorp/go-hclog v1.6.3
	github.com/hashicorp/go-plugin v1.6.3
	github.com/kr/pty v1.1.8
	github.com/lima-vm/go-qcow2reader v0.6.0
	github.com/mholt/archives v0.1.3
	github.com/moby/buildkit v0.0.0-00010101000000-000000000000
	github.com/moby/sys/reexec v0.1.0
	github.com/moby/sys/signal v0.7.1
	github.com/moby/sys/user v0.4.0
	github.com/moby/sys/userns v0.1.0
	github.com/muesli/termenv v0.16.0
	github.com/nxadm/tail v1.4.11
	github.com/opencontainers/cgroups v0.0.4
	github.com/opencontainers/runc v1.3.0
	github.com/opencontainers/runtime-spec v1.2.2-0.20250401095657-e935f995dd67
	github.com/opencontainers/selinux v1.12.0
	github.com/pelletier/go-toml v1.9.5
	github.com/pelletier/go-toml/v2 v2.2.4
	github.com/pkg/term v1.1.0
	github.com/prometheus/client_golang v1.23.0
	github.com/rs/xid v1.6.0
	github.com/samber/slog-multi v1.4.1
	github.com/soheilhy/cmux v0.1.5
	github.com/spf13/cobra v1.9.1
	github.com/spf13/pflag v1.0.7
	github.com/stretchr/testify v1.10.0
	github.com/urfave/cli v1.22.17
	github.com/urfave/cli/v2 v2.27.7
	github.com/veqryn/slog-context v0.8.0
	github.com/vishvananda/netlink v1.3.1
	gitlab.com/tozd/go/errors v0.10.0
	go.opentelemetry.io/contrib/bridges/otelslog v0.12.0
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.62.0
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.13.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.37.0
	go.opentelemetry.io/otel/log v0.13.0
	go.opentelemetry.io/otel/sdk/log v0.13.0
	go.opentelemetry.io/otel/sdk/metric v1.37.0
	go.uber.org/atomic v1.11.0
	golang.org/x/mod v0.26.0
	google.golang.org/protobuf v1.36.6
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	gvisor.dev/gvisor v0.0.0-20250509002459-06cdc4c49840
	k8s.io/klog/v2 v2.130.1
	k8s.io/utils v0.0.0-20250502105355-0f33e8f1c979
)

require (
	dario.cat/mergo v1.0.2 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.16.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.8.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.10.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.5.0 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.3.2 // indirect
	github.com/Code-Hex/go-infinity-channel v1.0.0 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/STARRY-S/zip v0.2.2 // indirect
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/anchore/go-struct-converter v0.0.0-20221118182256-c68fdcfa2092 // indirect
	github.com/andybalholm/brotli v1.1.2-0.20250424173009-453214e765f3 // indirect
	github.com/apparentlymart/go-cidr v1.1.0 // indirect
	github.com/armon/circbuf v0.0.0-20190214190532-5111143e8da2 // indirect
	github.com/aws/aws-sdk-go-v2 v1.36.3 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.3 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.29.14 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.67 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.30 // indirect
	github.com/aws/aws-sdk-go-v2/feature/s3/manager v1.17.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.3.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.17.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.58.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.25.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.30.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.19 // indirect
	github.com/aws/smithy-go v1.22.3 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/sevenzip v1.6.0 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.2.3-0.20250311203215-f60798e515dc // indirect
	github.com/charmbracelet/x/ansi v0.8.0 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13-0.20250311204145-2c3ea96c31dd // indirect
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/checkpoint-restore/checkpointctl v1.3.0 // indirect
	github.com/checkpoint-restore/go-criu/v7 v7.2.0 // indirect
	github.com/compose-spec/compose-go/v2 v2.8.1 // indirect
	github.com/containerd/accelerated-container-image v1.3.0 // indirect
	github.com/containerd/btrfs/v2 v2.0.0 // indirect
	github.com/containerd/fuse-overlayfs-snapshotter/v2 v2.1.6 // indirect
	github.com/containerd/go-cni v1.1.12 // indirect
	github.com/containerd/imgcrypt/v2 v2.0.1 // indirect
	github.com/containerd/nerdctl/mod/tigron v0.0.0-00010101000000-000000000000 // indirect
	github.com/containerd/nri v0.8.0 // indirect
	github.com/containerd/nydus-snapshotter v0.15.2 // indirect
	github.com/containerd/platforms v1.0.0-rc.1 // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.17.0 // indirect
	github.com/containerd/stargz-snapshotter/ipfs v0.16.3 // indirect
	github.com/containerd/zfs/v2 v2.0.0-rc.0 // indirect
	github.com/containernetworking/cni v1.3.0 // indirect
	github.com/containernetworking/plugins v1.7.1 // indirect
	github.com/containers/ocicrypt v1.2.1 // indirect
	github.com/coreos/go-iptables v0.8.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/cyphar/filepath-securejoin v0.4.1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dimchansky/utfbom v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/djherbis/times v1.6.0 // indirect
	github.com/docker/cli v28.3.3+incompatible // indirect
	github.com/docker/docker v28.3.3+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/docker/go-events v0.0.0-20190806004212-e31b211e4f1c // indirect
	github.com/dsnet/compress v0.0.2-0.20230904184137-39efe44ab707 // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/euank/go-kmsg-parser v2.0.0+incompatible // indirect
	github.com/fahedouch/go-logrotate v0.3.0 // indirect
	github.com/felixge/fgprof v0.9.3 // indirect
	github.com/fluent/fluent-logger-golang v1.10.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-jose/go-jose/v4 v4.0.5 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/gofrs/flock v0.12.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/gopacket v1.1.19 // indirect
	github.com/google/pprof v0.0.0-20250403155104-27863c87afa6 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus v1.1.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.1.0 // indirect
	github.com/hanwen/go-fuse/v2 v2.8.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-immutable-radix/v2 v2.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/hashicorp/yamux v0.1.1 // indirect
	github.com/in-toto/in-toto-golang v0.9.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/insomniacslk/dhcp v0.0.0-20250109001534-8abf58130905 // indirect
	github.com/intel/goresctrl v0.9.0 // indirect
	github.com/ipfs/go-cid v0.5.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/karrick/godirwalk v1.17.0 // indirect
	github.com/klauspost/compress v1.18.1-0.20250502091416-dee68d8e897e // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/klauspost/pgzip v1.2.6 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/linuxkit/virtsock v0.0.0-20220523201153-1a23e78aa7a2 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mattn/go-shellwords v1.0.12 // indirect
	github.com/miekg/dns v1.1.67 // indirect
	github.com/miekg/pkcs11 v1.1.1 // indirect
	github.com/mikelolasagasti/xz v1.0.1 // indirect
	github.com/minio/minlz v1.0.0 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mistifyio/go-zfs v2.1.1+incompatible // indirect
	github.com/mistifyio/go-zfs/v3 v3.0.1 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.1.0 // indirect
	github.com/moby/locker v1.0.1 // indirect
	github.com/moby/patternmatcher v0.6.0 // indirect
	github.com/moby/profiles/seccomp v0.1.0 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/moby/sys/capability v0.4.0 // indirect
	github.com/moby/sys/mount v0.3.4 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/symlink v0.3.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/mrunalp/fileutils v0.5.1 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multiaddr v0.16.0 // indirect
	github.com/multiformats/go-multibase v0.2.0 // indirect
	github.com/multiformats/go-multihash v0.2.3 // indirect
	github.com/multiformats/go-varint v0.0.7 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/nwaples/rardecode/v2 v2.1.0 // indirect
	github.com/oklog/run v1.0.0 // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20250523060157-0ea5ed0382a2 // indirect
	github.com/package-url/packageurl-go v0.1.1 // indirect
	github.com/petermattis/goid v0.0.0-20250319124200-ccd6737f222a // indirect
	github.com/philhofer/fwd v1.1.3-0.20240916144458-20a13a1f6b7c // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/profile v1.7.0 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.65.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/rootless-containers/bypass4netns v0.4.2 // indirect
	github.com/rootless-containers/rootlesskit/v2 v2.3.5 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/samber/lo v1.51.0 // indirect
	github.com/samber/slog-common v0.19.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.1 // indirect
	github.com/sasha-s/go-deadlock v0.3.5 // indirect
	github.com/seccomp/libseccomp-golang v0.11.0 // indirect
	github.com/secure-systems-lab/go-securesystemslib v0.9.0 // indirect
	github.com/shibumi/go-pathspec v1.3.0 // indirect
	github.com/smallstep/pkcs7 v0.1.1 // indirect
	github.com/sorairolake/lzip-go v0.3.5 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spdx/tools-golang v0.5.5 // indirect
	github.com/stefanberger/go-pkcs11uri v0.0.0-20230803200340-78284954bff6 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/tchap/go-patricia/v2 v2.3.3 // indirect
	github.com/tinylib/msgp v1.3.0 // indirect
	github.com/tonistiigi/dchapes-mode v0.0.0-20250318174251-73d941a28323 // indirect
	github.com/tonistiigi/fsutil v0.0.0-20250605211040-586307ad452f // indirect
	github.com/tonistiigi/go-actions-cache v0.0.0-20250626083717-378c5ed1ddd9 // indirect
	github.com/tonistiigi/go-archvariant v1.0.0 // indirect
	github.com/tonistiigi/go-csvvalue v0.0.0-20240814133006-030d3b2625d0 // indirect
	github.com/tonistiigi/units v0.0.0-20180711220420-6950e57a87ea // indirect
	github.com/tonistiigi/vt100 v0.0.0-20240514184818-90bafcd6abab // indirect
	github.com/u-root/uio v0.0.0-20240224005618-d2acac8f3701 // indirect
	github.com/ulikunitz/xz v0.5.12 // indirect
	github.com/vbatts/tar-split v0.12.1 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/walteh/go-errors v0.0.0-20250811132312-25eac94ea3ee // indirect
	github.com/walteh/go-errors/pkg-errors v0.0.0-20250811132312-25eac94ea3ee // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xhit/go-str2duration/v2 v2.1.0 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	github.com/yuchanns/srslog v1.1.0 // indirect
	go.etcd.io/bbolt v1.4.2 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.60.0 // indirect
	go.opentelemetry.io/otel/exporters/jaeger v1.17.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/prometheus v0.42.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	go4.org v0.0.0-20230225012048-214862532bf5 // indirect
	golang.org/x/crypto v0.40.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/term v0.33.0 // indirect
	golang.org/x/time v0.11.0 // indirect
	golang.org/x/tools v0.34.0 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gotest.tools/v3 v3.5.2 // indirect
	k8s.io/api v0.33.3 // indirect
	k8s.io/apimachinery v0.33.3 // indirect
	k8s.io/client-go v0.33.3 // indirect
	k8s.io/cri-api v0.33.3 // indirect
	k8s.io/kube-openapi v0.0.0-20250318190949-c8a335a9a2ff // indirect
	lukechampine.com/blake3 v1.3.0 // indirect
	sigs.k8s.io/json v0.0.0-20241010143419-9aa6b5e7a4b3 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.6.0 // indirect
	sigs.k8s.io/yaml v1.5.0 // indirect
	tags.cncf.io/container-device-interface v1.0.1 // indirect
	tags.cncf.io/container-device-interface/specs-go v1.0.0 // indirect
)

require (
	cel.dev/expr v0.24.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/Microsoft/hcsshim v0.13.1-0.20250728212919-1ee5fce7ed8f // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/cilium/ebpf v0.18.0 // indirect
	github.com/containerd/continuity v0.4.5
	github.com/containerd/otelttrpc v0.1.0
	github.com/coreos/go-systemd/v22 v22.5.1-0.20231103132048-7d375ecc2b09
	github.com/docker/go-units v0.5.0
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/google/cel-go v0.25.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.1 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/mdlayher/vsock v1.2.1
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.4-0.20230606125235-dd1b4c2e81af
	github.com/stoewer/go-strcase v1.3.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.61.0 // indirect
	go.opentelemetry.io/otel v1.37.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.37.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.37.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.36.0 // indirect
	go.opentelemetry.io/otel/metric v1.37.0
	go.opentelemetry.io/otel/sdk v1.37.0
	go.opentelemetry.io/otel/trace v1.37.0
	go.opentelemetry.io/proto/otlp v1.7.0 // indirect
	golang.org/x/exp v0.0.0-20250506013437-ce4c2cf36ca6 // indirect
	golang.org/x/net v0.42.0
	golang.org/x/sync v0.16.0
	golang.org/x/sys v0.34.0
	golang.org/x/text v0.27.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250721164621-a45f3dfb1074
	google.golang.org/grpc v1.74.2
)
