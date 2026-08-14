package rest

import (
	"crypto/subtle"
	"net/http"
)

// BasicAuthUser is the fixed username every request authenticates as — only
// the password (secret) is a real credential. Mirrors accounts-service's
// own BasicAuthUser/BasicAuth exactly (accounts/middleware.go): a single
// shared secret, not per-operator accounts, since WorkOS-backed human auth
// is a later phase for the whole POC, not something worth solving twice.
const BasicAuthUser = "admin"

// BasicAuth wraps next so every request must present BasicAuthUser and
// secret via HTTP Basic Auth.
func BasicAuth(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(BasicAuthUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(secret)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="trading-partner-service"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// auditActor extracts a best-effort actor identity for BR-TP06's audit row —
// mirrors accounts-service's auditActor exactly: the basic-auth username,
// overridden by an optional X-Actor header, plus the request's source
// address. Neither is authenticated identity; both are placeholders until
// WorkOS-backed human auth lands.
func auditActor(r *http.Request) (actor, sourceIP string) {
	actor = BasicAuthUser
	if user, _, ok := r.BasicAuth(); ok && user != "" {
		actor = user
	}
	if xa := r.Header.Get("X-Actor"); xa != "" {
		actor = xa
	}
	return actor, r.RemoteAddr
}
