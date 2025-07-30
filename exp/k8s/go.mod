module github.com/walteh/runm/exp/k8s

go 1.25rc2

replace github.com/walteh/runm => ../..

// copied from github.com/walteh/runm/go.mod
replace (
	github.com/Code-Hex/vz/v3 => ../../../vz
	github.com/containerd/console => ../../../console
	github.com/containerd/containerd/api => ../../../containerd/api
	github.com/containerd/containerd/v2 => ../../../containerd
	github.com/containerd/containerd/v2/pkg/sys => ../../../containerd/pkg/sys
	github.com/containerd/go-runc => ../../../go-runc
	github.com/containerd/nerdctl/mod/tigron => ../../../nerdctl/mod/tigron
	github.com/containerd/nerdctl/v2 => ../../../nerdctl
	github.com/containerd/stargz-snapshotter => ../../../stargz-snapshotter
	github.com/containerd/stargz-snapshotter/estargz => ../../../stargz-snapshotter/estargz
	github.com/containerd/ttrpc => ../../../ttrpc
	github.com/containers/gvisor-tap-vsock => ../../../gvisor-tap-vsock
	github.com/moby/buildkit => ../../../buildkit
	github.com/opencontainers/runc => ../../../runc
	github.com/pkg/errors => ../../../go-errors-2
	github.com/tonistiigi/fsutil => ../../../fsutil
	gitlab.com/tozd/go/errors => ../../../go-errors
	gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250611222258-0fe9a4bf489c
	k8s.io/kubernetes => ../../../kubernetes
)

// copied from k8s.io/kubernetes/go.mod
replace (
	k8s.io/api => ../../../kubernetes/staging/src/k8s.io/api
	k8s.io/apiextensions-apiserver => ../../../kubernetes/staging/src/k8s.io/apiextensions-apiserver
	k8s.io/apimachinery => ../../../kubernetes/staging/src/k8s.io/apimachinery
	k8s.io/apiserver => ../../../kubernetes/staging/src/k8s.io/apiserver
	k8s.io/cli-runtime => ../../../kubernetes/staging/src/k8s.io/cli-runtime
	k8s.io/client-go => ../../../kubernetes/staging/src/k8s.io/client-go
	k8s.io/cloud-provider => ../../../kubernetes/staging/src/k8s.io/cloud-provider
	k8s.io/cluster-bootstrap => ../../../kubernetes/staging/src/k8s.io/cluster-bootstrap
	k8s.io/code-generator => ../../../kubernetes/staging/src/k8s.io/code-generator
	k8s.io/component-base => ../../../kubernetes/staging/src/k8s.io/component-base
	k8s.io/component-helpers => ../../../kubernetes/staging/src/k8s.io/component-helpers
	k8s.io/controller-manager => ../../../kubernetes/staging/src/k8s.io/controller-manager
	k8s.io/cri-api => ../../../kubernetes/staging/src/k8s.io/cri-api
	k8s.io/cri-client => ../../../kubernetes/staging/src/k8s.io/cri-client
	k8s.io/csi-translation-lib => ../../../kubernetes/staging/src/k8s.io/csi-translation-lib
	k8s.io/dynamic-resource-allocation => ../../../kubernetes/staging/src/k8s.io/dynamic-resource-allocation
	k8s.io/endpointslice => ../../../kubernetes/staging/src/k8s.io/endpointslice
	k8s.io/externaljwt => ../../../kubernetes/staging/src/k8s.io/externaljwt
	k8s.io/kms => ../../../kubernetes/staging/src/k8s.io/kms
	k8s.io/kube-aggregator => ../../../kubernetes/staging/src/k8s.io/kube-aggregator
	k8s.io/kube-controller-manager => ../../../kubernetes/staging/src/k8s.io/kube-controller-manager
	k8s.io/kube-proxy => ../../../kubernetes/staging/src/k8s.io/kube-proxy
	k8s.io/kube-scheduler => ../../../kubernetes/staging/src/k8s.io/kube-scheduler
	k8s.io/kubectl => ../../../kubernetes/staging/src/k8s.io/kubectl
	k8s.io/kubelet => ../../../kubernetes/staging/src/k8s.io/kubelet
	k8s.io/metrics => ../../../kubernetes/staging/src/k8s.io/metrics
	k8s.io/mount-utils => ../../../kubernetes/staging/src/k8s.io/mount-utils
	k8s.io/pod-security-admission => ../../../kubernetes/staging/src/k8s.io/pod-security-admission
	k8s.io/sample-apiserver => ../../../kubernetes/staging/src/k8s.io/sample-apiserver
	k8s.io/sample-cli-plugin => ../../../kubernetes/staging/src/k8s.io/sample-cli-plugin
	k8s.io/sample-controller => ../../../kubernetes/staging/src/k8s.io/sample-controller
)

require k8s.io/kubectl v0.0.0-00010101000000-000000000000

require (
	github.com/Azure/go-ansiterm v0.0.0-20230124172434-306776ec8161 // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/chai2010/gettext-go v1.0.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/exponent-io/jsonpath v0.0.0-20210407135951-1de76d718b3f // indirect
	github.com/fatih/camelcase v1.0.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-errors/errors v1.4.2 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/lithammer/dedent v1.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/spf13/cobra v1.9.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	gitlab.com/tozd/go/errors v0.0.0-00010101000000-000000000000 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/oauth2 v0.27.0 // indirect
	golang.org/x/sync v0.12.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/term v0.30.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	golang.org/x/time v0.9.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/api v0.0.0 // indirect
	k8s.io/apimachinery v0.0.0 // indirect
	k8s.io/cli-runtime v0.0.0 // indirect
	k8s.io/client-go v0.0.0 // indirect
	k8s.io/component-base v0.0.0 // indirect
	k8s.io/component-helpers v0.0.0 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250710124328-f3f2b991d03b // indirect
	k8s.io/metrics v0.0.0 // indirect
	k8s.io/utils v0.0.0-20250604170112-4c0f3b243397 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/kustomize/api v0.20.1 // indirect
	sigs.k8s.io/kustomize/kustomize/v5 v5.7.1 // indirect
	sigs.k8s.io/kustomize/kyaml v0.20.1 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
