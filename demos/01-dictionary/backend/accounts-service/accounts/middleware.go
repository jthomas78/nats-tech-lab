package accounts

import (
	"crypto/subtle"
	"net/http"
)

// BasicAuthUser is the fixed username every request authenticates as — only
// the password (ACCOUNTS_AUTH_SECRET) is a real credential. A single shared
// secret, not per-operator accounts: WorkOS-backed human auth is deferred to
// a later phase (see .claude/memory/accounts_service_plan.md); this is
// enough to keep the provisioning API from being wide open on the network
// during this phase.
const BasicAuthUser = "admin"

// BasicAuth wraps next so every request must present BasicAuthUser and
// secret via HTTP Basic Auth. Constant-time comparison avoids leaking the
// secret's length/prefix through response timing.
func BasicAuth(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(BasicAuthUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(secret)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="accounts-service"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
