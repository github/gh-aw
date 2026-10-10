package cli

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var mcpHTTPLog = logger.New("cli:mcp_server_http")

// sanitizeForLog removes newline and carriage return characters from user input
// to prevent log injection attacks where malicious users could forge log entries.
func sanitizeForLog(input string) string {
	// Remove both \n and \r to prevent log injection
	sanitized := strings.ReplaceAll(input, "\n", "")
	sanitized = strings.ReplaceAll(sanitized, "\r", "")
	return sanitized
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func mcpHTTPServerAddr(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func mcpHTTPServerDisplayURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func loggingHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code.
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Sanitize user-controlled input before logging to prevent log injection
		sanitizedPath := sanitizeForLog(r.URL.Path)

		// Log request details.
		mcpHTTPLog.Printf("[REQUEST] %s | %s | %s %s",
			start.Format(time.RFC3339),
			r.RemoteAddr,
			r.Method,
			sanitizedPath)

		// Call the actual handler.
		handler.ServeHTTP(wrapped, r)

		// Log response details.
		duration := time.Since(start)
		mcpHTTPLog.Printf("[RESPONSE] %s | %s | %s %s | Status: %d | Duration: %v",
			time.Now().Format(time.RFC3339),
			r.RemoteAddr,
			r.Method,
			sanitizedPath,
			wrapped.statusCode,
			duration)
	})
}

// runHTTPServer runs the MCP server with HTTP/SSE transport
func runHTTPServer(server *mcp.Server, port int) error {
	mcpLog.Printf("Creating HTTP server on port %d", port)

	// Create the streamable HTTP handler.
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		// Stateless mode is deliberately left disabled. Stateless servers opt into
		// the 2026-07-28 protocol revision but drop session affinity, which the
		// long-running logs/audit tools rely on for progress notifications over the
		// lifetime of a session. Staying stateful lets the SDK negotiate down to
		// 2025-11-25 for peers that still expect the initialize handshake.
		SessionTimeout:             2 * time.Hour, // Close idle sessions after 2 hours
		DisableLocalhostProtection: false,         // Keep the SDK's localhost Host-header checks enabled.
		Logger:                     logger.NewSlogLoggerWithHandler(mcpLog),
	})

	handlerWithLogging := loggingHandler(handler)

	// Bind to loopback only. Since v1.6.0, the SDK no longer enables
	// cross-origin protection by default, so binding to 127.0.0.1 (instead
	// of all interfaces) is the safest default for a local MCP server — it
	// prevents the server from being reachable on non-localhost interfaces.
	// The SDK's localhost Host-header checks stay enabled as a second layer.
	addr := mcpHTTPServerAddr(port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handlerWithLogging,
		ReadHeaderTimeout: MCPServerHTTPTimeout,
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Starting MCP server on "+mcpHTTPServerDisplayURL(port)))
	mcpLog.Printf("HTTP server listening on %s", addr)

	// Run the HTTP server
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		mcpLog.Printf("HTTP server failed: %v", err)
		return fmt.Errorf("HTTP server failed: %w", err)
	}

	return nil
}
