module github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service

go 1.26.5

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/jthomas78/nats-tech-lab/shared/browserrpc v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/natstenants v0.0.0-00010101000000-000000000000
	github.com/jthomas78/nats-tech-lab/shared/natstrace v0.0.0-00010101000000-000000000000
	github.com/nats-io/nats-server/v2 v2.14.5
	github.com/nats-io/nats.go v1.52.0
	github.com/nats-io/nuid v1.0.1
	github.com/oklog/ulid/v2 v2.1.2
	github.com/onsi/ginkgo/v2 v2.32.0
	github.com/onsi/gomega v1.42.1
	go.temporal.io/api v1.34.0
	go.temporal.io/sdk v1.27.0
)


require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.7.2-default-no-op // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/facebookgo/clock v0.0.0-20150410010913-600d898af40a // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/pborman/uuid v1.2.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/robfig/cron v1.2.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/exp v0.0.0-20231127185646-65229373498e // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	// Pinned deliberately, and NOT removable by `go mod tidy` without
	// breaking the build: go-grpc-middleware v1.4.0 (reached via the Temporal
	// SDK) requires the 2020 monolithic genproto, which still carries
	// googleapis/{api,rpc} — the same packages that now live in their own
	// modules. With both in the graph every import of them is ambiguous. This
	// explicit require lifts the monolith to a version where those packages
	// have been split out. Nothing imports it directly, so tidy drops it and
	// the ambiguity returns; re-add it if that happens.
	// Pinned deliberately, and NOT removable by `go mod tidy` without breaking
	// the build: go-grpc-middleware v1.4.0 (reached via the Temporal SDK)
	// requires the 2020 monolithic genproto, which still carries
	// googleapis/{api,rpc} — the same packages that now live in their own
	// modules below. With both in the graph, every import of them is
	// ambiguous and the build fails. This explicit require lifts the monolith
	// to a version where those packages have been split out. Nothing imports
	// it directly, so `go mod tidy` drops it and the ambiguity comes back;
	// re-add it if that happens.
	google.golang.org/genproto v0.0.0-20240521202816-d264139d666e // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240521202816-d264139d666e // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240521202816-d264139d666e // indirect
	google.golang.org/grpc v1.64.0 // indirect
	google.golang.org/protobuf v1.36.7 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Phase 35: shared/natstrace is a workspace member (see go.work at the repo
// root) — this replace is a defensive belt-and-suspenders pin, not the
// primary resolution mechanism, since plain `go mod` subcommands (tidy, in
// particular) don't reliably honor go.work's implicit local override once
// the replaced module has its own external dependencies.
replace github.com/jthomas78/nats-tech-lab/shared/natstrace => ../../../../shared/natstrace

replace github.com/jthomas78/nats-tech-lab/shared/natstenants => ../../../../shared/natstenants

replace github.com/jthomas78/nats-tech-lab/shared/browserrpc => ../../../../shared/browserrpc
