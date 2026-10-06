package main

// The NATS commands (D03-R21, R22, R23), through nats.go. Each call makes
// its own connection to the server it was given (D03-R30) and closes it:
// a cached connection to a server that is later frozen would hang the next
// command. No reconnect, so a call never quietly moves to another server.
//
// Accounts are exercise 10's: LB (user lb) for streams, $SYS (user admin)
// for the meta step-down. Lab-only passwords, 127.0.0.1 only.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	userLB    = "lb"
	userAdmin = "admin"

	// connectFor is the most a connection may take. A frozen server accepts
	// the TCP connection and never sends INFO, so this is what ends it.
	connectFor = 2 * time.Second

	// metaStepDownSubject is what `nats server cluster step-down` sends —
	// `nats server raft step-down` in exercise 10, the same command under
	// its `raft` alias (JSApiLeaderStepDown, nats-server jetstream_api.go).
	// Checked against the CLI's --trace output in step 5, not taken from its
	// name.
	metaStepDownSubject = "$JS.API.META.LEADER.STEPDOWN"
)

type natsClient struct{}

// connect dials one server as one user. A failed connect sent nothing, so
// its error never wraps errNoReply or a deadline: the outcome is known.
func (natsClient) connect(ctx context.Context, via server, user string) (*nats.Conn, error) {
	wait := connectFor
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < wait {
		wait = time.Until(dl)
	}
	if wait <= 0 {
		return nil, fmt.Errorf("no time left to connect to %s", via.Name)
	}
	nc, err := nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", via.Client),
		nats.UserInfo(user, user),
		nats.Name("demo03-playground"),
		nats.Timeout(wait),
		nats.NoReconnect(),
	)
	if err != nil {
		return nil, fmt.Errorf("not sent: could not connect to %s in %s: %v", via.Name, secs(wait.Round(100*time.Millisecond)), err)
	}
	return nc, nil
}

// noReply marks a timeout as an unknown outcome (rule 6).
func noReply(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrTimeout) {
		return fmt.Errorf("%w: %v", errNoReply, err)
	}
	return err
}

func apiCode(err error) int {
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) {
		return int(apiErr.ErrorCode)
	}
	return 0
}

func (n natsClient) publish(ctx context.Context, via server, site, msgID string, body []byte, ackWait time.Duration) (pubAck, error) {
	nc, err := n.connect(ctx, via, userLB)
	if err != nil {
		return pubAck{}, err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return pubAck{}, err
	}
	msg := nats.NewMsg(subjectOf(site))
	msg.Header.Set(nats.MsgIdHdr, msgID)
	msg.Data = body
	pctx, cancel := context.WithTimeout(ctx, ackWait)
	defer cancel()
	ack, err := js.PublishMsg(pctx, msg)
	if err != nil {
		return pubAck{}, noReply(err)
	}
	if ack.Stream != streamOf(site) {
		return pubAck{}, fmt.Errorf("acked by stream %s, not %s", ack.Stream, streamOf(site))
	}
	return pubAck{Seq: ack.Sequence, Duplicate: ack.Duplicate}, nil
}

// readStream reads the stream's last sequence, then Direct Gets every
// sequence above afterSeq, each with its own verifyRequestFor limit.
func (n natsClient) readStream(ctx context.Context, via server, site string, afterSeq uint64) (streamRead, error) {
	nc, err := n.connect(ctx, via, userLB)
	if err != nil {
		return streamRead{}, err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return streamRead{}, err
	}
	rctx, cancel := context.WithTimeout(ctx, verifyRequestFor)
	st, err := js.Stream(rctx, streamOf(site))
	cancel()
	if err != nil {
		return streamRead{}, noReply(fmt.Errorf("stream info: %w", err))
	}
	out := streamRead{LastSeq: st.CachedInfo().State.LastSeq}
	for seq := afterSeq + 1; seq <= out.LastSeq; seq++ {
		rctx, cancel := context.WithTimeout(ctx, verifyRequestFor)
		m, err := st.GetMsg(rctx, seq)
		cancel()
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			continue
		}
		if err != nil {
			return streamRead{}, noReply(fmt.Errorf("direct get seq %d: %w", seq, err))
		}
		out.Msgs = append(out.Msgs, storedMsg{Seq: m.Sequence, MsgID: m.Header.Get(nats.MsgIdHdr)})
	}
	return out, nil
}

