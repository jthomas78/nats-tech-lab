package announcer

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
)

const releaseStateSchemaVersion = 1

type releaseAction string

const (
	releaseAnnounce   releaseAction = "announce"
	releaseUnregister releaseAction = "unregister"
)

type releaseState struct {
	SchemaVersion int           `json:"schemaVersion"`
	Plugin        string        `json:"plugin"`
	Release       int64         `json:"release"`
	Action        releaseAction `json:"action"`
}

// releaseStore is the one publisher-owned sequence used by every announcer
// entry point. State is installed before a command is sent, so a timeout or
// crash retries an action rather than inventing a newer one.
type releaseStore struct {
	mu       sync.Mutex
	path     string
	pluginID string
	recovery int64
}

func newReleaseStore(path, pluginID string, recovery int64) *releaseStore {
	return &releaseStore{path: path, pluginID: pluginID, recovery: recovery}
}

func (s *releaseStore) PrepareAnnounce() (release int64, fresh bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists, err := s.load()
	if err != nil {
		return 0, false, err
	}
	if !exists {
		release = 1
		if s.recovery > 0 {
			release = s.recovery
		}
		state = releaseState{SchemaVersion: releaseStateSchemaVersion, Plugin: s.pluginID, Release: release, Action: releaseAnnounce}
		if err := persistReleaseState(s.path, state); err != nil {
			return 0, false, err
		}
		return release, true, nil
	}
	if state.Action == releaseAnnounce {
		return state.Release, false, nil
	}
	if state.Release == math.MaxInt64 {
		return 0, false, errors.New("publisher release counter exhausted")
	}
	state.Release++
	state.Action = releaseAnnounce
	if err := persistReleaseState(s.path, state); err != nil {
		return 0, false, err
	}
	return state.Release, false, nil
}

func (s *releaseStore) PrepareUnregister() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists, err := s.load()
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, errors.New("publisher release state does not contain an announcement")
	}
	if state.Action == releaseUnregister {
		return state.Release, nil
	}
	if state.Release == math.MaxInt64 {
		return 0, errors.New("publisher release counter exhausted")
	}
	state.Release++
	state.Action = releaseUnregister
	if err := persistReleaseState(s.path, state); err != nil {
		return 0, err
	}
	return state.Release, nil
}

func (s *releaseStore) load() (releaseState, bool, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return releaseState{}, false, nil
	}
	if err != nil {
		return releaseState{}, false, fmt.Errorf("read release state: %w", err)
	}
	var state releaseState
	if err := json.Unmarshal(raw, &state); err != nil {
		return releaseState{}, false, fmt.Errorf("decode release state: %w", err)
	}
	if state.SchemaVersion != releaseStateSchemaVersion || state.Plugin != s.pluginID || state.Release <= 0 {
		return releaseState{}, false, errors.New("release state does not match this plugin or schema")
	}
	if state.Action != releaseAnnounce && state.Action != releaseUnregister {
		return releaseState{}, false, errors.New("release state has an unknown action")
	}
	return state, true, nil
}

func persistReleaseState(path string, state releaseState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create release state directory: %w", err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode release state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".release-*")
	if err != nil {
		return fmt.Errorf("create temporary release state: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) //nolint:errcheck
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("protect release state: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("write release state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("sync release state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close release state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install release state: %w", err)
	}
	return nil
}
