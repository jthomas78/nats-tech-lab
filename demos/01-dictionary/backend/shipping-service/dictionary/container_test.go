package dictionary

// Container / terminal domain specs (Phase 8). Each Context maps to a rule in
// BUSINESS_RULES.md (BR-008 … BR-018). Written before the implementation
// (red → green → refactor). Both aggregates live on the single SHIPPING
// stream, so every cross-aggregate rule is enforced from one atomic replay.

import (
	"context"
	"errors"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/shared/jstream"
)

var _ = Describe("Container Domain Rules", func() {
	var (
		ctx        context.Context
		ships      *commands.ShipHandler
		containers *commands.ContainerHandler
	)

	const fleetCtx = "acme-pacific-fleet"

	BeforeEach(func() {
		ctx = context.Background()
		js := newJetStream()
		pub := jstream.NewPublisher(js)
		portRepo := newFakePortRepo()
		ships = commands.NewShipHandler(pub, js, portRepo)
		containers = commands.NewContainerHandler(pub, js, portRepo)
	})

	// ── scenario helpers ──────────────────────────────────────────────────────

	arrive := func(shipID, port string) {
		GinkgoHelper()
		_, err := ships.ArrivePort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: shipID, ShipName: shipID, Port: port,
		})
		Expect(err).NotTo(HaveOccurred())
	}
	depart := func(shipID, port string) {
		GinkgoHelper()
		_, err := ships.DepartPort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: shipID, Port: port,
		})
		Expect(err).NotTo(HaveOccurred())
	}
	register := func(containerID, origin, dest string) {
		GinkgoHelper()
		_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: containerID,
			Cargo: "Electronics", OriginPort: origin, DestPort: dest,
		})
		Expect(err).NotTo(HaveOccurred())
	}
	load := func(containerID, shipID string) error {
		_, err := containers.LoadContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: containerID, ShipID: shipID,
		})
		return err
	}
	unload := func(containerID, shipID string) error {
		_, err := containers.UnloadContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: containerID, ShipID: shipID,
		})
		return err
	}

	// ── happy path ────────────────────────────────────────────────────────────

	Context("container lifecycle: registered → loaded → unloaded at destination", func() {
		It("moves the container from origin terminal to ship to destination terminal", func() {
			By("registering the container in the Hamburg terminal")
			register("TCKU1234567", "Hamburg", "Singapore")

			By("loading it onto a ship docked at Hamburg")
			arrive("lifecycle-ship", "Hamburg")
			Expect(load("TCKU1234567", "lifecycle-ship")).To(Succeed())

			By("sailing to Singapore")
			depart("lifecycle-ship", "Hamburg")
			arrive("lifecycle-ship", "Singapore")

			By("unloading at the destination")
			state, err := containers.UnloadContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU1234567", ShipID: "lifecycle-ship",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(state.Status).To(Equal(domain.ContainerInTerminal))
			Expect(state.TerminalPort).To(HaveValue(Equal("Singapore")))
			Expect(state.OnShipID).To(BeNil())
		})

		It("reports on-ship state while loaded", func() {
			register("TCKU7654321", "Hamburg", "Singapore")
			arrive("loaded-ship", "Hamburg")

			state, err := containers.LoadContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU7654321", ShipID: "loaded-ship",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(state.Status).To(Equal(domain.ContainerOnShip))
			Expect(state.OnShipID).To(HaveValue(Equal("loaded-ship")))
			Expect(state.TerminalPort).To(BeNil())
		})
	})

	// ── business rules ────────────────────────────────────────────────────────

	Context("BR-008: cannot load a container whose destination is the ship's current port", func() {
		It("returns ErrContainerAtDestination", func() {
			By("delivering the container to its destination via a first voyage")
			register("TCKU0000080", "Hamburg", "Rotterdam")
			arrive("br008-first", "Hamburg")
			Expect(load("TCKU0000080", "br008-first")).To(Succeed())
			depart("br008-first", "Hamburg")
			arrive("br008-first", "Rotterdam")
			Expect(unload("TCKU0000080", "br008-first")).To(Succeed())

			By("a second ship docked at Rotterdam tries to load the delivered container")
			arrive("br008-second", "Rotterdam")
			err := load("TCKU0000080", "br008-second")
			Expect(errors.Is(err, domain.ErrContainerAtDestination)).To(BeTrue())
		})
	})

	Context("BR-009: a container can only be unloaded at its destination port", func() {
		It("returns ErrWrongDestination", func() {
			register("TCKU0000090", "Hamburg", "Singapore")
			arrive("br009-ship", "Hamburg")
			Expect(load("TCKU0000090", "br009-ship")).To(Succeed())
			depart("br009-ship", "Hamburg")
			arrive("br009-ship", "Rotterdam")

			err := unload("TCKU0000090", "br009-ship")
			Expect(errors.Is(err, domain.ErrWrongDestination)).To(BeTrue())
		})
	})

	Context("BR-010: a container must be in-terminal to be loaded", func() {
		It("returns ErrContainerNotInTerminal", func() {
			register("TCKU0000100", "Hamburg", "Singapore")
			arrive("br010-ship", "Hamburg")
			Expect(load("TCKU0000100", "br010-ship")).To(Succeed())

			err := load("TCKU0000100", "br010-ship")
			Expect(errors.Is(err, domain.ErrContainerNotInTerminal)).To(BeTrue())
		})
	})

	Context("BR-011: a container must be on-ship to be unloaded", func() {
		It("returns ErrContainerNotOnShip", func() {
			register("TCKU0000110", "Hamburg", "Singapore")
			arrive("br011-ship", "Hamburg")

			err := unload("TCKU0000110", "br011-ship")
			Expect(errors.Is(err, domain.ErrContainerNotOnShip)).To(BeTrue())
		})
	})

	Context("BR-012: a ship must be docked to load or unload containers", func() {
		It("rejects load while the ship is at sea", func() {
			register("TCKU0000120", "Hamburg", "Singapore")

			err := load("TCKU0000120", "br012-at-sea")
			Expect(errors.Is(err, domain.ErrNotInPort)).To(BeTrue())
		})

		It("rejects unload while the ship is at sea", func() {
			register("TCKU0000121", "Hamburg", "Singapore")
			arrive("br012-ship", "Hamburg")
			Expect(load("TCKU0000121", "br012-ship")).To(Succeed())
			depart("br012-ship", "Hamburg")

			err := unload("TCKU0000121", "br012-ship")
			Expect(errors.Is(err, domain.ErrNotInPort)).To(BeTrue())
		})
	})

	Context("BR-013: a container can only be unloaded from the ship it is actually on", func() {
		It("returns ErrWrongShip", func() {
			register("TCKU0000130", "Hamburg", "Singapore")
			arrive("br013-carrier", "Hamburg")
			Expect(load("TCKU0000130", "br013-carrier")).To(Succeed())

			By("a different ship docked at the destination tries to unload it")
			arrive("br013-imposter", "Singapore")
			err := unload("TCKU0000130", "br013-imposter")
			Expect(errors.Is(err, domain.ErrWrongShip)).To(BeTrue())
		})
	})

	Context("BR-014: a container can only be loaded when the ship is docked at the container's terminal port", func() {
		It("returns ErrContainerNotAtPort", func() {
			register("TCKU0000140", "Rotterdam", "Singapore")
			arrive("br014-ship", "Hamburg")

			err := load("TCKU0000140", "br014-ship")
			Expect(errors.Is(err, domain.ErrContainerNotAtPort)).To(BeTrue())
		})
	})

	Context("BR-015: a container ID can only be registered once", func() {
		It("returns ErrContainerExists", func() {
			register("TCKU0000150", "Hamburg", "Singapore")

			_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000150",
				Cargo: "Textiles", OriginPort: "Rotterdam", DestPort: "Sydney",
			})
			Expect(errors.Is(err, domain.ErrContainerExists)).To(BeTrue())
		})
	})

	Context("BR-016: a container ID must be in ISO 6346 format (TCKU + 7 digits)", func() {
		It("returns ErrInvalidContainerID for a non-TCKU prefix", func() {
			_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "MSCU0000160",
				Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Singapore",
			})
			Expect(errors.Is(err, domain.ErrInvalidContainerID)).To(BeTrue())
		})

		It("returns ErrInvalidContainerID for the wrong digit count", func() {
			_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU001",
				Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Singapore",
			})
			Expect(errors.Is(err, domain.ErrInvalidContainerID)).To(BeTrue())
		})
	})

	Context("BR-018: a container's origin and destination ports must be registered", func() {
		It("returns ErrUnknownPort for an unregistered origin port", func() {
			_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000180",
				Cargo: "Electronics", OriginPort: "Atlantis", DestPort: "Singapore",
			})
			Expect(errors.Is(err, domain.ErrUnknownPort)).To(BeTrue())
		})

		It("returns ErrUnknownPort for an unregistered destination port", func() {
			_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000181",
				Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Atlantis",
			})
			Expect(errors.Is(err, domain.ErrUnknownPort)).To(BeTrue())
		})
	})

	// ── surrogate key (Phase 8.3) ─────────────────────────────────────────────

	Context("surrogate key: the container's identity is an immutable UUID, not the ISO 6346 natural key", func() {
		It("assigns a UUID at registration, distinct from the container ID", func() {
			state, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000200",
				Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Singapore",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(state.ID).To(MatchRegexp(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`))
			Expect(state.ID).NotTo(Equal(state.ContainerID))
		})

		It("keeps the same id stable across load and unload", func() {
			registered, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000201",
				Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Rotterdam",
			})
			Expect(err).NotTo(HaveOccurred())
			id := registered.ID

			arrive("surrogate-ship", "Hamburg")
			loaded, err := containers.LoadContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000201", ShipID: "surrogate-ship",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.ID).To(Equal(id))

			depart("surrogate-ship", "Hamburg")
			arrive("surrogate-ship", "Rotterdam")
			unloaded, err := containers.UnloadContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000201", ShipID: "surrogate-ship",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(unloaded.ID).To(Equal(id))
		})

		It("still rejects a duplicate natural key (BR-015) even though identity is the surrogate key", func() {
			first, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000202",
				Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Singapore",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(first.ID).NotTo(BeEmpty())

			_, err = containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000202",
				Cargo: "Textiles", OriginPort: "Rotterdam", DestPort: "Sydney",
			})
			Expect(errors.Is(err, domain.ErrContainerExists)).To(BeTrue())
		})
	})

	// ── guards (not numbered rules) ───────────────────────────────────────────

	Context("unregistered container", func() {
		It("rejects load with ErrContainerNotFound", func() {
			arrive("ghost-ship", "Hamburg")
			err := load("TCKU9999999", "ghost-ship")
			Expect(errors.Is(err, domain.ErrContainerNotFound)).To(BeTrue())
		})
	})

	Context("input validation (application layer, like BR-007)", func() {
		It("rejects register without a container ID", func() {
			_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Singapore",
			})
			Expect(err).To(MatchError("containerID is required"))
		})

		It("rejects load without a ship ID", func() {
			_, err := containers.LoadContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: "TCKU0000001",
			})
			Expect(err).To(MatchError("shipID is required"))
		})
	})
})

