// This project is a fork of sigs.k8s.io/scheduler-plugins
// The pkg/computegardener directory contains our custom scheduler code
// while other directories contain code derived from the upstream project
module github.com/elevated-systems/compute-gardener-scheduler

go 1.24.0

require (
	github.com/carbon-aware/cloudinfo v0.0.0-20250605223946-04933c6a3dc4
	github.com/prometheus/client_golang v1.21.1
	github.com/prometheus/common v0.62.0
	github.com/stretchr/testify v1.10.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.33.1
	k8s.io/apimachinery v0.33.1
	k8s.io/client-go v0.33.1
	k8s.io/code-generator v0.31.2
	k8s.io/component-base v0.32.1
	k8s.io/klog/v2 v2.130.1
	k8s.io/kubernetes v1.31.2
	k8s.io/metrics v0.31.2
	k8s.io/utils v0.0.0-20250604170112-4c0f3b243397
	sigs.k8s.io/logtools v0.9.0
	sigs.k8s.io/scheduler-plugins v0.31.2-devel
)

require (
	cel.dev/expr v0.19.1 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/OneOfOne/xxhash v1.2.8 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.0 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/diktyo-io/appgroup-api v1.0.4-alpha // indirect
	github.com/diktyo-io/networktopology-api v1.0.5-alpha // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.8.0 // indirect
	github.com/fxamacker/cbor/v2 v2.8.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.21.1 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/cel-go v0.23.2 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.1 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/k8stopologyawareschedwg/noderesourcetopology-api v0.1.2 // indirect
	github.com/k8stopologyawareschedwg/podfingerprint v0.2.2 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runc v1.2.5 // indirect
	github.com/opencontainers/selinux v1.11.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/spf13/cobra v1.9.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.etcd.io/etcd/api/v3 v3.5.18 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.18 // indirect
	go.etcd.io/etcd/client/v3 v3.5.18 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.59.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.59.0 // indirect
	go.opentelemetry.io/otel v1.34.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.34.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.34.0 // indirect
	go.opentelemetry.io/otel/metric v1.34.0 // indirect
	go.opentelemetry.io/otel/sdk v1.34.0 // indirect
	go.opentelemetry.io/otel/trace v1.34.0 // indirect
	go.opentelemetry.io/proto/otlp v1.5.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/exp v0.0.0-20250218142911-aa4b98e5adaa // indirect
	golang.org/x/mod v0.25.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sync v0.15.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/term v0.32.0 // indirect
	golang.org/x/text v0.26.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	golang.org/x/tools v0.33.0 // indirect
	gonum.org/v1/gonum v0.15.1 // indirect
	google.golang.org/genproto v0.0.0-20240227224415-6ceb2ff114de // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250219182151-9fdb1cabc7b2 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250219182151-9fdb1cabc7b2 // indirect
	google.golang.org/grpc v1.70.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.32.2 // indirect
	k8s.io/apiserver v0.32.2 // indirect
	k8s.io/cloud-provider v0.32.2 // indirect
	k8s.io/component-helpers v0.32.2 // indirect
	k8s.io/controller-manager v0.32.2 // indirect
	k8s.io/csi-translation-lib v0.32.2 // indirect
	k8s.io/dynamic-resource-allocation v0.32.2 // indirect
	k8s.io/gengo/v2 v2.0.0-20240826214909-a7b603a56eb7 // indirect
	k8s.io/kms v0.32.2 // indirect
	k8s.io/kube-openapi v0.0.0-20250318190949-c8a335a9a2ff // indirect
	k8s.io/kube-scheduler v0.31.2 // indirect
	k8s.io/kubelet v0.32.2 // indirect
	k8s.io/mount-utils v0.32.2 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.31.2 // indirect
	sigs.k8s.io/controller-runtime v0.20.2 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.7.0 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

replace (
	k8s.io/api => k8s.io/api v0.31.2
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.31.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.31.2
	k8s.io/apiserver => k8s.io/apiserver v0.31.2
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.31.2
	k8s.io/client-go => k8s.io/client-go v0.31.2
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.31.2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.31.2
	k8s.io/code-generator => k8s.io/code-generator v0.31.2
	k8s.io/component-base => k8s.io/component-base v0.31.2
	k8s.io/component-helpers => k8s.io/component-helpers v0.31.2
	k8s.io/controller-manager => k8s.io/controller-manager v0.31.2
	k8s.io/cri-api => k8s.io/cri-api v0.31.2
	k8s.io/cri-client => k8s.io/cri-client v0.31.2
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.31.2
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.31.2
	k8s.io/endpointslice => k8s.io/endpointslice v0.31.2
	k8s.io/kms => k8s.io/kms v0.31.2
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.31.2
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.31.2
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.31.2
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.31.2
	k8s.io/kubectl => k8s.io/kubectl v0.31.2
	k8s.io/kubelet => k8s.io/kubelet v0.31.2
	k8s.io/kubernetes => k8s.io/kubernetes v1.31.2
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.31.2
	k8s.io/metrics => k8s.io/metrics v0.31.2
	k8s.io/mount-utils => k8s.io/mount-utils v0.31.2
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.31.2
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.31.2
)
