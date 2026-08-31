package auth_test

import (
	"context"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

var _ = Describe("registry publisher grants", func() {
	Context("BR-AS21 — no browser transport permits announcement", func() {
		It("rejects a shell's announce publish at the broker before a service receives it", func() {
			kp, err := nkeys.CreateAccount()
			Expect(err).NotTo(HaveOccurred())
			pub, err := kp.PublicKey()
			Expect(err).NotTo(HaveOccurred())
			seed, err := kp.Seed()
			Expect(err).NotTo(HaveOccurred())
			token, err := auth.MintShellToken(context.Background(), &fakeSessionRegistry{}, pub, string(seed), "/nats", time.Minute)
			Expect(err).NotTo(HaveOccurred())
			claims, err := jwt.DecodeUserClaims(token.JWT)
			Expect(err).NotTo(HaveOccurred())
			// Exercise the actual minted grants through NATS's permission engine;
			// JWT trust-chain verification is separately covered by auth's tests.
			perms := &server.Permissions{
				Publish:   &server.SubjectPermission{Allow: claims.Permissions.Pub.Allow, Deny: claims.Permissions.Pub.Deny},
				Subscribe: &server.SubjectPermission{Allow: claims.Permissions.Sub.Allow, Deny: claims.Permissions.Sub.Deny},
			}
			srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, Users: []*server.User{
				{Username: "shell", Password: "shell", Permissions: perms}, {Username: "registry", Password: "registry"},
			}})
			Expect(err).NotTo(HaveOccurred())
			srv.Start()
			DeferCleanup(srv.Shutdown)
			Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
			service, err := nats.Connect(srv.ClientURL(), nats.UserInfo("registry", "registry"), nats.Name("registry-grant-test"))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(service.Close)
			messages, err := service.SubscribeSync(mferegistry.Announce)
			Expect(err).NotTo(HaveOccurred())
			Expect(service.Flush()).To(Succeed())
			refused := make(chan error, 1)
			shell, err := nats.Connect(srv.ClientURL(), nats.UserInfo("shell", "shell"), nats.Name("shell-grant-test"), nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) { refused <- err }))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(shell.Close)
			Expect(shell.Publish(mferegistry.Announce, []byte(`{}`))).To(Succeed())
			Expect(shell.Flush()).To(Succeed())
			Eventually(refused).Should(Receive(MatchError(ContainSubstring("Permissions Violation"))))
			_, err = messages.NextMsg(30 * time.Millisecond)
			Expect(err).To(MatchError(nats.ErrTimeout))
		})
	})
})
