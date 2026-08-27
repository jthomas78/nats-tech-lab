module github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service

go 1.27

require (
	github.com/jthomas78/nats-tech-lab/shared/natsconn v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/natsnotify v0.0.0-00010101000000-000000000000
	github.com/nats-io/nats-server/v2 v2.14.5
	github.com/nats-io/nats.go v1.52.0
	github.com/onsi/gomega v1.42.1
	github.com/swaggo/http-swagger v1.3.4
	github.com/swaggo/swag v1.16.6
)

require (
	github.com/KyleBanks/depth v1.2.1 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.7.2-default-no-op // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/spec v0.20.6 // indirect
	github.com/go-openapi/swag v0.19.15 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/jthomas78/nats-tech-lab/shared/natstrace v0.0.0-00010101000000-000000000000 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/swaggo/files v0.0.0-20220610200504-28940afbdbfe // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
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
