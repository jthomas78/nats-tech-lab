// Command callout is demo 07's NATS auth callout service.
//
// The NATS server runs in operator mode. Account AUTH carries the auth callout
// configuration in its JWT. When a client connects, the server publishes a
// signed authorization request on $SYS.REQ.USER.AUTH. This service:
//
//  1. decodes the request (a JWT signed by the server),
//  2. verifies the WorkOS access token in connect_opts.auth_token against the
//     WorkOS JWKS for our client ID, and checks it is for our organization,
//  3. maps the WorkOS role to NATS permissions,
//  4. issues a user JWT for the server-chosen user_nkey, signed by an ORDERS
//     signing key (operator mode: the user JWT's issuer picks the account),
//  5. wraps it in an authorization response signed by an AUTH signing key.
//
// It logs claims and public keys only. It never logs the token, the raw
// request (which contains the token) or a seed.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
	gjwt "github.com/golang-jwt/jwt/v5"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

const workosAPI = "https://api.workos.com"

// workosClaims is the part of a WorkOS AuthKit access token this service reads.
type workosClaims struct {
	gjwt.RegisteredClaims
	ClientID string   `json:"client_id"`
	OrgID    string   `json:"org_id"`
	Role     string   `json:"role"`
	Roles    []string `json:"roles"`
	SID      string   `json:"sid"`
}

// permissionsFor maps a WorkOS role to NATS permissions in ORDERS.
// Any other role, or no role, is refused.
func permissionsFor(role string) (jwt.Permissions, bool) {
	var p jwt.Permissions
	switch role {
	case "dispatcher":
		p.Pub.Allow.Add("orders.>")
		p.Sub.Allow.Add("orders.>", "_INBOX.>")
	case "viewer":
		p.Pub.Deny.Add(">")
		p.Sub.Allow.Add("orders.>")
	default:
		return p, false
	}
	return p, true
}

type service struct {
	clientID      string
	orgID         string // the one WorkOS organization allowed in
	issuers       []string
	jwks          keyfunc.Keyfunc
	authKP        nkeys.KeyPair // AUTH signing key: signs the response
	authAccount   string
	ordersKP      nkeys.KeyPair // ORDERS signing key: signs the user JWT
	ordersAccount string
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	clientID := mustEnv("D07_WORKOS_CLIENT_ID")
	s := &service{
		clientID: clientID,
		orgID:    mustEnv("D07_WORKOS_ORG_ID"),
		// WorkOS documents iss as https://api.workos.com; tokens issued for a
		// client can carry the client-scoped form. Both are WorkOS; the JWKS
		// below is the client-specific key set either way.
		issuers:       []string{workosAPI, workosAPI + "/user_management/" + clientID},
		authKP:        mustSeedFile("D07_AUTH_SIGNING_SEED"),
		authAccount:   mustEnv("D07_AUTH_ACCOUNT"),
		ordersKP:      mustSeedFile("D07_ORDERS_SIGNING_SEED"),
		ordersAccount: mustEnv("D07_ORDERS_ACCOUNT"),
	}

	jwksURL := workosAPI + "/sso/jwks/" + clientID
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Fail at start if the WorkOS key set cannot be fetched: a service that
	// cannot verify must not look ready.
	store, err := jwkset.NewStorageFromHTTP(jwksURL, jwkset.HTTPClientStorageOptions{
		Ctx:             ctx,
		RefreshInterval: time.Hour,
	})
	if err != nil {
		log.Fatalf("jwks     %s: %v", jwksURL, err)
	}
	keys, err := store.KeyReadAll(ctx)
	if err != nil || len(keys) == 0 {
		log.Fatalf("jwks     %s: no keys (%v)", jwksURL, err)
	}
	for _, k := range keys {
		m := k.Marshal()
		log.Printf("jwks     key kid=%s kty=%s alg=%s", m.KID, m.KTY, m.ALG)
	}
	s.jwks, err = keyfunc.New(keyfunc.Options{Ctx: ctx, Storage: store})
	if err != nil {
		log.Fatalf("jwks     %v", err)
	}

	authPub, _ := s.authKP.PublicKey()
	ordersPub, _ := s.ordersKP.PublicKey()
	log.Printf("start    jwks=%s", jwksURL)
	log.Printf("start    allowed org_id=%s", s.orgID)
	log.Printf("start    response signer=%s (AUTH %s)", authPub, s.authAccount)
	log.Printf("start    user JWT signer=%s (ORDERS %s)", ordersPub, s.ordersAccount)

	url := envOr("D07_URL", "nats://127.0.0.1:4822")
	nc, err := nats.Connect(url,
		nats.UserCredentials(mustEnv("D07_CALLOUT_CREDS")),
		nats.Name("d07-callout"),
	)
	if err != nil {
		log.Fatalf("connect  %s: %v", url, err)
	}
	defer nc.Drain()

	if _, err := nc.Subscribe("$SYS.REQ.USER.AUTH", s.handle); err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	log.Printf("ready    listening on $SYS.REQ.USER.AUTH at %s", url)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("stop")
}

