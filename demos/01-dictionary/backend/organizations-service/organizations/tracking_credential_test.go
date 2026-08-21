package organizations_test

import (
	"errors"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

// Specs for BR-TP51 (shape) and BR-TP52's negative guarantee at the domain
// layer. The KV write ordering (BR-TP53), overwrite semantics (BR-TP54) and
// the availability computation (BR-TP55) are specced alongside their own
// units — see tracking_credential_secret_test.go and the profile suite.
var _ = Describe("TrackingCredential Rules", func() {
	Context("BR-TP51: a tracking credential may only belong to a Transporter", func() {
		It("allows a Transporter to configure a credential", func() {
			cred, err := domain.AddTrackingCredential(domain.PartnerTypeTransporter,
				domain.ProviderCartrack, domain.CredentialTypeAPIKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(cred.Provider).To(Equal(domain.ProviderCartrack))
			Expect(cred.CredentialType).To(Equal(domain.CredentialTypeAPIKey))
			Expect(cred.CredentialsConfigured).To(BeTrue())
		})

		It("rejects a tracking credential for a Shipper", func() {
			_, err := domain.AddTrackingCredential(domain.PartnerTypeShipper,
				domain.ProviderCartrack, domain.CredentialTypeAPIKey)
			Expect(errors.Is(err, domain.ErrTrackingCredentialRequiresTransporter)).To(BeTrue())
		})
	})

	Context("BR-TP51: provider and credentialType are closed vocabularies", func() {
		It("accepts each of V2's three real credential types", func() {
			// All three carry real weight in V2's live data (METADATA_ONLY 40,
			// USERNAME_PASSWORD 34, API_KEY 15), which is why the enum was
			// kept rather than reduced.
			for _, ct := range []domain.CredentialType{
				domain.CredentialTypeAPIKey,
				domain.CredentialTypeUsernamePassword,
				domain.CredentialTypeMetadataOnly,
			} {
				_, err := domain.AddTrackingCredential(domain.PartnerTypeTransporter, domain.ProviderMixTelematics, ct)
				Expect(err).NotTo(HaveOccurred(), string(ct))
			}
		})

		It("rejects an unknown credential type", func() {
			_, err := domain.AddTrackingCredential(domain.PartnerTypeTransporter,
				domain.ProviderCartrack, domain.CredentialType("PLAINTEXT"))
			Expect(errors.Is(err, domain.ErrInvalidCredentialType)).To(BeTrue())
		})

		It("rejects an unknown provider", func() {
			// V2's free-text providerName column is deliberately not carried:
			// its live values are corrupted (MixSite1..MixSite16,
			// ctrack-32332, Autotrak51) beside a clean enum holding the same
			// fact. Only the enum exists here.
			_, err := domain.AddTrackingCredential(domain.PartnerTypeTransporter,
				domain.Provider("MixSite14"), domain.CredentialTypeAPIKey)
			Expect(errors.Is(err, domain.ErrInvalidTrackingProvider)).To(BeTrue())
		})
	})

	Context("BR-TP52: the domain type cannot carry a secret", func() {
		It("has no field capable of holding credential material", func() {
			// Asserted structurally rather than by inspecting a value: the
			// guarantee is that no future edit can put a secret on the
			// aggregate's type at all. If a field is added here, this fails
			// and the author has to justify it against BR-TP52.
			cred, err := domain.AddTrackingCredential(domain.PartnerTypeTransporter,
				domain.ProviderWebfleet, domain.CredentialTypeUsernamePassword)
			Expect(err).NotTo(HaveOccurred())

			Expect(fieldNames(cred)).To(ConsistOf("Provider", "CredentialType", "CredentialsConfigured"),
				"BR-TP52: TrackingCredential must carry no secret-bearing field")
		})
	})
})

// fieldNames reflects over a struct's exported field names.
func fieldNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if f := t.Field(i); f.IsExported() {
			names = append(names, f.Name)
		}
	}
	return names
}
