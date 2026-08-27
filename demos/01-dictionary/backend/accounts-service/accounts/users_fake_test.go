package accounts_test

// An in-memory accounts.UserRegistry for specs that need to observe the
// record-then-sign ordering BR-AC38 requires, without a Postgres container.

import (
	"context"
	"errors"
	"sync"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

type fakeUserRegistry struct {
	mu sync.Mutex
	// calls records the sequence of registry operations as
	// "record:<pub>" / "active:<pub>" — the ordering itself is the thing
	// under test, so it is captured rather than just the final state.
	calls       []string
	recorded    []accounts.NewUser
	recordErr   error
	activateErr error
}

func (f *fakeUserRegistry) RecordPendingUser(_ context.Context, u accounts.NewUser) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return f.recordErr
	}
	f.calls = append(f.calls, "record:"+u.PublicKey)
	f.recorded = append(f.recorded, u)
	return nil
}

func (f *fakeUserRegistry) MarkUserActive(_ context.Context, publicKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.activateErr != nil {
		return f.activateErr
	}
	f.calls = append(f.calls, "active:"+publicKey)
	return nil
}

func (f *fakeUserRegistry) snapshot() ([]string, []accounts.NewUser) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...), append([]accounts.NewUser(nil), f.recorded...)
}

var errRegistryDown = errors.New("registry unavailable")