func (s *service) handle(msg *nats.Msg) {
	req, err := jwt.DecodeAuthorizationRequestClaims(string(msg.Data))
	if err != nil {
		// No user_nkey to answer for. The server times out the client.
		log.Printf("bad      request did not decode: %v", err)
		return
	}
	ci := req.ClientInformation
	log.Printf("request  cid=%d user_nkey=%s name_tag=%q client_user=%s server=%s token=%s",
		ci.ID, req.UserNkey, ci.NameTag, ci.User, req.Server.ID, present(req.ConnectOptions.Token))

	claims, kid, err := s.verify(req.ConnectOptions.Token)
	if err != nil {
		s.respond(msg, req, "", "token rejected: "+err.Error())
		return
	}
	log.Printf("token    ok kid=%s sub=%s org=%s role=%q roles=%v sid=%s iss=%s iat=%s exp=%s",
		kid, claims.Subject, claims.OrgID, claims.Role, claims.Roles, claims.SID, claims.Issuer,
		claims.IssuedAt.UTC().Format(time.RFC3339), claims.ExpiresAt.UTC().Format(time.RFC3339))

	perms, ok := permissionsFor(claims.Role)
	if !ok {
		s.respond(msg, req, "", fmt.Sprintf("role %q has no NATS access", claims.Role))
		return
	}

	uc := jwt.NewUserClaims(req.UserNkey)
	uc.Name = claims.Subject
	uc.IssuerAccount = s.ordersAccount
	uc.Permissions = perms
	uc.Expires = claims.ExpiresAt.Unix() // NATS drops the client when the WorkOS token expires
	uc.Tags.Add("workos-role:" + claims.Role)
	userJWT, err := uc.Encode(s.ordersKP)
	if err != nil {
		s.respond(msg, req, "", "sign user JWT: "+err.Error())
		return
	}
	s.respond(msg, req, userJWT, "")
	log.Printf("allow    user_nkey=%s account=ORDERS role=%s workos_sub=%s pub=%v sub=%v expires=%s",
		req.UserNkey, claims.Role, claims.Subject, perms.Pub.Allow, perms.Sub.Allow,
		claims.ExpiresAt.UTC().Format(time.RFC3339))
}

// verify checks the WorkOS access token: signature against the JWKS,
// algorithm, expiry, issuer, and client ID when the token carries one.
// It returns the claims and the kid of the JWKS key that verified them.
func (s *service) verify(token string) (*workosClaims, string, error) {
	if token == "" {
		return nil, "", errors.New("no token in connect options")
	}
	var c workosClaims
	t, err := gjwt.ParseWithClaims(token, &c, s.jwks.Keyfunc,
		gjwt.WithValidMethods([]string{"RS256"}),
		gjwt.WithExpirationRequired(),
		gjwt.WithIssuedAt(),
	)
	if err != nil {
		return nil, "", err
	}
	kid, _ := t.Header["kid"].(string)
	if !slices.Contains(s.issuers, c.Issuer) {
		return nil, "", fmt.Errorf("issuer %q is not WorkOS for this client", c.Issuer)
	}
	if c.ClientID != "" && c.ClientID != s.clientID {
		return nil, "", fmt.Errorf("token is for client %q", c.ClientID)
	}
	if c.Subject == "" {
		return nil, "", errors.New("token has no sub")
	}
	// A role only means something inside our organization. A dispatcher of
	// another organization in the same environment is refused.
	if c.OrgID != s.orgID {
		return nil, "", fmt.Errorf("token org_id %q is not %q", c.OrgID, s.orgID)
	}
	return &c, kid, nil
}

// respond signs an authorization response with the AUTH signing key. Exactly
// one of userJWT and denyReason is set.
func (s *service) respond(msg *nats.Msg, req *jwt.AuthorizationRequestClaims, userJWT, denyReason string) {
	rc := jwt.NewAuthorizationResponseClaims(req.UserNkey)
	rc.Audience = req.Server.ID
	rc.IssuerAccount = s.authAccount
	rc.Jwt = userJWT
	rc.Error = denyReason
	out, err := rc.Encode(s.authKP)
	if err != nil {
		log.Printf("bad      sign response: %v", err)
		return
	}
	if denyReason != "" {
		log.Printf("deny     user_nkey=%s reason=%q", req.UserNkey, denyReason)
	}
	if err := msg.Respond([]byte(out)); err != nil {
		log.Printf("bad      respond: %v", err)
	}
}

func present(token string) string {
	if token == "" {
		return "absent"
	}
	return fmt.Sprintf("present(%d bytes)", len(token))
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("missing env %s", name)
	}
	return v
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// mustSeedFile reads a seed from the file named by env var name.
func mustSeedFile(name string) nkeys.KeyPair {
	path := mustEnv(name)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	kp, err := nkeys.FromSeed(bytes.TrimSpace(data))
	if err != nil {
		log.Fatalf("%s: not a seed file", name)
	}
	return kp
}
