// Package errors provides middleware for sanitizing error responses.
package errors

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"regexp"
	"strings"
)

var (
	stackTracePattern = regexp.MustCompile(`(?m)^\s*at\s+.*$|^\s*goroutine\s+\d+.*$|^\s*\S+\.go:\d+.*$`)
	filePathPattern   = regexp.MustCompile(`(/[a-zA-Z0-9_\-./]+\.go:\d+)|([A-Z]:\\[a-zA-Z0-9_\-\\./]+\.go:\d+)`)
)

type SanitizeConfig struct {
	StripStackTraces bool
	StripFilePaths   bool
	ReplacementMsg   string
}

type sanitizeWriter struct {
	http.ResponseWriter
	config       SanitizeConfig
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

// Sanitize returns middleware that sanitizes error responses by removing sensitive information.
// By default, it strips both stack traces and file paths from 4xx/5xx responses.
func Sanitize(opts ...SanitizeOption) func(http.Handler) http.Handler {
	config := SanitizeConfig{
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

// SanitizeOption configures the Sanitize middleware.
type SanitizeOption func(*SanitizeConfig)

// WithStackTraces controls whether stack traces are stripped (default: true).
func WithStackTraces(strip bool) SanitizeOption {
	return func(c *SanitizeConfig) {
		c.StripStackTraces = strip
	}
}

// WithFilePaths controls whether file paths are stripped (default: true).
func WithFilePaths(strip bool) SanitizeOption {
	return func(c *SanitizeConfig) {
		c.StripFilePaths = strip
	}
}

// WithReplacementMessage sets the message to use when all content is stripped (default: "Internal Server Error").
func WithReplacementMessage(msg string) SanitizeOption {
	return func(c *SanitizeConfig) {
		c.ReplacementMsg = msg
	}
}
