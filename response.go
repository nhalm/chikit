package chikit

import "net/http"

// SetError sets an error response in the request context.
// If wrapper middleware is not present (state is nil), this is a no-op.
// Use HasState() to check if wrapper middleware is active.
func SetError(r *http.Request, err *APIError) {
	state := getState(r.Context())
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.err = err
}

// SetResponse sets a success response in the request context.
// If wrapper middleware is not present (state is nil), this is a no-op.
// Use HasState() to check if wrapper middleware is active.
func SetResponse(r *http.Request, status int, body any) {
	state := getState(r.Context())
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.status = status
	state.body = body
}

// SetHeader sets a response header in the request context.
// If wrapper middleware is not present (state is nil), this is a no-op.
// Use HasState() to check if wrapper middleware is active.
func SetHeader(r *http.Request, key, value string) {
	state := getState(r.Context())
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.headers == nil {
		state.headers = make(http.Header)
	}
	state.headers.Set(key, value)
}

// AddHeader adds a response header value in the request context.
// If wrapper middleware is not present (state is nil), this is a no-op.
// Use HasState() to check if wrapper middleware is active.
func AddHeader(r *http.Request, key, value string) {
	state := getState(r.Context())
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.headers == nil {
		state.headers = make(http.Header)
	}
	state.headers.Add(key, value)
}
