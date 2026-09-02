module github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service

go 1.27

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/jthomas78/nats-tech-lab/shared/browserrpc v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/mferegistry v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/mferegistry/announcer v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/mferegistry/client v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/natsconn v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/natsnotify v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/natstrace v0.0.0-00010101000000-000000000000
	github.com/nats-io/nats-server/v2 v2.14.5
	github.com/nats-io/nats.go v1.52.0
	github.com/nats-io/nkeys v0.4.16
	github.com/onsi/ginkgo/v2 v2.32.0
	github.com/onsi/gomega v1.42.1
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.7.2-default-no-op // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
)

// Workspace members (see go.work at the repo root); the replaces are the same
// defensive pin accounts-service carries, for the same reason — plain `go mod`
// subcommands don't reliably honor go.work's implicit local override.
replace github.com/jthomas78/nats-tech-lab/shared/browserrpc => ../../../../shared/browserrpc

replace github.com/jthomas78/nats-tech-lab/shared/mferegistry => ../../../../shared/mferegistry

replace github.com/jthomas78/nats-tech-lab/shared/mferegistry/announcer => ../../../../shared/mferegistry/announcer

replace github.com/jthomas78/nats-tech-lab/shared/mferegistry/client => ../../../../shared/mferegistry/client

replace github.com/jthomas78/nats-tech-lab/shared/natsconn => ../../../../shared/natsconn

replace github.com/jthomas78/nats-tech-lab/shared/natsnotify => ../../../../shared/natsnotify

replace github.com/jthomas78/nats-tech-lab/shared/natstrace => ../../../../shared/natstrace
