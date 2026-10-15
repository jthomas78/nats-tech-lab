// Package servicelist is the live service list (Phase 54, decision 17,
// BR-072).
//
// Operators ask for it over NATS on one api._platform.* subject. The reply
// is the lab's own versioned snapshot: every micro service instance that
// answered PING, INFO and STATS within one bounded discovery window, in
// PLATFORM and in each tenant account through BR-AC31's monitor.{tenant}.srv.>
// import. It does not read trace export, so it works with the Collector and
// Tempo down.
//
// A reply means responding, not dependency-ready. Readiness comes only from
// shared/natsready, where a service mounts it; otherwise it is unknown. No
// reply means not observed in the window, not dead. Failed or partial
// discovery is marked in the snapshot, never shown as an empty success.
//
// Clients cannot name a discovery subject. The subjects are built here.
//
// STATUS: skeleton (54b-1). Specs are red until 54b-6.
package servicelist

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
)

// Subject is the one request subject (decision 17).
const Subject = "api._platform.observability.service.list.v1"

// SchemaVersion names the reply schema. A breaking change gets a new one.
const SchemaVersion = "lab.servicelist.v1"

// PlatformAccount labels services found on the bare $SRV.* subjects.
const PlatformAccount = "PLATFORM"

// Readiness values. Unknown means the service mounts no natsready
// responder, or it did not answer in time.
const (
	ReadinessReady    = "ready"
	ReadinessNotReady = "not-ready"
	ReadinessUnknown  = "unknown"
)

// Account status values.
const (
	AccountOK    = "ok"
	AccountError = "error"
)

// Defaults bound one discovery.
const (
	DefaultWindow       = 500 * time.Millisecond
	DefaultMaxInstances = 200
)

// Options bound one discovery. Zero values take the defaults.
type Options struct {
	// Window is how long discovery waits for PING, INFO and STATS replies.
	Window time.Duration
	// MaxInstances caps the instances in one reply. Over the cap the reply
	// is marked truncated.
	MaxInstances int
}

// TenantSource lists the tenant accounts to query. An error is reported in
// the snapshot; it never becomes an empty tenant list.
type TenantSource interface {
	Tenants(ctx context.Context) ([]string, error)
}

// Snapshot is one reply in the lab schema.
type Snapshot struct {
	Schema     string    `json:"schema"`
	ObservedAt time.Time `json:"observedAt"`
	WindowMs   int64     `json:"windowMs"`
	// Complete is false when any account failed, the tenant list failed, or
	// the reply was truncated.
	Complete  bool      `json:"complete"`
	Truncated bool      `json:"truncated"`
	Errors    []string  `json:"errors,omitempty"`
	Accounts  []Account `json:"accounts"`
	Services  []Service `json:"services"`
}

// Account is one queried account and how its discovery went.
type Account struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Service is one micro service name in one account.
type Service struct {
	Name      string     `json:"name"`
	Account   string     `json:"account"`
	Readiness string     `json:"readiness"`
	Instances []Instance `json:"instances"`
}

// Instance is one responding process. Instances are never merged.
type Instance struct {
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	Description string            `json:"description,omitempty"`
	Started     time.Time         `json:"started"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Endpoints   []Endpoint        `json:"endpoints"`
}

// Endpoint is one endpoint's identity and live statistics.
type Endpoint struct {
	Name                    string `json:"name"`
	Subject                 string `json:"subject"`
	QueueGroup              string `json:"queueGroup"`
	NumRequests             int    `json:"numRequests"`
	NumErrors               int    `json:"numErrors"`
	LastError               string `json:"lastError,omitempty"`
	AverageProcessingTimeMs int64  `json:"averageProcessingTimeMs"`
}

// Discover runs one bounded discovery on nc, the PLATFORM connection.
func Discover(ctx context.Context, nc *nats.Conn, tenants TenantSource, opts Options) Snapshot {
	return Snapshot{}
}

// Mount serves Subject on nc, the PLATFORM connection. Call stop on
// shutdown.
func Mount(nc *nats.Conn, tenants TenantSource, opts Options) (stop func() error, err error) {
	return func() error { return nil }, nil
}
