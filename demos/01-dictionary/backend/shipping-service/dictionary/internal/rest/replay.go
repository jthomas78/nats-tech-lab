package rest

import (
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
// @Description  Returns every currently-retained raw JetStream message from stream as a single JSON array, snapshotted at request time. Replaces replayJetStream's SSE full-history half — the Admin UI now does one bootstrap fetch here, then subscribes to notify.{context}.shipping.raw.{entity}.{event} (eventhandler.publishRawNotify) for anything published afterward, instead of holding an SSE connection open.
// @Tags         streams
// @Produce      json
// @Param        stream  query     string  false  "Stream name (default SHIPPING)"
// @Success      200  {array}   jsEvent
// @Failure      400  {object}  errorResponse  "Unknown stream"
// @Failure      500  {object}  errorResponse
// @Router       /api/jetstream/replay [get]
func (h *Handlers) jetstreamReplayOnce(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	streamName := r.URL.Query().Get("stream")
	if streamName == "" {
		streamName = domain.StreamName
	}

	// Same two-aggregate filter as streamJetStream (watchJetStream/
	// replayJetStream) — SHIPPING carries ship.* and container.* together;
	// any other stream gets no filter.
	cfg := jetstream.OrderedConsumerConfig{DeliverPolicy: jetstream.DeliverAllPolicy}
	if streamName == domain.StreamName {
		cfg.FilterSubjects = domain.StreamSubjects()
	}

	consumer, err := h.deps().JS.OrderedConsumer(ctx, streamName, cfg)
	if err != nil {
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			writeError(w, http.StatusBadRequest, "unknown stream: "+streamName)
			return
		}
		h.deps().Log.Error("jetstream replay: create consumer", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

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

// rpcTraceReplayOnce godoc
//
// @Summary      RPCTRACE one-shot replay (Phase 23, BR-D29)
// @Description  Returns every currently-retained obs.rpc.* trace entry from the RPCTRACE stream (PLATFORM account) as a single JSON array, snapshotted at request time. Replaces the replay half of watchRPCObs's combined SSE feed — the Admin UI now does one bootstrap fetch here, then subscribes to notify._platform.rpctrace.entry (eventhandler.RegisterRPCTraceNotify) for anything published afterward. The obs.api.> (tenant-side, live-only) half of the old feed isn't part of this endpoint at all — the browser subscribes to obs.api.> directly on its own tenant connection (auth/token.go's MintBrowserToken already grants it).
// @Tags         streams
// @Produce      json
// @Success      200  {array}   object
// @Failure      500  {object}  errorResponse
// @Router       /api/rpctrace/replay [get]
func (h *Handlers) rpcTraceReplayOnce(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	entries := []json.RawMessage{}

	platformJS := h.deps().PlatformJS
	if platformJS == nil {
		writeJSON(w, http.StatusOK, entries)
		return
	}

	consumer, err := platformJS.OrderedConsumer(ctx, "RPCTRACE", jetstream.OrderedConsumerConfig{
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			// RPCTRACE not existing yet (no obs.rpc.* traffic since boot) is
			// a legitimate race, not an error — same tolerance
			// watchRPCObs/watchRefdata give it.
			writeJSON(w, http.StatusOK, entries)
			return
		}
		h.deps().Log.Warn("rpctrace replay: create consumer", "err", err)
		writeJSON(w, http.StatusOK, entries)
		return
	}
	if consumer.CachedInfo().NumPending == 0 {
		writeJSON(w, http.StatusOK, entries)
		return
	}
	msgs, err := consumer.Messages()
	if err != nil {
		h.deps().Log.Warn("rpctrace replay: consume messages", "err", err)
		writeJSON(w, http.StatusOK, entries)
		return
	}
	defer msgs.Stop()

	for {
		msg, err := msgs.Next()
		if err != nil {
			break
		}
		entries = append(entries, json.RawMessage(msg.Data()))
		meta, metaErr := msg.Metadata()
		if metaErr == nil && meta.NumPending == 0 {
			break
		}
	}

	writeJSON(w, http.StatusOK, entries)
}
