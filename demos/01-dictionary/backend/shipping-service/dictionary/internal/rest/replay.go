package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
)

// jsEvent is one raw JetStream message, as returned by jetstreamReplayOnce —
// the wire shape the old replayJetStream/watchJetStream SSE handlers used
// before Phase 23.
type jsEvent struct {
	Subject   string          `json:"subject"`
	Seq       uint64          `json:"seq"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload" swaggertype:"object"`
}

// jetstreamReplayOnce godoc
//
// @Summary      JetStream one-shot replay (Phase 23)
// @Description  Returns every currently-retained raw JetStream message from one account's stream as a single JSON array, snapshotted at request time. Replaces replayJetStream's SSE full-history half — the Admin UI now does one bootstrap fetch here, then subscribes to notify.{context}.shipping.raw.{entity}.{event} (eventhandler.publishRawNotify) for anything published afterward, instead of holding an SSE connection open. Because this snapshot is backend-mediated it works for ANY account listStreams reports, not just the browser's own — but the notify.* live tail does not: NATS enforces account isolation at the server, so a browser can only subscribe within the account its own connection authenticated as.
// @Tags         streams
// @Produce      json
// @Param        account query     string  false  "NATS account (a known tenant name, or \"platform\") — defaults to the currently active tenant"
// @Param        stream  query     string  false  "Stream name (default SHIPPING)"
// @Success      200  {array}   jsEvent
// @Failure      400  {object}  errorResponse  "Unknown account or stream"
// @Failure      500  {object}  errorResponse
// @Router       /api/jetstream/replay [get]
func (h *Handlers) jetstreamReplayOnce(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	streamName := r.URL.Query().Get("stream")
	if streamName == "" {
		streamName = domain.StreamName
	}
	// Stream names are only unique within an account (every tenant has its own
	// SHIPPING), so a bare ?stream= is ambiguous — ?account= resolves it,
	// exactly as the {account} path segment does for the KV entries endpoint.
	// Omitted means "the currently active tenant", preserving the behaviour
	// this endpoint had when it read Deps.JS directly (SwitchTenant sets
	// Deps.Tenant and Deps.JS from one and the same tenantResources bundle).
	account := r.URL.Query().Get("account")
	if account == "" {
		account = h.deps().Tenant
	}
	js, ok := h.jsForAccount(account)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown account: "+account)
		return
	}

	// Same two-aggregate filter as streamJetStream (watchJetStream/
	// replayJetStream) — a tenant's SHIPPING carries ship.* and container.*
	// together. Every other stream gets no filter: PLATFORM's REFDATA and
	// TRACES carry unrelated subject taxonomies that the ship/container
	// filters would exclude entirely, leaving a permanently empty panel.
	cfg := jetstream.OrderedConsumerConfig{DeliverPolicy: jetstream.DeliverAllPolicy}
	if account != platformAccount && streamName == domain.StreamName {
		cfg.FilterSubjects = domain.StreamSubjects()
	}

	consumer, err := js.OrderedConsumer(ctx, streamName, cfg)
	if err != nil {
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			writeError(w, http.StatusBadRequest, "unknown stream: "+streamName)
			return
		}
		h.deps().Log.Error("jetstream replay: create consumer", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Ephemeral only means "reaped after InactiveThreshold" (5m), not "removed
	// when the client stops pulling" — one replay request per consumer slot
	// would otherwise walk the account's JetStream MaxConsumers limit up.
	defer func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = js.DeleteConsumer(dropCtx, streamName, consumer.CachedInfo().Name)
	}()

	events := []jsEvent{}
	// See queries/shape_c.go's ReconstructFleet for why completion is
	// measured against the filtered consumer's own pending count, not the
	// raw stream's LastSeq: a stream can hold messages that no longer match
	// the filter (e.g. a pre-migration subject shape), and folding until
	// LastSeq would block forever waiting for a tail message the filtered
	// consumer never delivers.
	if consumer.CachedInfo().NumPending == 0 {
		writeJSON(w, http.StatusOK, events)
		return
	}
	msgs, err := consumer.Messages()
	if err != nil {
		h.deps().Log.Error("jetstream replay: consume messages", "err", err)
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

// rpcTraceReplayOnce (Phase 23, BR-D29) was retired in Phase 28g along with
// the RPCTRACE stream and its notify bridge (eventhandler's now-removed
// RegisterRPCTraceNotify — see that file's retirement note). It served
// obs.rpc.* replay for the Admin UI's old [messages] tab; nothing had
// published to obs.rpc.* since Phase 28a-28e replaced every adapter's
// publishObs call with a natstrace span, so this endpoint had been
// returning an empty array since then. The [messages] tab now bootstraps
// from GET /api/kv/buckets/platform/trace-request-reply/entries instead — the same
// generic KV endpoint TraceWaterfall.vue already uses, no dedicated
// replay endpoint needed.
