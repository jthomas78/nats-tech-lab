package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// watchEvent is one SSE payload: a KV change in one of the watched buckets.
// Shape identifies the source bucket family: "A" / "B" (ship projections),
// "CONTAINER" (container projection), "META" (meta.* lookup sets).
type watchEvent struct {
	Shape    string          `json:"shape"`
	Key      string          `json:"key"`
	Op       string          `json:"op"` // PUT, DEL, PURGE
	Revision uint64          `json:"revision"`
	Value    json.RawMessage `json:"value,omitempty"`
}

func opString(op jetstream.KeyValueOp) string {
	switch op {
	case jetstream.KeyValueDelete:
		return "DEL"
	case jetstream.KeyValuePurge:
		return "PURGE"
	default:
		return "PUT"
	}
}

// watch godoc
//
// @Summary      Ship KV watch stream (SSE)
// @Description  Server-Sent Events stream of NATS KV changes for both the Shape A and Shape B ship buckets in the given context. Replays current bucket state first (snapshot), then delivers live updates. Each event is a JSON-encoded watchEvent object.
// @Tags         streams
// @Produce      text/event-stream
// @Param        context  path      string  true  "Fleet context (e.g. acme, acme-atlantic-fleet)"
// @Success      200      {string}  string  "SSE stream — data: {watchEvent JSON}"
// @Failure      500      {object}  errorResponse
// @Router       /api/watch/{context} [get]
// watch streams ship KV changes for a context over SSE. It watches both the
// Shape A read-model bucket and the Shape B cache bucket, replaying current
// state first, then pushing live updates. This is the server half of the
// KV watch → SSE → Pinia store pipeline.
func (h *Handlers) watch(w http.ResponseWriter, r *http.Request) {
	h.watchBuckets(w, r, r.PathValue("context"), []watchSource{
		{shape: "A", store: h.deps().KVA},
		{shape: "B", store: h.deps().KVB},
	})
}

