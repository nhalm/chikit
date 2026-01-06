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
}

// HasState returns true if wrapper state exists in the context.
func HasState(ctx context.Context) bool {
	return getState(ctx) != nil
}

func getState(ctx context.Context) *State {
	state, _ := ctx.Value(stateKey).(*State)
	return state
}