// probe is exercise 10's metadata probe: create PG_PROBE_<n> (memory, R1,
// in the entry server's cluster), delete it, check it is gone. It stops at
// the first step that fails.
func (n natsClient) probe(ctx context.Context, via server, num int) []probeStep {
	nc, err := n.connect(ctx, via, userLB)
	if err != nil {
		return []probeStep{{Step: "create", Err: err.Error()}}
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return []probeStep{{Step: "create", Err: err.Error()}}
	}
	name := fmt.Sprintf("PG_PROBE_%d", num)
	step := func(label string, f func(context.Context) error) probeStep {
		sctx, cancel := context.WithTimeout(ctx, probeCallFor)
		defer cancel()
		err := f(sctx)
		if err == nil {
			return probeStep{Step: label, OK: true}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return probeStep{Step: label, Err: fmt.Sprintf("no reply in %s", secs(probeCallFor))}
		}
		return probeStep{Step: label, Code: apiCode(err), Err: err.Error()}
	}
	var steps []probeStep
	for _, s := range []struct {
		label string
		f     func(context.Context) error
	}{
		{"create", func(c context.Context) error {
			_, err := js.CreateStream(c, jetstream.StreamConfig{
				Name:      name,
				Subjects:  []string{fmt.Sprintf("pg.probe.%d", num)},
				Storage:   jetstream.MemoryStorage,
				Replicas:  1,
				Placement: &jetstream.Placement{Cluster: via.Cluster},
			})
			return err
		}},
		{"delete", func(c context.Context) error { return js.DeleteStream(c, name) }},
		{"gone", func(c context.Context) error {
			_, err := js.Stream(c, name)
			if errors.Is(err, jetstream.ErrStreamNotFound) {
				return nil
			}
			if err == nil {
				return errors.New(name + " is still present")
			}
			return err
		}},
	} {
		st := step(s.label, s.f)
		steps = append(steps, st)
		if !st.OK {
			break
		}
	}
	return steps
}

// stepDown sends the meta step-down with a placement cluster, as $SYS. An
// accepted reply says only that the request was taken; the leader is read
// from the monitors afterwards.
func (n natsClient) stepDown(ctx context.Context, via server, cluster string) error {
	nc, err := n.connect(ctx, via, userAdmin)
	if err != nil {
		return err
	}
	defer nc.Close()
	req, _ := json.Marshal(map[string]any{"placement": map[string]string{"cluster": cluster}})
	msg, err := nc.RequestWithContext(ctx, metaStepDownSubject, req)
	if err != nil {
		return noReply(err)
	}
	var resp struct {
		Success bool `json:"success"`
		Error   *struct {
			Code        int    `json:"code"`
			ErrCode     int    `json:"err_code"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return fmt.Errorf("unreadable reply: %v", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("%s (%d)", resp.Error.Description, resp.Error.ErrCode)
	}
	if !resp.Success {
		return fmt.Errorf("reply did not say success: %s", msg.Data)
	}
	return nil
}

// ensureStreams creates exercise 10's three streams, as its step 1 does:
// ODOMETER_<SITE> on evt.odo.<site>.v1, file, R3, placed in <site>, Direct
// Get on, created through a server in that site. An identical stream that
// already exists is accepted.
func (n natsClient) ensureStreams(ctx context.Context) error {
	for _, site := range []string{clusterArb, clusterZA, clusterAU} {
		via := serversIn(site)[0]
		nc, err := n.connect(ctx, via, userLB)
		if err != nil {
			return err
		}
		js, err := jetstream.New(nc)
		if err == nil {
			cctx, cancel := context.WithTimeout(ctx, probeCallFor)
			_, err = js.CreateStream(cctx, jetstream.StreamConfig{
				Name:        streamOf(site),
				Subjects:    []string{subjectOf(site)},
				Storage:     jetstream.FileStorage,
				Replicas:    3,
				Placement:   &jetstream.Placement{Cluster: site},
				AllowDirect: true,
			})
			cancel()
		}
		nc.Close()
		if err != nil {
			return fmt.Errorf("create %s: %v", streamOf(site), err)
		}
	}
	return nil
}
