module istio.io/istio

go 1.16

replace github.com/spf13/viper => github.com/istio/viper v1.3.3-0.20190515210538-2789fed3109c

// Old version had no license
replace github.com/chzyer/logex => github.com/chzyer/logex v1.1.11-0.20170329064859-445be9e134b2

// Avoid pulling in incompatible libraries
replace github.com/docker/distribution => github.com/docker/distribution v0.0.0-20191216044856-a8371794149d

replace github.com/docker/docker => github.com/moby/moby v17.12.0-ce-rc1.0.20200618181300-9dc6525e6118+incompatible

// Client-go does not handle different versions of mergo due to some breaking changes - use the matching version
replace github.com/imdario/mergo => github.com/imdario/mergo v0.3.5

require (
	cloud.google.com/go v0.97.0
	cloud.google.com/go/security v0.1.0
	contrib.go.opencensus.io/exporter/prometheus v0.4.0
	github.com/AdaLogics/go-fuzz-headers v0.0.0-20210818151418-6e8bb17796da
	github.com/Masterminds/sprig/v3 v3.2.3
	github.com/aws/aws-sdk-go v1.43.16
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/census-instrumentation/opencensus-proto v0.3.0
	github.com/cheggaaa/pb/v3 v3.0.8
	github.com/cncf/udpa/go v0.0.0-20210930031921-04548b0d99d4
	github.com/cncf/xds/go v0.0.0-20211011173535-cb28da3451f1
	github.com/containernetworking/cni v1.1.1
	github.com/containernetworking/plugins v1.1.1
	github.com/coreos/go-oidc v2.2.1+incompatible
	github.com/davecgh/go-spew v1.1.1
	github.com/distribution/distribution/v3 v3.0.0-20221208165359-362910506bc2
	github.com/envoyproxy/go-control-plane v0.10.2-0.20220325020618-49ff273808a1
	github.com/evanphx/json-patch v5.6.0+incompatible
	github.com/evanphx/json-patch/v5 v5.5.0
	github.com/fatih/color v1.12.0
	github.com/fsnotify/fsnotify v1.6.0
	github.com/ghodss/yaml v1.0.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.2
	github.com/google/go-cmp v0.5.9
	github.com/google/go-containerregistry v0.6.0
	github.com/google/gofuzz v1.2.0
	github.com/google/uuid v1.3.0
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.4.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/hashicorp/go-version v1.3.0
	github.com/hashicorp/golang-lru v0.5.4
	github.com/kr/pretty v0.3.0
	github.com/kylelemons/godebug v1.1.0
	github.com/lestrrat-go/jwx v1.2.0
	github.com/lucas-clemente/quic-go v0.23.0
	github.com/mattn/go-isatty v0.0.13
	github.com/miekg/dns v1.1.43
	github.com/mitchellh/copystructure v1.2.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/onsi/gomega v1.23.0
	github.com/openshift/api v0.0.0-20200713203337-b2494ecb17dd
	github.com/pkg/errors v0.9.1
	github.com/pmezard/go-difflib v1.0.0
	github.com/prometheus/client_golang v1.14.0
	github.com/prometheus/client_model v0.3.0
	github.com/prometheus/common v0.37.0
	github.com/prometheus/prometheus v2.5.0+incompatible
	github.com/ryanuber/go-glob v1.0.0
	github.com/spf13/cobra v1.6.1
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.8.1
	github.com/stretchr/testify v1.8.1
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/yl2chen/cidranger v1.0.2
	go.opencensus.io v0.23.0
	go.uber.org/atomic v1.9.0
	go.uber.org/multierr v1.7.0
	golang.org/x/net v0.5.0
	golang.org/x/oauth2 v0.0.0-20220223155221-ee480838109b
	golang.org/x/sync v0.1.0
	golang.org/x/sys v0.4.0
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8
	gomodules.xyz/jsonpatch/v2 v2.2.0
	gomodules.xyz/jsonpatch/v3 v3.0.1
	google.golang.org/api v0.57.0
	google.golang.org/genproto v0.0.0-20220502173005-c8bf987b8c21
	google.golang.org/grpc v1.49.0
	google.golang.org/protobuf v1.28.1
	gopkg.in/square/go-jose.v2 v2.6.0
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
	helm.sh/helm/v3 v3.11.1
	istio.io/api v0.0.0-20210908191335-4c3fa7872b75
	istio.io/client-go v0.0.0-20210908192230-c714593a9eb4
	istio.io/gogo-genproto v0.0.0-20210806192525-32ebb2f9006c
	istio.io/pkg v0.0.0-20210806192335-d58436bead18
	k8s.io/api v0.26.0
	k8s.io/apiextensions-apiserver v0.26.0
	k8s.io/apimachinery v0.26.0
	k8s.io/cli-runtime v0.26.0
	k8s.io/client-go v0.26.0
	k8s.io/kube-openapi v0.0.0-20221012153701-172d655c2280
	k8s.io/kubectl v0.26.0
	k8s.io/utils v0.0.0-20221107191617-1a15be271d1d
	sigs.k8s.io/controller-runtime v0.9.6
	sigs.k8s.io/gateway-api v0.4.0-rc1
	sigs.k8s.io/mcs-api v0.1.0
	sigs.k8s.io/yaml v1.3.0
)
