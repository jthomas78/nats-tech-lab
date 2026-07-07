package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
)

// watchEvent is one SSE payload: a KV change in either shape's bucket.
type watchEvent struct {
	Shape    string          `json:"shape"` // "A" or "B"
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

// watch streams KV changes for a context over SSE. It watches both the
// Shape A read-model bucket and the Shape B cache bucket, replaying current
// state first, then pushing live updates. This is the server half of the
// KV watch → SSE → Pinia store pipeline.
func (h *Handlers) watch(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	kvContext := r.PathValue("context")
	ctx := r.Context()

	watcherA, err := h.kvA.Watch(ctx, kvContext)
	if err != nil {
		h.log.Error("watch shape A", "context", kvContext, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = watcherA.Stop() }()

	watcherB, err := h.kvB.Watch(ctx, kvContext)
	if err != nil {
		h.log.Error("watch shape B", "context", kvContext, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = watcherB.Stop() }()

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
		case entry := <-watcherA.Updates():
			send("A", entry)
		case entry := <-watcherB.Updates():
			send("B", entry)
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

// watchJetStream streams raw JetStream messages from the DICTIONARY stream over
// SSE. It uses an ephemeral ordered consumer with DeliverNew so only messages
// published after the connection is established are delivered — no replay.
func (h *Handlers) watchJetStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ctx := r.Context()

	consumer, err := h.js.OrderedConsumer(ctx, domain.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects:  domain.StreamSubjects(),
		DeliverPolicy:   jetstream.DeliverNewPolicy,
	})
	if err != nil {
		h.log.Error("create ordered consumer", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	msgs, err := consumer.Messages()
	if err != nil {
		h.log.Error("consume jetstream messages", "err", err)
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
