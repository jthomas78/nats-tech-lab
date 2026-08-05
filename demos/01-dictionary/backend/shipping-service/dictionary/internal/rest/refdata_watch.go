package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// watchEvent is one SSE payload for watchRefdata below — the only remaining
// SSE stream in this package (Phase 23 moved the ship/container/KV/
// JetStream/RPC streams to REST bootstrap + notify.* subscribe; see
// replay.go, kv.go, and dictionary/internal/eventhandler/platform_notify.go).
// watchRefdata itself stays SSE deliberately: it backs
// shared/refdata/useRefdataLabels.js's UI-text/label refresh, used by every
// frontend in this repo (admin, seafreight-app, refdata), not just the four
// admin-specific panels Phase 23 replaced — out of that phase's scope.
type watchEvent struct {
	Shape    string          `json:"shape"`
	Key      string          `json:"key"`
	Op       string          `json:"op"` // PUT, DEL, PURGE
	Revision uint64          `json:"revision"`
	Value    json.RawMessage `json:"value,omitempty"`
}

// refdataChangeStreamName is the JetStream stream refdata-service publishes
// change-event pointers on (kvcache.ChangeStreamName in that service — this
// module has no Go dependency on refdata-service's code, so the literal is
// duplicated here, agreeing only on the published stream name/subject
// contract, same convention as refdataconsumer's rpc.* wire shapes).
const refdataChangeStreamName = "REFDATA"

// watchRefdata godoc
//
// @Summary      Refdata change-event stream (SSE, Phase 12.12)
// @Description  Server-Sent Events stream of refdata-service's evt.{tenant}.refdata.*.changed change-event pointers (the REFDATA JetStream stream) — drives live label refresh in the shipping UIs. This subscribes to refdata-service's published event contract rather than watching its KV cache directly (BR-D08: that cache is internal to refdata-service). No historical replay — a client refetches its label map once via REST on connect, so this stream only needs to signal "something changed" going forward. No fleet-context param — the refdata company context is derived from the active tenant (Phase 16f, refdataCompanyContext), not a path param.
// @Tags         streams
// @Produce      text/event-stream
// @Success      200  {string}  string  "SSE stream — data: {watchEvent JSON}"
// @Failure      500  {object}  errorResponse
// @Router       /api/refdata-watch [get]
func (h *Handlers) watchRefdata(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	// REFDATA is refdata-service's own stream, on the permanent PLATFORM
	// account — not the tenant-scoped h.deps().JS (Phase 13b: refdata-service
	// is unreachable from any tenant account, see Main-POC-Plan.md
	// Phase 13b, cost #3).
	if h.deps().PlatformJS == nil {
		writeError(w, http.StatusInternalServerError, "JetStream not configured")
		return
	}
	ctx := r.Context()

	var msgs jetstream.MessagesContext
	consumer, err := h.deps().PlatformJS.OrderedConsumer(ctx, refdataChangeStreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{"evt." + refdataCompanyContext(h.deps().Tenant) + ".refdata.>"},
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	})
	if err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
		h.deps().Log.Error("refdata change stream: create consumer", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err == nil {
		// REFDATA not existing yet (refdata-service hasn't published its
		// first change) is a legitimate race, not a hard error — degrade to
		// heartbeat-only below rather than failing the whole SSE connection.
		if msgs, err = consumer.Messages(); err != nil {
			h.deps().Log.Error("refdata change stream: consume messages", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		defer msgs.Stop()
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	msgCh := make(chan jetstream.Msg, 16)
	if msgs != nil {
		go func() {
			for {
				msg, err := msgs.Next()
				if err != nil {
					close(msgCh)
					return
				}
				select {
				case msgCh <- msg:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	send := func(msg jetstream.Msg) {
		var revision uint64
		if meta, err := msg.Metadata(); err == nil {
			revision = meta.Sequence.Stream
		}
		event := watchEvent{Shape: "REFDATA", Key: msg.Subject(), Op: "PUT", Revision: revision, Value: msg.Data()}
		data, err := json.Marshal(event)
		if err != nil {
			return
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			send(msg)
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}
