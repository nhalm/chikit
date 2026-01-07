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
}

// markWritten attempts to mark the state as written.
// Returns true if this call successfully marked it (first caller wins).
// Returns false if already written (second caller should not write).
func (s *State) markWritten() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.written {
		return false
	}
	s.written = true
	return true
}

// HasState returns true if wrapper state exists in the context.
func HasState(ctx context.Context) bool {
	return getState(ctx) != nil
}

func getState(ctx context.Context) *State {
	state, _ := ctx.Value(stateKey).(*State)
	return state
}
