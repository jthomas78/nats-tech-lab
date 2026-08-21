// Package refdataclient is BR-TP14's consuming side of refdata-service's
// rpc.* dual-transport adapter — trimmed to the one call organizations-service
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
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
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

// Corpora backing BR-TP47's operating-area validation. Both are
// `standards`-category types seeded in the platform context (BR-D46), which
// is why platformContext below is a fixed literal rather than a parameter.
const (
	regionTypeKey  = "region"
	countryTypeKey = "country"

	// regionCountryRelation is BR-D47's relation name linking a region to
	// its country.
	regionCountryRelation = "country"

	// platformContext is where standards corpora live. refdata's item.get is
	// an exact context match with no ancestor walk (inheritance is resolved
	// at its corpus/version layer), so a region seeded in _platform is NOT
	// reachable by asking for it in a business-unit context — this must be
	// the literal, not the caller's context.
	platformContext = "_platform"
)

const requestorHeader = "Nats-Requestor"

// Client makes the one rpc.* call BR-TP14 needs. Holds a NATS connection and
// nothing else (BR-D28) — no HTTP client, no base URL.
type Client struct {
	nc          *nats.Conn
	requestorID string
	tracer      *natstrace.Tracer
}

func New(nc *nats.Conn) *Client {
	return &Client{
		nc:          nc,
		requestorID: fmt.Sprintf("%s/%s", nc.Opts.Name, nuid.Next()),
		tracer:      natstrace.New(nc),
	}
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
	// References carries the item's typed relations (BR-D05), added to the
	// item.get contract in Phase 38d-ii. BR-TP48 reads a region's `country`
	// relation from here — the corpus's own statement of the hierarchy
	// (BR-D47) rather than anything inferred from the code's shape.
	References []struct {
		Relation  string `json:"relation"`
		ToTypeKey string `json:"toTypeKey"`
		ToCode    string `json:"toCode"`
	} `json:"references"`
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
	resp, found, err := c.getItem(ctx, contextKey, vehicleTypeKey, code)
	if err != nil || !found {
		return false, err
	}
	return resp.Item.Code == code, nil
}

// getItem is the shared one-call item lookup behind Exists (BR-TP14) and
// ResolveArea (BR-TP47). found=false with a nil error means refdata reported
// the code as not found; an error means the call itself failed.
func (c *Client) getItem(ctx context.Context, contextKey, typeKey, code string) (rpcItemGetResponse, bool, error) {
	var zero rpcItemGetResponse
	reqBody, err := json.Marshal(rpcItemGetRequest{Context: contextKey, TypeKey: typeKey, Code: code})
	if err != nil {
		return zero, false, err
	}
	// "refdata.item.get.v1" is this tenant account's *local* alias, not the
	// real wire subject — accounts-service's provisioner.go mints every
	// tenant account with a service import remapping this local subject to
	// "rpc.{tenant}.refdata.item.get.v1" entirely inside the NATS server
	// (jwt.RenamingSubject), so the account's own identity lands at that
	// token, never a caller-supplied value. Publishing the real subject
	// directly here would neither route (it doesn't match the account's
	// LocalSubject) nor be safe (contextKey is a business-unit context, not
	// the tenant, and must never be the value that lands there).
	data, err := c.requestRPC(ctx, "refdata.item.get.v1", contextKey, "item", "get", reqBody)
	if err != nil {
		return zero, false, err
	}

	var errResp rpcErrorResponse
	if err := json.Unmarshal(data, &errResp); err == nil && errResp.Error != "" {
		if errResp.NotFound {
			return zero, false, nil
		}
		return zero, false, fmt.Errorf("refdata rpc: %s", errResp.Error)
	}

	var resp rpcItemGetResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return zero, false, err
	}
	if resp.Item.Code != code {
		return zero, false, nil
	}
	return resp, true, nil
}

// ResolveArea implements domain.OperatingAreaResolver (BR-TP47).
//
// Regions and countries are looked up in platformContext, never the caller's
// business-unit context — see that constant's comment. For a REGION the
// parent country comes from BR-D47's `country` relation on the item; a
// region whose relation is missing resolves as not-found rather than as a
// country-less area, because BR-TP48 cannot evaluate overlap without it and
// domain.AddOperatingArea would reject it anyway.
func (c *Client) ResolveArea(ctx context.Context, level domain.AreaLevel, code string) (string, bool, error) {
	switch level {
	case domain.AreaLevelCountry:
		_, found, err := c.getItem(ctx, platformContext, countryTypeKey, code)
		if err != nil || !found {
			return "", false, err
		}
		// A country-level area is its own country (BR-TP47).
		return code, true, nil

	case domain.AreaLevelRegion:
		resp, found, err := c.getItem(ctx, platformContext, regionTypeKey, code)
		if err != nil || !found {
			return "", false, err
		}
		for _, ref := range resp.References {
			if ref.Relation == regionCountryRelation && ref.ToTypeKey == countryTypeKey {
				return ref.ToCode, true, nil
			}
		}
		return "", false, nil

	default:
		return "", false, nil
	}
}

// requestRPC makes one logical rpc.* call, retrying up to defaultRPCRetries
// times. BR-037: exactly one natstrace span covers the whole call — minted
// before the retry loop starts, not one per attempt, so a 3-attempt failure
// is one span with rpc.retry_count=2, not three parentless siblings. The
// span continues whatever parent natstrace.ContextWithSpan attached to ctx
// (e.g. the browserrpc handler's own inbound span), or mints a root span if
// ctx carries none. contextValue/entity/action label the span explicitly
// (see StartOutbound's doc comment on why subject can't be parsed for this)
// — service is always "refdata", the only thing this package ever calls.
func (c *Client) requestRPC(ctx context.Context, subject, contextValue, entity, action string, body []byte) ([]byte, error) {
	sp := c.tracer.StartOutbound(natstrace.SpanFromContext(ctx), subject, body, contextValue, "refdata", entity, action)
	msg := &nats.Msg{
		Subject: subject,
		Data:    body,
		Header: nats.Header{
			requestorHeader:             []string{c.requestorID},
			natstrace.TraceparentHeader: []string{sp.Traceparent()},
		},
	}
	// See refdataconsumer's identical call: an outbound span predates its own
	// headers, so it has to be handed them rather than capturing them.
	sp.SetRequestHeaders(map[string][]string(msg.Header))
	var lastErr error
	attempt := 0
	for ; attempt <= defaultRPCRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(defaultRPCBackoff * time.Duration(attempt)):
			case <-ctx.Done():
				sp.SetAttribute("rpc.retry_count", strconv.Itoa(attempt))
				sp.Fail(ctx.Err(), nil, nil)
				return nil, ctx.Err()
			}
		}
		rctx, cancel := context.WithTimeout(ctx, defaultRPCRequestTimeout)
		reply, err := c.nc.RequestMsgWithContext(rctx, msg)
		cancel()
		if err == nil {
			sp.SetAttribute("rpc.retry_count", strconv.Itoa(attempt))
			sp.End(reply.Data, map[string][]string(reply.Header))
			return reply.Data, nil
		}
		lastErr = err
	}
	sp.SetAttribute("rpc.retry_count", strconv.Itoa(attempt-1))
	finalErr := fmt.Errorf("%w (subject %s): %v", ErrRPCUnavailable, subject, lastErr)
	sp.Fail(finalErr, nil, nil)
	return nil, finalErr
}
