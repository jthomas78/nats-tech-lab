package main

import (
	"encoding/json"
	"errors"
	"testing"

	. "github.com/onsi/gomega"
)

func TestSeederUsesOperatorSubjectsAndExplicitZero(t *testing.T) {
	g := NewWithT(t)
	var subjects []string
	c := &client{request: func(subject string, data []byte) ([]byte, error) {
		subjects = append(subjects, subject)
		if len(subjects) == 2 {
			var payload map[string]any
			g.Expect(json.Unmarshal(data, &payload)).To(Succeed())
			g.Expect(payload["ifRevision"]).To(Equal(float64(0)))
			g.Expect(payload["entryId"]).To(Equal("fleet"))
			g.Expect(payload["entry"]).NotTo(BeNil())
		}
		return []byte(`{"revision":0}`), nil
	}}
	rev, err := c.revision()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(c.upsert(json.RawMessage(`{"id":"fleet"}`), rev)).To(Succeed())
	g.Expect(subjects).To(Equal([]string{"api._platform.registry.entries.curated.v1", "api._platform.registry.entries.upsert.v1"}))
}

func TestSeederStopsOnStaleRefusalWithoutRetryingOrMerging(t *testing.T) {
	g := NewWithT(t)
	calls := 0
	c := &client{request: func(string, []byte) ([]byte, error) {
		calls++
		return []byte(`{"error":"registry moved","conflict":true,"currentRevision":9,"yourRevision":4}`), nil
	}}
	g.Expect(c.upsert(json.RawMessage(`{"id":"fleet"}`), 4)).To(MatchError("registry moved"))
	g.Expect(calls).To(Equal(1))
}

func TestSeederPropagatesUnreadableReplies(t *testing.T) {
	g := NewWithT(t)
	for _, raw := range []string{`not JSON`, `{"error":"unavailable"}`} {
		c := &client{request: func(string, []byte) ([]byte, error) { return []byte(raw), nil }}
		_, err := c.revision()
		g.Expect(err).To(HaveOccurred())
	}
	c := &client{request: func(string, []byte) ([]byte, error) { return nil, errors.New("timeout") }}
	_, err := c.revision()
	g.Expect(err).To(MatchError("timeout"))
}