// ─── Terminal read models ─────────────────────────────────────────────────────

var _ = Describe("Terminal read models", func() {
	var (
		ctx        context.Context
		ships      *commands.ShipHandler
		containers *commands.ContainerHandler
		terminal   *queries.Terminal
		meta       *queries.Meta
	)

	const fleetCtx = "acme-pacific-fleet"

	BeforeEach(func() {
		ctx = context.Background()
		js := newJetStream()
		pub := jstream.NewPublisher(js)
		log := slog.New(slog.DiscardHandler)

		kvContainers := kvstore.New(js, "container")
		kvMeta := kvstore.New(js, "meta")

		consumeC, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nil, newFakeContainerRepo(), log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(consumeC.Stop)

		consumeM, err := eventhandler.RegisterMeta(ctx, js, kvMeta, nil, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(consumeM.Stop)

		portRepo := newFakePortRepo()
		ships = commands.NewShipHandler(pub, js, portRepo)
		containers = commands.NewContainerHandler(pub, js, portRepo)
		terminal = queries.NewTerminal(kvContainers)
		meta = queries.NewMeta(kvMeta)
	})

	It("projects containers into the terminal KV bucket, queryable by port and by ship", func() {
		By("registering two containers in the Hamburg terminal")
		for _, id := range []string{"TCKU1111111", "TCKU2222222"} {
			_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
				Context: fleetCtx, ContainerID: id,
				Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Singapore",
			})
			Expect(err).NotTo(HaveOccurred())
		}

		By("both appear in the Hamburg yard")
		eventually(func() error {
			inYard, err := terminal.ListByPort(ctx, fleetCtx, "Hamburg")
			if err != nil {
				return err
			}
			if len(inYard) != 2 {
				return errors.New("waiting for containers in Hamburg yard")
			}
			return nil
		})

		By("loading one onto a docked ship")
		_, err := ships.ArrivePort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: "reader-ship", ShipName: "Reader", Port: "Hamburg",
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = containers.LoadContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: "TCKU1111111", ShipID: "reader-ship",
		})
		Expect(err).NotTo(HaveOccurred())

		By("the yard shrinks to one and the ship manifest shows one")
		eventually(func() error {
			inYard, err := terminal.ListByPort(ctx, fleetCtx, "Hamburg")
			if err != nil {
				return err
			}
			if len(inYard) != 1 {
				return errors.New("yard not updated yet")
			}
			manifest, err := terminal.ListByShip(ctx, fleetCtx, "reader-ship")
			if err != nil {
				return err
			}
			if len(manifest) != 1 || manifest[0].ContainerID != "TCKU1111111" {
				return errors.New("manifest not updated yet")
			}
			return nil
		})
	})

	It("maintains meta.known-containers", func() {
		_, err := ships.ArrivePort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: "meta-ship", ShipName: "Meta", Port: "Hamburg",
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = containers.RegisterContainer(ctx, commands.ContainerInput{
			Context: fleetCtx, ContainerID: "TCKU3333333",
			Cargo: "Textiles", OriginPort: "Rotterdam", DestPort: "Singapore",
		})
		Expect(err).NotTo(HaveOccurred())

		By("known-containers accumulates registered container IDs")
		eventually(func() error {
			ids, err := meta.KnownContainers(ctx, fleetCtx)
			if err != nil {
				return err
			}
			if len(ids) != 1 || ids[0] != "TCKU3333333" {
				return errors.New("known-containers not updated yet")
			}
			return nil
		})
	})
})

// ─── fakeContainerRepo ────────────────────────────────────────────────────────

type fakeContainerRepo struct{}

func newFakeContainerRepo() *fakeContainerRepo { return &fakeContainerRepo{} }

func (r *fakeContainerRepo) Upsert(_ context.Context, state domain.ContainerState) (domain.ContainerState, error) {
	return state, nil
}
