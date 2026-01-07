// Package chikit provides state management for context-based response handling.
package chikit

import (
	"context"
	"net/http"
	"sync"
)

type stateContextKey string

const stateKey stateContextKey = "chikit_state"

// State holds the response state for a request.
type State struct {
	mu      sync.Mutex
	err     *APIError
	status  int
	body    any
	headers http.Header
	written bool
	frozen  bool
}

// stateSnapshot holds a frozen copy of state for safe reading after freeze.
type stateSnapshot struct {
	err     *APIError
	status  int
	headers http.Header
}

// markWritten attempts to mark the state as written and frozen.
// Returns true if this call successfully marked it (first caller wins).
// Returns false if already written (second caller should not write).
// After this call, all mutations via SetError/SetResponse/SetHeader become no-ops.
func (s *State) markWritten() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.written {
		return false
	}
	s.written = true
	s.frozen = true
	return true
}

// snapshot returns a frozen copy of the current state for safe reading.
// Must be called while holding the mutex or after state is frozen.
func (s *State) snapshot() stateSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return stateSnapshot{
		err:     s.err,
		status:  s.status,
		headers: s.headers,
	}
}

// HasState returns true if wrapper state exists in the context.
func HasState(ctx context.Context) bool {
	return getState(ctx) != nil
}

func getState(ctx context.Context) *State {
	state, _ := ctx.Value(stateKey).(*State)
	return state
}
