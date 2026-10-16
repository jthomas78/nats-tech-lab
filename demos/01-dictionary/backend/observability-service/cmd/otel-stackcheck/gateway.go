package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// Phase 54c stack check for the trace query gateway (BR-074, BR-075).
//
//	gateway <n>   n ship moves of STK-SHIP-01; each is looked for through
//	              trace.search.v1 and then read whole through trace.get.v1,
//	              as the Admin UI does. Prints the time to each. Then checks
//	              the reply schema, the limited flag, trace.services, a
//	              refused body, and that a tenant token gets no trace data.

const (
	searchSubject   = "api._platform.observability.trace.search.v1"
	servicesSubject = "api._platform.observability.trace.services.v1"
)

type gatewaySearch struct {
	Schema  string `json:"schema"`
	Limit   int    `json:"limit"`
	Limited bool   `json:"limited"`
	Traces  []struct {
		TraceID       string   `json:"traceId"`
		RootName      string   `json:"rootName"`
		StartUnixNano int64    `json:"startUnixNano"`
		SpanCount     int      `json:"spanCount"`
		Services      []string `json:"services"`
	} `json:"traces"`
}

// ask sends one request and returns the body, or the micro error code.
func ask(nc *nats.Conn, subject string, body any) ([]byte, string, error) {
	data, _ := json.Marshal(body)
	if s, ok := body.(string); ok {
		data = []byte(s)
	}
	reply, err := nc.Request(subject, data, 5*time.Second)
	if err != nil {
		return nil, "", err
	}
	return reply.Data, reply.Header.Get("Nats-Service-Error-Code"), nil
}

func gateway(n int) bool {
	admin := adminConn()
	defer admin.Close()
	acme := tenantConn("acme")
	defer acme.Close()
	pass := true
	check := func(name string, ok bool, detail string) {
		fmt.Printf("%s %s %s\n", verdict(ok), name, detail)
		pass = pass && ok
	}

	const ship = "STK-SHIP-01"
	move := func(action, port string) (string, error) {
		msg := nats.NewMsg("api.acme.shipping.ship." + action + ".v1")
		msg.Data, _ = json.Marshal(map[string]any{"shipID": ship, "port": port})
		reply, err := acme.RequestMsg(msg, 5*time.Second)
		if err != nil {
			return "", err
		}
		return reply.Header.Get("Nats-Service-Error"), nil
	}
	cur := ""
	if e, _ := move("depart", ""); strings.Contains(e, "currently: ") {
		cur = strings.TrimSuffix(strings.SplitN(e, "currently: ", 2)[1], ")")
	}
	// The probe above is a trace too, with the same root name as a depart.
	// Wait past it, so a move never matches the probe.
	time.Sleep(1500 * time.Millisecond)

	var found, whole []float64
	for i := 0; i < n; i++ {
		action, port := "depart", cur
		if cur == "" {
			action, port = "arrive", seedPorts[i%len(seedPorts)]
		}
		sent := time.Now()
		code, err := move(action, port)
		t0 := time.Now()
		if err != nil || code != "" {
			fmt.Printf("move %d %s %s: err=%v error=%q\n", i, action, port, err, code)
			continue
		}
		if action == "depart" {
			cur = ""
		} else {
			cur = port
		}
		root := "api.acme.shipping.ship." + action + ".v1"
		id := ""
		for time.Since(t0) < 120*time.Second && id == "" {
			data, code, err := ask(admin, searchSubject, map[string]any{"name": root, "sinceMs": 120000, "limit": 20})
			if err == nil && code == "" {
				var r gatewaySearch
				_ = json.Unmarshal(data, &r)
				for _, t := range r.Traces {
					if t.StartUnixNano >= sent.UnixNano() && t.RootName == root {
						id = t.TraceID
					}
				}
			}
			if id == "" {
				time.Sleep(250 * time.Millisecond)
			}
		}
		if id == "" {
			fmt.Printf("move %d %s: NOT FOUND through trace.search\n", i, action)
			pass = false
			continue
		}
		f := time.Since(t0)
		spans := 0
		for time.Since(t0) < 120*time.Second && spans < 6 {
			data, code, err := ask(admin, getSubject, map[string]any{"traceId": id})
			if err == nil && code == "" {
				var tr struct {
					Spans []json.RawMessage `json:"spans"`
				}
				_ = json.Unmarshal(data, &tr)
				spans = len(tr.Spans)
			}
			if spans < 6 {
				time.Sleep(250 * time.Millisecond)
			}
		}
		w := time.Since(t0)
		if spans < 6 {
			fmt.Printf("move %d %s: trace %s has %d of 6 spans\n", i, action, id, spans)
			pass = false
			continue
		}
		found = append(found, f.Seconds())
		whole = append(whole, w.Seconds())
		fmt.Printf("move %d %s: trace %s found %.1fs, trace.get %d spans %.1fs\n", i, action, id, f.Seconds(), spans, w.Seconds())
	}
	fmt.Printf("GATEWAY search found: %s\nGATEWAY get whole: %s\n", stats(found), stats(whole))
	check("G1 every move found and whole", len(found) == n, fmt.Sprintf("(%d of %d)", len(found), n))

	data, code, err := ask(admin, searchSubject, map[string]any{"limit": 1})
	var r gatewaySearch
	_ = json.Unmarshal(data, &r)
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	_, hasTotal := raw["total"]
	check("G2 search schema, limit 1 is limited, no total",
		err == nil && code == "" && r.Schema == "lab.tracesearch.v1" && r.Limit == 1 && r.Limited && len(r.Traces) == 1 && !hasTotal,
		fmt.Sprintf("(schema %q, limited %v, traces %d, err %v, code %q)", r.Schema, r.Limited, len(r.Traces), err, code))

	data, code, err = ask(admin, servicesSubject, "{}")
	var s struct {
		Schema   string   `json:"schema"`
		Services []string `json:"services"`
	}
	_ = json.Unmarshal(data, &s)
	hasShipping := false
	for _, name := range s.Services {
		hasShipping = hasShipping || name == "shipping-service"
	}
	check("G3 services lists shipping-service", err == nil && code == "" && s.Schema == "lab.traceservices.v1" && hasShipping,
		fmt.Sprintf("(%v)", s.Services))

	_, code, err = ask(admin, searchSubject, `{"service":"x\" } || { "}`)
	check("G4 a quote in a filter is refused", err == nil && code == "400", fmt.Sprintf("(code %q, err %v)", code, err))

	data, code, err = ask(acme, searchSubject, "{}")
	check("G5 a tenant token gets no trace data", err != nil || (code != "" && len(data) == 0),
		fmt.Sprintf("(err %v, code %q)", err, code))
	return pass
}