// watchTerminal godoc
//
// @Summary      Terminal KV watch stream (SSE)
// @Description  Server-Sent Events stream of NATS KV changes for the container projection bucket and the meta.* lookup bucket in the given context. Replays current state first, then delivers live updates. Shape is "CONTAINER" or "META".
// @Tags         streams
// @Produce      text/event-stream
// @Param        context  path      string  true  "Fleet context (e.g. acme)"
// @Success      200      {string}  string  "SSE stream — data: {watchEvent JSON}"
// @Failure      500      {object}  errorResponse
// @Router       /api/watch-terminal/{context} [get]
// watchTerminal is the terminal-side twin of watch: container states and
// meta.* lookup sets, consumed by the Port Management frontend.
func (h *Handlers) watchTerminal(w http.ResponseWriter, r *http.Request) {
	h.watchBuckets(w, r, r.PathValue("context"), []watchSource{
		{shape: "CONTAINER", store: h.deps().KVCont},
		{shape: "META", store: h.deps().KVMeta},
	})
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
	// REFDATA is refdata-service's own stream, on the permanent DEFAULT
	// account — not the tenant-scoped h.deps().JS (Phase 13b: refdata-service
	// is unreachable from any tenant account, see Main-POC-Plan.md
	// Phase 13b, cost #3).
	if h.deps().DefaultJS == nil {
		writeError(w, http.StatusInternalServerError, "JetStream not configured")
		return
	}
	ctx := r.Context()

	var msgs jetstream.MessagesContext
	consumer, err := h.deps().DefaultJS.OrderedConsumer(ctx, refdataChangeStreamName, jetstream.OrderedConsumerConfig{
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

type watchSource struct {
	shape string
	store *kvstore.Store
}

func (h *Handlers) watchBuckets(w http.ResponseWriter, r *http.Request, kvContext string, sources []watchSource) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ctx := r.Context()

	type update struct {
		shape string
		entry jetstream.KeyValueEntry
	}
	updates := make(chan update, 16)

	for _, src := range sources {
		watcher, err := src.store.Watch(ctx, kvContext)
		if err != nil {
			h.deps().Log.Error("kv watch", "shape", src.shape, "context", kvContext, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		defer func() { _ = watcher.Stop() }()

		go func(shape string, watcher jetstream.KeyWatcher) {
			for {
				select {
				case <-ctx.Done():
					return
				case entry, ok := <-watcher.Updates():
					if !ok {
						return
					}
					select {
					case updates <- update{shape: shape, entry: entry}:
					case <-ctx.Done():
						return
					}
				}
			}
		}(src.shape, watcher)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	send := func(shape string, entry jetstream.KeyValueEntry) {
		if entry == nil {
			return // end-of-replay marker
		}
		event := watchEvent{
			Shape:    shape,
			Key:      entry.Key(),
			Op:       opString(entry.Operation()),
			Revision: entry.Revision(),
		}
		if entry.Operation() == jetstream.KeyValuePut {
			event.Value = entry.Value()
		}
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
		case u := <-updates:
			send(u.shape, u.entry)
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

// watchRPCObs godoc
//
// @Summary      obs.rpc.* + obs.api.* request/reply traffic stream (SSE, Phase 12.10)
// @Description  Server-Sent Events stream of the best-effort request/reply observability side-channels: refdata-service's obs.rpc.* (backend-to-backend, DEFAULT account) and shipping-service's obs.api.* (browser-to-service, the ACTIVE tenant's account only). Replays whatever the RPCTRACE stream currently retains for obs.rpc.* (up to the last 10 minutes, BR-D29) first, then continues live via core NATS subscribe; obs.api.* is live-only — RPCTRACE lives on the DEFAULT account and does not capture tenant-account traffic. Each event carries direction ("request"|"reply"), a correlationId pairing a request with its reply, the real rpc.*/api.* subject, and the payload. See ARCHITECTURE-COMMUNICATIONS.md §6.
// @Tags         streams
// @Produce      text/event-stream
// @Success      200  {string}  string  "SSE stream — data: {obs.rpc.*/obs.api.* JSON}"
// @Failure      500  {object}  errorResponse
// @Router       /api/rpc-watch [get]
// watchRPCObs replays the RPCTRACE stream's current backlog (up to its 10m
// MaxAge, BR-D29 — best-effort catch-up for a tab opened after a call
// completes), then subscribes to obs.rpc.> on the shared DEFAULT-account
// connection AND obs.api.> on the active tenant's connection (browserrpc's
// adapter publishes its observability events inside the tenant account —
// see the Deps doc comment in browserrpc/adapter.go), re-emitting each live
// message as an SSE event. The obs.api.> half is live-only (no RPCTRACE
// retention exists inside tenant accounts) and is pinned to whichever tenant
// was active when the SSE connection opened — the Admin UI reconnects on
// tenant switch. The live subscribes are established before the replay is
// drained so nothing published during the replay window is missed; a message
// published in the narrow gap before a subscribe took effect can in
// principle appear twice, which is acceptable for a best-effort debug trace
// feed. h.deps().DefaultJS is optional — when nil (or RPCTRACE doesn't exist
// yet), this degrades to live-only behavior. h.deps().TenantNC is likewise
// optional — when nil, the feed simply carries no obs.api.* traffic.
func (h *Handlers) watchRPCObs(w http.ResponseWriter, r *http.Request) {
	if h.deps().NC == nil {
		writeError(w, http.StatusInternalServerError, "NATS connection not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ctx := r.Context()

	msgCh := make(chan *nats.Msg, 64)
	sub, err := h.deps().NC.ChanSubscribe("obs.rpc.>", msgCh)
	if err != nil {
		h.deps().Log.Error("obs.rpc subscribe", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = sub.Unsubscribe() }()

	// obs.api.* events publish on the active tenant's own account (see
	// browserrpc.ObsSubjectWildcard) — subscribe there, feeding the same
	// channel so both families interleave into one SSE feed.
	if tenantNC := h.deps().TenantNC; tenantNC != nil {
		apiSub, err := tenantNC.ChanSubscribe("obs.api.>", msgCh)
		if err != nil {
			h.deps().Log.Error("obs.api subscribe", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		defer func() { _ = apiSub.Unsubscribe() }()
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	write := func(data []byte) {
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}

	// RPCTRACE is refdata-service's own stream, on the permanent DEFAULT
	// account, like REFDATA above — not the tenant-scoped h.deps().JS.
	if h.deps().DefaultJS != nil {
		if consumer, err := h.deps().DefaultJS.OrderedConsumer(ctx, "RPCTRACE", jetstream.OrderedConsumerConfig{
			DeliverPolicy: jetstream.DeliverAllPolicy,
		}); err != nil {
			if !errors.Is(err, jetstream.ErrStreamNotFound) {
				h.deps().Log.Warn("rpctrace replay: create consumer", "err", err)
			}
		} else if batch, err := consumer.FetchNoWait(1000); err != nil {
			h.deps().Log.Warn("rpctrace replay: fetch", "err", err)
		} else {
			for msg := range batch.Messages() {
				write(msg.Data())
			}
			if err := batch.Error(); err != nil {
				h.deps().Log.Warn("rpctrace replay: batch", "err", err)
			}
		}
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			write(msg.Data)
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

// jsEvent is one SSE payload from the raw JetStream stream.
type jsEvent struct {
	Subject   string          `json:"subject"`
	Seq       uint64          `json:"seq"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// replayJetStream godoc
//
// @Summary      JetStream full replay + live stream (SSE)
// @Description  Server-Sent Events stream of raw emea.events.acme.> messages from SHIPPING using DeliverAll policy — replays from seq=1, then continues live. Each event is a JSON-encoded jsEvent object.
// @Tags         streams
// @Produce      text/event-stream
// @Param        stream  query     string  false  "Stream name (default SHIPPING)"
// @Success      200  {string}  string  "SSE stream — data: {jsEvent JSON}"
// @Failure      400  {object}  errorResponse  "Unknown stream"
// @Failure      500  {object}  errorResponse
// @Router       /api/jetstream/stream [get]
// replayJetStream streams all JetStream messages from the beginning of the
// SHIPPING stream, then continues delivering new ones. Uses DeliverAll policy.
func (h *Handlers) replayJetStream(w http.ResponseWriter, r *http.Request) {
	h.streamJetStream(w, r, jetstream.DeliverAllPolicy)
}

// watchJetStream godoc
//
// @Summary      JetStream live watch (SSE)
// @Description  Server-Sent Events stream of raw emea.events.acme.> messages from SHIPPING using DeliverNew policy — only messages published after connection. Each event is a JSON-encoded jsEvent object.
// @Tags         streams
// @Produce      text/event-stream
// @Param        stream  query     string  false  "Stream name (default SHIPPING)"
// @Success      200  {string}  string  "SSE stream — data: {jsEvent JSON}"
// @Failure      400  {object}  errorResponse  "Unknown stream"
// @Failure      500  {object}  errorResponse
// @Router       /api/jetstream/watch [get]
// watchJetStream streams raw JetStream messages from the SHIPPING stream over
// SSE. It uses an ephemeral ordered consumer with DeliverNew so only messages
// published after the connection is established are delivered — no replay.
func (h *Handlers) watchJetStream(w http.ResponseWriter, r *http.Request) {
	h.streamJetStream(w, r, jetstream.DeliverNewPolicy)
}

func (h *Handlers) streamJetStream(w http.ResponseWriter, r *http.Request, policy jetstream.DeliverPolicy) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ctx := r.Context()

	streamName := r.URL.Query().Get("stream")
	if streamName == "" {
		streamName = domain.StreamName
	}

	// SHIPPING carries two aggregates on one stream (ship.* and container.*);
	// FilterSubjects narrows the ordered consumer to just its own subjects.
	// Any other registered stream (e.g. REFDATA) gets no filter — deliver
	// everything it carries, since it isn't the fixed two-aggregate shape.
	cfg := jetstream.OrderedConsumerConfig{DeliverPolicy: policy}
	if streamName == domain.StreamName {
		cfg.FilterSubjects = domain.StreamSubjects()
	}

	consumer, err := h.deps().JS.OrderedConsumer(ctx, streamName, cfg)
	if err != nil {
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			writeError(w, http.StatusBadRequest, "unknown stream: "+streamName)
			return
		}
		h.deps().Log.Error("create ordered consumer", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	msgs, err := consumer.Messages()
	if err != nil {
		h.deps().Log.Error("consume jetstream messages", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer msgs.Stop()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	msgCh := make(chan jetstream.Msg, 16)
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

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			meta, err := msg.Metadata()
			if err != nil {
				continue
			}
			event := jsEvent{
				Subject:   msg.Subject(),
				Seq:       meta.Sequence.Stream,
				Timestamp: meta.Timestamp,
				Payload:   json.RawMessage(msg.Data()),
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}
