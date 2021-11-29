module github.com/yndd/nddc-metric-collector

go 1.16

require (
	github.com/gorilla/mux v1.8.0
	github.com/karimra/gnmic v0.20.4
	github.com/openconfig/gnmi v0.0.0-20210914185457-51254b657b7d
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/spf13/cobra v1.2.1
	github.com/yndd/ndd-core v0.1.3
	github.com/yndd/ndd-runtime v0.1.2
	github.com/yndd/ndd-yang v0.1.321
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	google.golang.org/grpc v1.40.0
	k8s.io/api v0.22.2
	k8s.io/apiextensions-apiserver v0.22.1
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	sigs.k8s.io/controller-runtime v0.9.3
)
