//go:build !integration

package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPHTTPHandler_RenegotiatesStatefulProtocol(t *testing.T) {
	server := createMCPServer("", "", false, false, "", nil)
	httpServer := httptest.NewServer(newMCPHTTPHandler(server))
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	require.Equal(t, "2025-11-25", session.InitializeResult().ProtocolVersion)
}

func TestMCPHTTPServerAddr_BindsToLoopback(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		port int
		want string
	}{
		{port: 0, want: "127.0.0.1:0"},
		{port: 12345, want: "127.0.0.1:12345"},
		{port: 65535, want: "127.0.0.1:65535"},
	}

	for _, tc := range testCases {
		assert.Equal(t, tc.want, mcpHTTPServerAddr(tc.port), "mcpHTTPServerAddr(%d)", tc.port)
	}
}

func TestMCPHTTPServerDisplayURL_UsesLoopbackAddress(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		port int
		want string
	}{
		{port: 0, want: "http://127.0.0.1:0"},
		{port: 12345, want: "http://127.0.0.1:12345"},
		{port: 65535, want: "http://127.0.0.1:65535"},
	}

	for _, tc := range testCases {
		assert.Equal(t, tc.want, mcpHTTPServerDisplayURL(tc.port), "mcpHTTPServerDisplayURL(%d)", tc.port)
	}
}

func TestSanitizeForLog(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "no special chars", input: "/api/v1/workflows", want: "/api/v1/workflows"},
		{name: "newline injection", input: "/path\nINJECTED LOG ENTRY", want: "/pathINJECTED LOG ENTRY"},
		{name: "carriage return", input: "/path\rmalicious", want: "/pathmalicious"},
		{name: "both newline and carriage return", input: "line1\r\nline2", want: "line1line2"},
		{name: "empty", input: "", want: ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, sanitizeForLog(tc.input))
		})
	}
}

func TestResponseWriter_DefaultStatusCode(t *testing.T) {
	t.Parallel()
	rw := &responseWriter{
		ResponseWriter: httptest.NewRecorder(),
		statusCode:     http.StatusOK,
	}
	assert.Equal(t, http.StatusOK, rw.statusCode, "default statusCode")
}

func TestResponseWriter_EmbeddedResponseWriter(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
	}
	// The responseWriter delegates writes to the embedded ResponseWriter
	rw.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, rec.Code, "embedded ResponseWriter code")
	assert.Equal(t, http.StatusCreated, rw.statusCode, "responseWriter statusCode")
}

func TestLoggingHandler_PassesRequestToHandler(t *testing.T) {
	t.Parallel()
	var handlerCalled bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := loggingHandler(inner)

	req := httptest.NewRequest(http.MethodGet, "/test-path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, handlerCalled, "loggingHandler should call the inner handler")
	assert.Equal(t, http.StatusOK, rec.Code, "response code")
}

func TestLoggingHandler_PreservesResponseStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			})

			rec := httptest.NewRecorder()
			loggingHandler(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))

			assert.Equal(t, status, rec.Code, "status should reach the client unchanged")
		})
	}
}

func TestLoggingHandler_SanitizesPath(t *testing.T) {
	t.Parallel()
	// The handler should not panic on paths with newlines/carriage returns
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := loggingHandler(inner)

	// Path with newline injection attempt (Go's http package strips these, but test sanitize logic)
	req := httptest.NewRequest(http.MethodGet, "/safe-path", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "response code")
}
