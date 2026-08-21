package domain

import "errors"

// Provider is the telematics vendor a tracking credential belongs to
// (BR-TP51). A closed enum, deliberately a small representative subset of
// V2's 35 vendors — the ones its live data actually uses most.
//
// V2's companion free-text `providerName` column is NOT carried. Its real
// values are visibly corrupted (`MixSite1`…`MixSite16`, `ctrack-32332`,
// `Autotrak51`) sitting beside a clean `trackingProvider` enum that records
// the same fact, so carrying both would import the corruption for nothing.
type Provider string

const (
	ProviderCartrack      Provider = "CARTRACK"
	ProviderMixTelematics Provider = "MIX_TELEMATICS"
	ProviderWebfleet      Provider = "WEBFLEET"
	ProviderCtrack        Provider = "CTRACK"
	ProviderNetstar       Provider = "NETSTAR"
)

// CredentialType is V2's real three-value enum, kept verbatim. All three
// carry weight in its live data (METADATA_ONLY 40, USERNAME_PASSWORD 34,
// API_KEY 15), so none is dead vocabulary.
type CredentialType string

const (
	CredentialTypeAPIKey           CredentialType = "API_KEY"
	CredentialTypeUsernamePassword CredentialType = "USERNAME_PASSWORD"
	CredentialTypeMetadataOnly     CredentialType = "METADATA_ONLY"
)

// TrackingCredential is the non-secret record of a configured telematics
// integration (BR-TP51).
//
// BR-TP52 is enforced by this type's *shape*: there is no field capable of
// holding credential material, and there must never be one. The payload
// lives only in the at-rest-encrypted `organizations-secrets` KV bucket, so
// a secret cannot reach Postgres, the event log, or any read path by
// accident — only by someone adding a field here, which a spec asserts
// against.
type TrackingCredential struct {
	Provider              Provider       `json:"provider"`
	CredentialType        CredentialType `json:"credentialType"`
	CredentialsConfigured bool           `json:"credentialsConfigured"`
}

var (
	// ErrTrackingCredentialRequiresTransporter — BR-TP51.
	ErrTrackingCredentialRequiresTransporter = errors.New("tracking credentials may only be configured for a Transporter")

	// ErrInvalidTrackingProvider — BR-TP51: provider is a closed enum.
	ErrInvalidTrackingProvider = errors.New("unknown tracking provider")

	// ErrInvalidCredentialType — BR-TP51: credentialType is a closed enum.
	ErrInvalidCredentialType = errors.New("credential type must be API_KEY, USERNAME_PASSWORD or METADATA_ONLY")
)

// AddTrackingCredential implements BR-TP51's guards. It takes no secret
// material by design — see TrackingCredential's doc comment.
func AddTrackingCredential(partnerType PartnerType, provider Provider, credentialType CredentialType) (TrackingCredential, error) {
	if partnerType != PartnerTypeTransporter {
		return TrackingCredential{}, ErrTrackingCredentialRequiresTransporter
	}
	if !validProviders[provider] {
		return TrackingCredential{}, ErrInvalidTrackingProvider
	}
	if !validCredentialTypes[credentialType] {
		return TrackingCredential{}, ErrInvalidCredentialType
	}
	return TrackingCredential{
		Provider:       provider,
		CredentialType: credentialType,
		// Configured is true at construction: this constructor is only
		// reached on the write path that also stores the payload (BR-TP53),
		// so a TrackingCredential that exists is one whose secret exists.
		CredentialsConfigured: true,
	}, nil
}

// TrackingCredentialSecretKey builds BR-TP52's KV key:
// {context}.transporter.{id}.trackingcreds.{provider}.
//
// This lives in the domain, not the KV adapter, for the same reason
// DocumentObjectName does (BR-TP42): the key format is a business rule —
// every token service-controlled, mirroring this repo's KV convention — not
// a transport detail. Keeping it here also means the command layer can build
// it without importing the adapter.
func TrackingCredentialSecretKey(contextKey, partnerID string, provider Provider) string {
	return contextKey + ".transporter." + partnerID + ".trackingcreds." + string(provider)
}

var validProviders = map[Provider]bool{
	ProviderCartrack: true, ProviderMixTelematics: true, ProviderWebfleet: true,
	ProviderCtrack: true, ProviderNetstar: true,
}

var validCredentialTypes = map[CredentialType]bool{
	CredentialTypeAPIKey: true, CredentialTypeUsernamePassword: true, CredentialTypeMetadataOnly: true,
}
