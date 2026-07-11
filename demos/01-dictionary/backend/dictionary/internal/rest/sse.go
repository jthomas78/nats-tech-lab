package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
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
// @Param        context  path      string  true  "Fleet context (e.g. global, atlantic-fleet)"
// @Success      200      {string}  string  "SSE stream — data: {watchEvent JSON}"
// @Failure      500      {object}  errorResponse
// @Router       /api/watch/{context} [get]
// watch streams ship KV changes for a context over SSE. It watches both the
// Shape A read-model bucket and the Shape B cache bucket, replaying current
// state first, then pushing live updates. This is the server half of the
// KV watch → SSE → Pinia store pipeline.
func (h *Handlers) watch(w http.ResponseWriter, r *http.Request) {
	h.watchBuckets(w, r, []watchSource{
		{shape: "A", store: h.deps.KVA},
		{shape: "B", store: h.deps.KVB},
	})
}

// watchTerminal godoc
//
// @Summary      Terminal KV watch stream (SSE)
// @Description  Server-Sent Events stream of NATS KV changes for the container projection bucket and the meta.* lookup bucket in the given context. Replays current state first, then delivers live updates. Shape is "CONTAINER" or "META".
// @Tags         streams
// @Produce      text/event-stream
// @Param        context  path      string  true  "Fleet context (e.g. global)"
// @Success      200      {string}  string  "SSE stream — data: {watchEvent JSON}"
// @Failure      500      {object}  errorResponse
// @Router       /api/watch-terminal/{context} [get]
// watchTerminal is the terminal-side twin of watch: container states and
// meta.* lookup sets, consumed by the Port Management frontend.
func (h *Handlers) watchTerminal(w http.ResponseWriter, r *http.Request) {
	h.watchBuckets(w, r, []watchSource{
		{shape: "CONTAINER", store: h.deps.KVCont},
		{shape: "META", store: h.deps.KVMeta},
	})
}

type watchSource struct {
	shape string
	store *kvstore.Store
}

func (h *Handlers) watchBuckets(w http.ResponseWriter, r *http.Request, sources []watchSource) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	kvContext := r.PathValue("context")
	ctx := r.Context()

	type update struct {
		shape string
		entry jetstream.KeyValueEntry
	}
	updates := make(chan update, 16)

	for _, src := range sources {
		watcher, err := src.store.Watch(ctx, kvContext)
		if err != nil {
			h.deps.Log.Error("kv watch", "shape", src.shape, "context", kvContext, "err", err)
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

	var filterSubjects []string
	switch streamName {
	case domain.StreamName:
		filterSubjects = domain.StreamSubjects()
	default:
		writeError(w, http.StatusBadRequest, "unknown stream: "+streamName)
		return
	}

	consumer, err := h.deps.JS.OrderedConsumer(ctx, streamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: filterSubjects,
		DeliverPolicy:  policy,
	})
	if err != nil {
		h.deps.Log.Error("create ordered consumer", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	msgs, err := consumer.Messages()
	if err != nil {
		h.deps.Log.Error("consume jetstream messages", "err", err)
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
