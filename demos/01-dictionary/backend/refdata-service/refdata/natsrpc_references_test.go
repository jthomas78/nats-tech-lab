package refdata_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/natsrpc"
)

// item.get gained a References field in Phase 38d-ii so a cross-service
// caller can read a hierarchy the corpus already states (BR-D47) instead of
// re-deriving it — BR-TP48 resolves a region's parent country this way.
//
// The field is additive and omitempty, so these specs also pin the promise
// that a consumer written before it existed is unaffected.
var _ = Describe("rpc.* item.get carries an item's typed references (Phase 38d-ii)", func() {
	const itemCtx = "acme-test"

	var (
		ctx    context.Context
		itemH  *commands.ItemHandler
		refH   *commands.ReferenceHandler
		locH   *commands.LocalizationHandler
		regH   *commands.RegionHandler
		reqRPC func(typeKey, code string) natsrpc.ItemGetResponse
	)

	BeforeEach(func() {
		ctx = context.Background()
		items := newFakeItemRepo()
		refs := newFakeReferenceRepo()
		locs := newFakeLocalizationRepo()
		locales := newFakeLocaleRepo()

		itemH = commands.NewItemHandler(items, refs, nil)
		refH = commands.NewReferenceHandler(items, refs, nil)
		locH = commands.NewLocalizationHandler(items, locs, locales, nil)
		regH = commands.NewRegionHandler(items, refs, nil)

		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: domain.CountryTypeKey, Code: "ZA", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())
		_, err = regH.RegisterRegion(ctx, commands.RegionInput{
			Context: itemCtx, Code: "ZA-GP", CountryCode: "ZA", Name: "Gauteng",
		})
		Expect(err).NotTo(HaveOccurred())

		nc := newTestNATSConn()
		adapter, err := natsrpc.New(nc, natsrpc.Deps{Localizations: locH, Items: itemH, References: refH})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })

		reqRPC = func(typeKey, code string) natsrpc.ItemGetResponse {
			GinkgoHelper()
			body, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: typeKey, Code: code, Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			msg, err := nc.Request("rpc."+itemCtx+".refdata.item.get.v1", body, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())
			var resp natsrpc.ItemGetResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			return resp
		}
	})

	It("returns a region's country relation, so BR-TP48 need not infer parentage from the code", func() {
		resp := reqRPC(domain.RegionTypeKey, "ZA-GP")

		Expect(resp.References).To(HaveLen(1))
		ref := resp.References[0]
		Expect(ref.Relation).To(Equal(domain.RegionCountryRelation))
		Expect(ref.ToTypeKey).To(Equal(domain.CountryTypeKey))
		Expect(ref.ToCode).To(Equal("ZA"))
	})

	It("omits the field entirely for an item with no references", func() {
		// omitempty matters on the wire, not just in Go: a consumer decoding
		// into an older struct must see no unexpected key, and one decoding
		// into the new struct must see a nil slice rather than an empty
		// object it might treat as "references were fetched and none exist"
		// versus "not fetched".
		resp := reqRPC(domain.CountryTypeKey, "ZA")
		Expect(resp.References).To(BeEmpty())

		raw, err := json.Marshal(resp)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).NotTo(ContainSubstring("references"))
	})

	It("leaves every pre-existing response field unchanged", func() {
		// The contract change is additive. If this ever fails, a consumer
		// that never asked for references has been broken by a field it
		// does not read.
		resp := reqRPC(domain.RegionTypeKey, "ZA-GP")

		direct, err := locH.ResolveItem(ctx, domain.RegionTypeKey, itemCtx, "ZA-GP", "en")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Item).To(Equal(direct.Item))
		Expect(resp.Label).To(Equal(direct.Localization.Label))
		Expect(resp.IsFallback).To(Equal(direct.Localization.IsFallback))
	})
})
