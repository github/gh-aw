package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
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

func newMCPHTTPHandler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
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
func runHTTPServer(ctx context.Context, server *mcp.Server, port int) error {
	mcpLog.Printf("Creating HTTP server on port %d", port)

	// Create the streamable HTTP handler.
	handler := newMCPHTTPHandler(server)

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
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Starting MCP server on "+mcpHTTPServerDisplayURL(port)))
	mcpLog.Printf("HTTP server listening on %s", addr)

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("HTTP server panicked: %v", r)
			}
		}()
		done <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		// The request contexts are canceled above, including active SSE streams.
		// Wait for handlers to return before the command cleans up its reports.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err := httpServer.Shutdown(shutdownCtx)
		if err != nil {
			err = errors.Join(err, httpServer.Close())
		}
		if serveErr := <-done; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
		return err
	}
}
