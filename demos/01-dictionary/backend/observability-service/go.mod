module github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service

go 1.27

require (
	github.com/jthomas78/nats-tech-lab/shared/natsconn v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/natsnotify v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/natsready v0.0.0-00010101000000-000000000000
	github.com/nats-io/nats-server/v2 v2.14.5
	github.com/nats-io/nats.go v1.52.0
	github.com/onsi/ginkgo/v2 v2.32.0
	github.com/onsi/gomega v1.42.1
	github.com/swaggo/http-swagger v1.3.4
	github.com/swaggo/swag v1.16.6
)

require (
	github.com/KyleBanks/depth v1.2.1 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.7.2-default-no-op // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v1.0.0 // indirect
	github.com/go-openapi/jsonreference v1.0.0 // indirect
	github.com/go-openapi/spec v0.22.9 // indirect
	github.com/go-openapi/swag/conv v0.28.0 // indirect
	github.com/go-openapi/swag/jsonutils v0.28.0 // indirect
	github.com/go-openapi/swag/loading v0.28.0 // indirect
	github.com/go-openapi/swag/pools v0.28.0 // indirect
	github.com/go-openapi/swag/stringutils v0.28.0 // indirect
	github.com/go-openapi/swag/typeutils v0.28.0 // indirect
	github.com/go-openapi/swag/yamlutils v0.28.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/jthomas78/nats-tech-lab/shared/mferegistry v0.0.0-00010101000000-000000000000 // indirect
	github.com/jthomas78/nats-tech-lab/shared/natstrace v0.0.0-00010101000000-000000000000 // indirect
	github.com/jthomas78/nats-tech-lab/shared/otelnats v0.0.0-00010101000000-000000000000 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/swaggo/files v0.0.0-20220610200504-28940afbdbfe // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.47.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

// Phase 43d: shared/natsnotify is a workspace member (see go.work at the
// repo root); the replace keeps a non-workspace `go build` in this directory
// resolving it too.
replace github.com/jthomas78/nats-tech-lab/shared/natsnotify => ../../../../shared/natsnotify

// natstrace is a transitive dependency of natsnotify. A replace directive in
// a dependency's go.mod is ignored, so this module needs its own.
replace github.com/jthomas78/nats-tech-lab/shared/natstrace => ../../../../shared/natstrace

replace github.com/jthomas78/nats-tech-lab/shared/natstest => ../../../../shared/natstest

replace github.com/jthomas78/nats-tech-lab/shared/natsconn => ../../../../shared/natsconn

replace github.com/jthomas78/nats-tech-lab/shared/otelnats => ../../../../shared/otelnats

replace github.com/jthomas78/nats-tech-lab/shared/natsready => ../../../../shared/natsready

replace github.com/jthomas78/nats-tech-lab/shared/mferegistry => ../../../../shared/mferegistry
