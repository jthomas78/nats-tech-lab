// Package refdataclient is BR-TP14's consuming side of refdata-service's
// rpc.* dual-transport adapter — trimmed to the one call trading-partner-service
// needs (does a vehicle-type code exist?), agreeing only on the NATS wire
// shape with refdata-service, exactly as shipping-service's own
// internal/refdataconsumer does (this package has no dependency on
// refdata-service's or shipping-service's Go code — two independent
// modules, same convention).
//
// Per BR-D28 in BUSINESS_RULES-REFDATA.md, this is NATS-only: no REST
// fallback, no HTTP client, no base URL/hostname/port pointing at
// refdata-service. The connection passed to New must be the caller's
// tenant-scoped connection (internal/tenants) — refdata-service's
// account-export/import model (Phase 21) means the rpc.* call only reaches
// the right corpus when made over that tenant's own NATS account.
package refdataclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
)

// ErrRPCUnavailable is returned when every retry against rpc.* fails — there
// is no REST fallback to fall through to (BR-D28).
var ErrRPCUnavailable = errors.New("refdata rpc: unavailable after retries")

const (
	defaultRPCRequestTimeout = 3 * time.Second
	defaultRPCRetries        = 2
	defaultRPCBackoff        = 150 * time.Millisecond
)

// vehicleTypeKey is refdata-service's typeKey for the vehicle-type corpus
// (see refdata-service/cmd/seed-vehicle-types/main.go).
const vehicleTypeKey = "vehicle-type"

const requestorHeader = "Nats-Requestor"

// Client makes the one rpc.* call BR-TP14 needs. Holds a NATS connection and
// nothing else (BR-D28) — no HTTP client, no base URL.
type Client struct {
	nc          *nats.Conn
	requestorID string
}

func New(nc *nats.Conn) *Client {
	return &Client{nc: nc, requestorID: fmt.Sprintf("%s/%s", nc.Opts.Name, nuid.Next())}
}

// rpcItemGetRequest/rpcItemGetResponse mirror refdata-service's
// natsrpc.ItemGetRequest/ItemGetResponse wire shape (same contract
// shipping-service's refdataconsumer agrees on).
type rpcItemGetRequest struct {
	Context string `json:"context"`
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
	Locale  string `json:"locale"`
}

type rpcItemGetResponse struct {
	Item struct {
		Code   string `json:"code"`
		Status string `json:"status"`
	} `json:"item"`
}

type rpcErrorResponse struct {
	Error    string `json:"error"`
	NotFound bool   `json:"notFound"`
}

// Exists implements domain.VehicleTypeValidator — true if code resolves to
// an item in contextKey's vehicle-type corpus, false (no error) if
// refdata-service reports it as not found, and an error only for a genuine
// transport/unexpected failure.
func (c *Client) Exists(ctx context.Context, contextKey, code string) (bool, error) {
	reqBody, err := json.Marshal(rpcItemGetRequest{Context: contextKey, TypeKey: vehicleTypeKey, Code: code})
	if err != nil {
		return false, err
	}
	data, err := c.requestRPC(ctx, "refdata.item.get.v1", reqBody)
	if err != nil {
		return false, err
	}

	var errResp rpcErrorResponse
	if err := json.Unmarshal(data, &errResp); err == nil && errResp.Error != "" {
		if errResp.NotFound {
			return false, nil
		}
		return false, fmt.Errorf("refdata rpc: %s", errResp.Error)
	}

	var resp rpcItemGetResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return false, err
	}
	return resp.Item.Code == code, nil
}

func (c *Client) requestRPC(ctx context.Context, subject string, body []byte) ([]byte, error) {
	msg := &nats.Msg{
		Subject: subject,
		Data:    body,
		Header:  nats.Header{requestorHeader: []string{c.requestorID}},
	}
	var lastErr error
	for attempt := 0; attempt <= defaultRPCRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(defaultRPCBackoff * time.Duration(attempt)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		rctx, cancel := context.WithTimeout(ctx, defaultRPCRequestTimeout)
		reply, err := c.nc.RequestMsgWithContext(rctx, msg)
		cancel()
		if err == nil {
			return reply.Data, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w (subject %s): %v", ErrRPCUnavailable, subject, lastErr)
}
