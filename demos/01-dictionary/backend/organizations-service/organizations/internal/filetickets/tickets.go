// Package filetickets implements BR-TP41 — the capability tokens that
// authorize document byte transfer over this service's HTTP ingress.
//
// Why this exists at all. Every other caller of this service authenticates by
// *connecting to a NATS account*: tenancy is the account boundary, enforced
// server-side, and `internal/browserrpc` never reads an identity from a
// request body (see its package doc). The HTTP ingress 38c-ii introduces is
// the one path that sits outside that boundary — and the repo has no JWT
// verification anywhere to reuse (accounts-service only *mints* NATS
// credentials; nothing validates one). Rather than invent a second
// authentication system for two byte-shovelling routes, the authoritative
// decision stays where it already works: the browser asks for a ticket over
// its authenticated per-tenant NATS connection, and HTTP only honours a
// capability this service minted moments earlier.
//
// A ticket is therefore not a credential the client holds — it is a
// single-use, short-lived grant naming exactly one transfer. Nothing about
// the tenant, context or document is taken from the HTTP request itself; all
// of it is read back off the redeemed ticket, so a ticket for tenant A can
// never be spent against tenant B's bucket no matter what the request says.
package filetickets

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Direction distinguishes an upload grant from a download grant. They are
// deliberately not interchangeable: read access to a compliance document is a
// weaker permission than the write that creates one, and a single Direction
// field is what stops a download ticket being spent as an upload.
type Direction string

const (
	DirectionUpload   Direction = "upload"
	DirectionDownload Direction = "download"
)

// DefaultTTL is how long a minted ticket stays redeemable. Short on purpose:
// the browser mints one immediately before it starts the transfer, so this
// only has to cover a round-trip plus the user's own upload time, not a
// session. It bounds how long a leaked ticket is worth anything.
const DefaultTTL = 2 * time.Minute

var (
	// ErrUnknownTicket covers never-existed, already-redeemed and
	// swept-after-expiry alike. They are one error on purpose: telling a
	// caller which of the three it hit tells them whether a token was ever
	// valid, and that is not information an unauthenticated endpoint should
	// hand out.
	ErrUnknownTicket = errors.New("unknown or already-redeemed file ticket")

	// ErrWrongDirection — the ticket exists but grants the other operation.
	ErrWrongDirection = errors.New("file ticket does not grant this operation")
)

// Grant is what a redeemed ticket authorizes. Every field is trusted because
// every field was set by this service from an authenticated NATS request, not
// copied from the HTTP call that redeems it.
type Grant struct {
	Tenant     string
	Context    string
	PartnerID  string
	DocumentID string
	Direction  Direction
	// Actor and ActorSourceIP travel with the ticket rather than being read
	// from the HTTP upload that spends it, for exactly the reason Tenant does
	// (BR-TP41): by the time the ingress sees a ticket, every identity
	// decision has already been made on the NATS side. An upload's event has
	// to name someone, and the only trustworthy moment to capture that is at
	// mint time.
	Actor         string
	ActorSourceIP string
}

type entry struct {
	grant     Grant
	expiresAt time.Time
}

// Store holds live tickets in memory.
//
// In-memory is a deliberate, bounded choice: a ticket's whole lifetime is one
// browser interaction, so losing every outstanding ticket on restart costs a
// user one retry — nothing durable is being tracked. Persisting them would
// mean a second store to keep consistent for no gain. (A multi-replica
// deployment would need one, since a ticket minted by one replica is unknown
// to another; this lab runs a single replica per tenant connection.)
type Store struct {
	mu  sync.Mutex
	ttl time.Duration
	// now is injectable so the expiry rule is testable without sleeping.
	now      func() time.Time
	byToken  map[string]entry
	newToken func() (string, error)
}

// NewStore returns a Store with the given TTL, or DefaultTTL if ttl <= 0.
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Store{
		ttl:      ttl,
		now:      time.Now,
		byToken:  map[string]entry{},
		newToken: randomToken,
	}
}

// Mint issues a ticket for the given grant and returns its opaque token.
func (s *Store) Mint(g Grant) (string, error) {
	token, err := s.newToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Sweep on write. There is no background goroutine to own and shut down,
	// and mint traffic is the only thing that grows this map, so the sweep is
	// naturally paced by the growth it has to bound.
	s.sweepLocked()
	s.byToken[token] = entry{grant: g, expiresAt: s.now().Add(s.ttl)}
	return token, nil
}

// Redeem consumes a ticket and returns what it grants. A token is valid
// exactly once: it is removed whether or not the direction matched, so a
// mismatched attempt cannot be retried against the other route.
func (s *Store) Redeem(token string, dir Direction) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.byToken[token]
	if !ok {
		return Grant{}, ErrUnknownTicket
	}
	delete(s.byToken, token)
	if !s.now().Before(e.expiresAt) {
		return Grant{}, ErrUnknownTicket
	}
	if e.grant.Direction != dir {
		return Grant{}, ErrWrongDirection
	}
	return e.grant, nil
}

func (s *Store) sweepLocked() {
	now := s.now()
	for token, e := range s.byToken {
		if !now.Before(e.expiresAt) {
			delete(s.byToken, token)
		}
	}
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
