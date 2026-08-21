package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

// SecretStore is the sealed-payload port (BR-TP52). Put only — nothing on
// the api.* surface ever reads a payload back, so no Get is exposed here.
type SecretStore interface {
	Put(ctx context.Context, key string, payload []byte) error
}

// SecretStoreResolver resolves a tenant to its own sealed bucket, mirroring
// ObjectStoreResolver. Tenant isolation is the NATS account boundary, as it
// is for every stream and bucket in this repo.
type SecretStoreResolver interface {
	SecretStore(tenant string) (SecretStore, error)
}

// ProfileEventAppender is the slice of the profile orchestrator this handler
// needs (BR-TP55). Narrow on purpose: this command may append the credential
// event and nothing else on that aggregate.
type ProfileEventAppender interface {
	ConfigureTrackingCredential(ctx context.Context, contextKey, organizationID, provider, credentialType string) (profiledomain.State, error)
}

// ProfileEventAppenderResolver resolves a tenant to its own event store. The
// TRANSPORTER stream lives inside the tenant's NATS account, so — like
// SecretStore and DocumentStore above — there is one appender per tenant,
// not one per process.
type ProfileEventAppenderResolver interface {
	ProfileCommands(tenant string) (ProfileEventAppender, error)
}

// TrackingCredentialHandler implements BR-TP51-BR-TP55.
type TrackingCredentialHandler struct {
	partners domain.OrganizationRepository
	creds    domain.TrackingCredentialRepository
	secrets  SecretStoreResolver
	profiles ProfileEventAppenderResolver
}

func NewTrackingCredentialHandler(
	partners domain.OrganizationRepository,
	creds domain.TrackingCredentialRepository,
	secrets SecretStoreResolver,
	profiles ProfileEventAppenderResolver,
) *TrackingCredentialHandler {
	return &TrackingCredentialHandler{partners: partners, creds: creds, secrets: secrets, profiles: profiles}
}

// ConfigureTrackingCredential seals and stores the payload, then records the
// credential and appends BR-TP55's event.
//
// # BR-TP53: the write order is forced, not preferred
//
// Nothing spans the KV bucket, Postgres and the event log transactionally,
// and the failure modes are not symmetric — the same asymmetry BR-TP43
// records for document bytes:
//
//   - Event first, payload second: a failure leaves an **immutable** log
//     asserting a configured credential whose secret was never stored. An
//     event can be compensated but never retracted, so the log would
//     permanently claim something untrue.
//   - Payload first, event second: a failure leaves an unreferenced sealed
//     value in the bucket. Nothing reads it, it is opaque, and BR-TP54's
//     overwrite makes the next attempt at the same provider reclaim the key.
//
// So the payload goes first. The orphan is the acceptable outcome precisely
// because it is inert and self-correcting.
func (h *TrackingCredentialHandler) ConfigureTrackingCredential(
	ctx context.Context, partnerID, tenant string,
	provider domain.Provider, credentialType domain.CredentialType,
	payload []byte,
) (domain.TrackingCredential, error) {
	tp, err := h.partners.Get(ctx, partnerID)
	if err != nil {
		return domain.TrackingCredential{}, err
	}

	// BR-TP51's guards run before anything is stored — a rejected credential
	// must not leave a sealed payload behind for a record that never existed.
	cred, err := domain.AddTrackingCredential(tp.Type, provider, credentialType)
	if err != nil {
		return domain.TrackingCredential{}, err
	}

	store, err := h.secrets.SecretStore(tenant)
	if err != nil {
		return domain.TrackingCredential{}, err
	}

	// Step 1 (BR-TP53): the payload. Sealed inside Put — this handler never
	// sees ciphertext and never holds the key.
	if err := store.Put(ctx, domain.TrackingCredentialSecretKey(tp.Context, partnerID, provider), payload); err != nil {
		return domain.TrackingCredential{}, err
	}

	// Step 2: the non-secret record. BR-TP54 — an upsert, since
	// re-configuring a provider replaces it.
	if err := h.creds.UpsertTrackingCredential(ctx, partnerID, cred); err != nil {
		return domain.TrackingCredential{}, err
	}

	// Step 3 (BR-TP55): the event. Carries provider and credential type
	// only; `payload` is not in scope here and cannot be passed even by
	// accident, because ConfigureTrackingCredential takes no such parameter.
	appender, err := h.profiles.ProfileCommands(tenant)
	if err != nil {
		return domain.TrackingCredential{}, err
	}
	if _, err := appender.ConfigureTrackingCredential(ctx, tp.Context, partnerID, string(provider), string(credentialType)); err != nil {
		return domain.TrackingCredential{}, err
	}

	return cred, nil
}

func (h *TrackingCredentialHandler) ListTrackingCredentials(ctx context.Context, partnerID string) ([]domain.TrackingCredential, error) {
	return h.creds.ListTrackingCredentials(ctx, partnerID)
}
