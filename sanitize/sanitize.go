// Package sanitize provides middleware for sanitizing error responses.
//
// SECURITY: This middleware prevents leaking sensitive internal information in error
// responses by removing stack traces, file paths, and other implementation details.
// Critical for production deployments to avoid exposing internal application structure,
// source code locations, or debug information that could aid attackers.
//
// The middleware buffers error responses (4xx/5xx status codes) and applies
// configurable sanitization rules before sending to the client. Success responses
// (2xx/3xx) pass through without buffering for optimal performance.
//
// Basic usage (strips stack traces and file paths):
//
//	r.Use(sanitize.New())
//
// Custom configuration:
//
//	r.Use(sanitize.New(
//		sanitize.WithStackTraces(true),  // Strip stack traces
//		sanitize.WithFilePaths(true),     // Strip file paths
//		sanitize.WithReplacementMessage("An error occurred"),
//	))
//
// Example transformations:
//   - Before: "panic: runtime error at /app/internal/handler.go:42"
//   - After:  "Internal Server Error"
package sanitize

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"regexp"
	"strings"
)

var (
	// stackTracePattern matches common stack trace formats from Go panics and error libraries
	stackTracePattern = regexp.MustCompile(`(?m)^\s*at\s+.*$|^\s*goroutine\s+\d+.*$|^\s*\S+\.go:\d+.*$`)

	// filePathPattern matches absolute file paths (Unix and Windows) with line numbers
	filePathPattern = regexp.MustCompile(`(/[a-zA-Z0-9_\-./]+\.go:\d+)|([A-Z]:\\[a-zA-Z0-9_\-\\./]+\.go:\d+)`)
)

// Config configures the sanitization middleware.
type Config struct {
	// StripStackTraces removes stack trace lines from error responses (default: true)
	StripStackTraces bool

	// StripFilePaths removes file paths and line numbers from error responses (default: true)
	StripFilePaths bool

	// ReplacementMsg is shown when all content is stripped (default: "Internal Server Error")
	ReplacementMsg string
}

type sanitizeWriter struct {
	http.ResponseWriter
	config       Config
	buf          *bytes.Buffer
	statusCode   int
	wroteHeader  bool
	shouldBuffer bool
}

func (sw *sanitizeWriter) WriteHeader(code int) {
	if sw.wroteHeader {
		return
	}
	sw.statusCode = code
	sw.wroteHeader = true
	sw.shouldBuffer = code >= 400
	if !sw.shouldBuffer {
		sw.ResponseWriter.WriteHeader(code)
	}
}

func (sw *sanitizeWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}

	if !sw.shouldBuffer {
		return sw.ResponseWriter.Write(b)
	}

	return sw.buf.Write(b)
}

func (sw *sanitizeWriter) Flush() {
	if !sw.shouldBuffer {
		if f, ok := sw.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	body := sw.buf.String()

	if sw.config.StripStackTraces {
		body = stackTracePattern.ReplaceAllString(body, "")
	}

	if sw.config.StripFilePaths {
		body = filePathPattern.ReplaceAllString(body, sw.config.ReplacementMsg)
	}

	body = strings.TrimSpace(body)
	if body == "" && sw.config.ReplacementMsg != "" {
		body = sw.config.ReplacementMsg
	}

	sw.ResponseWriter.WriteHeader(sw.statusCode)
	sw.ResponseWriter.Write([]byte(body))
}

func (sw *sanitizeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return sw.ResponseWriter.(http.Hijacker).Hijack()
}

// New returns middleware that sanitizes error responses by removing sensitive information.
// Only error responses (4xx/5xx status codes) are processed; success responses pass through
// unchanged for performance. The middleware buffers error responses to apply sanitization
// before sending to the client.
//
// SECURITY IMPLICATIONS:
//   - Prevents exposure of internal file structure and source code locations
//   - Removes stack traces that could reveal application logic and dependencies
//   - Protects against information disclosure vulnerabilities
//   - Ensures consistent, safe error messages for clients
//
// By default, strips both stack traces and file paths from error responses.
// Use options to customize behavior.
//
// Example:
//
//	r.Use(sanitize.New())
//
// With custom settings:
//
//	r.Use(sanitize.New(
//		sanitize.WithStackTraces(false),  // Keep stack traces (dev only!)
//		sanitize.WithReplacementMessage("Service unavailable"),
//	))
func New(opts ...Option) func(http.Handler) http.Handler {
	config := Config{
		StripStackTraces: true,
		StripFilePaths:   true,
		ReplacementMsg:   "Internal Server Error",
	}

	for _, opt := range opts {
		opt(&config)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &sanitizeWriter{
				ResponseWriter: w,
				config:         config,
				buf:            &bytes.Buffer{},
				statusCode:     http.StatusOK,
			}

			defer sw.Flush()
			next.ServeHTTP(sw, r)
		})
	}
}

// Option configures the sanitization middleware.
type Option func(*Config)

// WithStackTraces controls whether stack traces are stripped (default: true).
// Set to false only in development environments where debugging information is needed.
// NEVER disable in production.
func WithStackTraces(strip bool) Option {
	return func(c *Config) {
		c.StripStackTraces = strip
	}
}

// WithFilePaths controls whether file paths are stripped (default: true).
// Set to false only in development environments where debugging information is needed.
// NEVER disable in production.
func WithFilePaths(strip bool) Option {
	return func(c *Config) {
		c.StripFilePaths = strip
	}
}

// WithReplacementMessage sets the message to use when all content is stripped.
// This message is shown when sanitization removes all error content, providing
// a safe, generic error message to clients. Default: "Internal Server Error".
func WithReplacementMessage(msg string) Option {
	return func(c *Config) {
		c.ReplacementMsg = msg
	}
}
