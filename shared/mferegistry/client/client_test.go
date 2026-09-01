package client_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	registryclient "github.com/jthomas78/nats-tech-lab/shared/mferegistry/client"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type recordingSigner struct {
	publicKey string
	signed    []byte
}

func (s *recordingSigner) PublicKey() (string, error) { return s.publicKey, nil }
func (s *recordingSigner) Sign(payload []byte) ([]byte, error) {
	s.signed = append([]byte(nil), payload...)
	return []byte("test-signature"), nil
}

type recordingRequester struct {
	subject  string
	payload  []byte
	response []byte
	err      error
}

func (r *recordingRequester) RequestWithContext(_ context.Context, subject string, payload []byte) (*nats.Msg, error) {
	r.subject = subject
	r.payload = append([]byte(nil), payload...)
	if r.err != nil {
		return nil, r.err
	}
	return &nats.Msg{Data: r.response}, nil
}

var _ = Describe("Signed publisher client", func() {
	var (
		signer    *recordingSigner
		requester *recordingRequester
		client    *registryclient.Client
	)

	BeforeEach(func() {
		signer = &recordingSigner{publicKey: "UPUBLISHER"}
		requester = &recordingRequester{}
		client = registryclient.New(requester, signer, "publisher-a")
	})

	Context("BR-AS37 — the signed manifest is sent as signed", func() {
		It("produces a publisher NKey signature over the returned payload", func() {
			pair, err := nkeys.CreatePair(nkeys.PrefixByteUser)
			Expect(err).NotTo(HaveOccurred())
			seed, err := pair.Seed()
			Expect(err).NotTo(HaveOccurred())
			seedPair, err := registryclient.NewNKeySigner(seed)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(seedPair.Wipe)
			realClient := registryclient.New(requester, seedPair, "publisher-a")

			req, err := realClient.BuildAnnounce(json.RawMessage(`{"id":"plugin-a","release":7}`))

			Expect(err).NotTo(HaveOccurred())
			publicKey, err := seedPair.PublicKey()
			Expect(err).NotTo(HaveOccurred())
			verifier, err := nkeys.FromPublicKey(publicKey)
			Expect(err).NotTo(HaveOccurred())
			signature, err := base64.StdEncoding.DecodeString(req.Signature)
			Expect(err).NotTo(HaveOccurred())
			Expect(verifier.Verify(req.Payload, signature)).To(Succeed())
		})

		It("signs the exact announce payload bytes recovered by the server from the published request", func() {
			requester.response = []byte(`{"ok":true,"outcome":"inserted","revision":3}`)
			manifest := json.RawMessage("{\n  \"release\": 7,\n  \"id\": \"plugin-a\"\n}")

			out, err := client.Announce(context.Background(), manifest)

			Expect(err).NotTo(HaveOccurred())
			Expect(out.Outcome).To(Equal(mferegistry.AnnounceInserted))
			Expect(requester.subject).To(Equal(mferegistry.Announce))
			var sent mferegistry.Request
			Expect(json.Unmarshal(requester.payload, &sent)).To(Succeed())
			Expect([]byte(sent.Payload)).To(Equal(signer.signed))
			Expect(sent.SigningKey).To(Equal("UPUBLISHER"))
			Expect(sent.Signature).To(Equal(base64.StdEncoding.EncodeToString([]byte("test-signature"))))
		})

		It("signs the exact unregister command bytes recovered from the published request", func() {
			requester.response = []byte(`{"ok":true,"outcome":"withdrawn","revision":4}`)

			out, err := client.Unregister(context.Background(), "plugin-a", 8)

			Expect(err).NotTo(HaveOccurred())
			Expect(out.Outcome).To(Equal(mferegistry.UnregisterWithdrawn))
			Expect(requester.subject).To(Equal(mferegistry.Unregister))
			var sent mferegistry.Request
			Expect(json.Unmarshal(requester.payload, &sent)).To(Succeed())
			Expect([]byte(sent.Payload)).To(Equal(signer.signed))
		})
	})

	Context("BR-AS47 — the release belongs inside the signed command", func() {
		It("reports an equal-release NoOp as success so a retry is safe", func() {
			requester.response = []byte(`{"ok":true,"revision":8,"noOp":true}`)

			out, err := client.Announce(context.Background(), json.RawMessage(`{"id":"plugin-a","release":18}`))

			Expect(err).NotTo(HaveOccurred())
			Expect(out.NoOp).To(BeTrue())
		})

		It("binds unregister action, plugin, publisher, key, release and schema version into the signature", func() {
			req, err := client.BuildUnregister("plugin-a", 19)

			Expect(err).NotTo(HaveOccurred())
			Expect([]byte(req.Payload)).To(Equal(signer.signed))
			var command map[string]any
			Expect(json.Unmarshal(req.Payload, &command)).To(Succeed())
			Expect(command).To(Equal(map[string]any{
				"action":        "unregister",
				"plugin":        "plugin-a",
				"publisher":     "publisher-a",
				"release":       float64(19),
				"schemaVersion": float64(1),
				"signingKey":    "UPUBLISHER",
			}))
		})

		It("preserves an announcement release inside the signed manifest", func() {
			req, err := client.BuildAnnounce(json.RawMessage(`{"id":"plugin-a","release":18}`))

			Expect(err).NotTo(HaveOccurred())
			Expect([]byte(req.Payload)).To(Equal(signer.signed))
			Expect(req.Payload).To(MatchJSON(`{"id":"plugin-a","release":18}`))
		})
	})

	Context("request failures", func() {
		It("returns the registry's closed error code to the publisher", func() {
			requester.response = []byte(`{"error":"older release","code":"release-backwards"}`)

			_, err := client.Announce(context.Background(), json.RawMessage(`{"id":"plugin-a","release":1}`))

			var remote *registryclient.RemoteError
			Expect(errors.As(err, &remote)).To(BeTrue())
			Expect(remote.Code).To(Equal("release-backwards"))
		})
	})
})
