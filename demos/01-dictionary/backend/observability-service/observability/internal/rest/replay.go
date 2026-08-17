package rest

// JetStream replay — lifted from shipping-service's jetstreamReplayOnce
// (dictionary/internal/rest/replay.go), Phase 30e, with two deliberate
// simplifications:
//
//  1. No ship/container-specific subject filter. The original special-cased
//     the SHIPPING stream (filtering to ship.*/container.* to skip
//     pre-migration stale-subject messages) — shipping domain knowledge that
//     has no place in a domain-agnostic cross-account diagnostic tool, and
//     domain.StreamSubjects() isn't reachable here anyway (separate go.mod,
//     no domain package). Every stream now replays unfiltered. Cosmetic
//     effect only: the SHIPPING stream's replay panel may show a few extra
//     historical messages with old subject shapes, never a correctness
//     issue.
//  2. account and stream are both required query params. The original
//     defaulted ?stream= to "SHIPPING" and ?account= to the currently
//     active tenant (h.deps().Tenant) — a concept that only exists in
//     shipping-service's own per-session tenant switch. This service has no
//     "currently active tenant"; inventing a default would be arbitrary.
//
// DeleteConsumer's name always comes from consumer.CachedInfo().Name — the
// name the server itself returned when this handler created the consumer —
// never from request input. This is the discipline BR-AC32's design note
// (BUSINESS_RULES-ACCOUNTS.md) requires: CONSUMER.DELETE.*.* permits
// deleting any consumer on any stream in the account, so nothing in this
// file may ever pass a caller-supplied or otherwise-sourced name to it.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type jsEvent struct {
	Subject   string          `json:"subject"`
	Seq       uint64          `json:"seq"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload" swaggertype:"object"`
}

// jetstreamReplayOnce godoc
//
// @Summary      JetStream one-shot replay
// @Description  Returns every currently-retained raw JetStream message from one account's stream as a single JSON array, snapshotted at request time.
// @Tags         streams
// @Produce      json
// @Param        account  query     string  true  "NATS account (a known tenant name, or \"platform\")"
// @Param        stream   query     string  true  "Stream name"
// @Success      200  {array}   jsEvent
// @Failure      400  {object}  errorResponse  "Missing or unknown account/stream"
// @Failure      500  {object}  errorResponse
// @Router       /api/jetstream/replay [get]
func (h *Handlers) jetstreamReplayOnce(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account := r.URL.Query().Get("account")
	streamName := r.URL.Query().Get("stream")
	if account == "" || streamName == "" {
		writeError(w, http.StatusBadRequest, "account and stream are both required")
		return
	}

	js, ok := h.jsForAccount(ctx, account)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown account: "+account)
		return
	}

	consumer, err := js.OrderedConsumer(ctx, streamName, jetstream.OrderedConsumerConfig{DeliverPolicy: jetstream.DeliverAllPolicy})
	if err != nil {
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			writeError(w, http.StatusBadRequest, "unknown stream: "+streamName)
			return
		}
		h.deps.Log.Error("jetstream replay: create consumer", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Ephemeral only means "reaped after InactiveThreshold" (5m), not
	// "removed when the client stops pulling" — one replay request per
	// consumer slot would otherwise walk the account's JetStream
	// MaxConsumers limit up. consumer.CachedInfo().Name is the name the
	// server assigned at creation — never a request-derived value (see the
	// package doc comment above).
	defer func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = js.DeleteConsumer(dropCtx, streamName, consumer.CachedInfo().Name)
	}()

	events := []jsEvent{}
	if consumer.CachedInfo().NumPending == 0 {
		writeJSON(w, http.StatusOK, events)
		return
	}
	msgs, err := consumer.Messages()
	if err != nil {
		h.deps.Log.Error("jetstream replay: consume messages", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer msgs.Stop()

	for {
		msg, err := msgs.Next()
		if err != nil {
			break
		}
		meta, metaErr := msg.Metadata()
		if metaErr == nil {
			events = append(events, jsEvent{
				Subject:   msg.Subject(),
				Seq:       meta.Sequence.Stream,
				Timestamp: meta.Timestamp,
				Payload:   msg.Data(),
			})
		}
		if metaErr == nil && meta.NumPending == 0 {
			break
		}
	}

	writeJSON(w, http.StatusOK, events)
}
