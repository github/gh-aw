//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/types"
)

// TestHeaderRoundTripper tests the custom RoundTripper that adds headers
func TestHeaderRoundTripper(t *testing.T) {
	tests := []struct {
		name            string
		headers         map[string]string
		expectedHeaders map[string]string
	}{
		{
			name: "single header",
			headers: map[string]string{
				"Authorization": "Bearer test-token",
			},
			expectedHeaders: map[string]string{
				"Authorization": "Bearer test-token",
			},
		},
		{
			name: "multiple headers",
			headers: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Custom":      "custom-value",
			},
			expectedHeaders: map[string]string{
				"Authorization": "Bearer test-token",
				"X-Custom":      "custom-value",
			},
		},
		{
			name:            "no headers",
			headers:         map[string]string{},
			expectedHeaders: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server that captures headers
			capturedHeaders := make(map[string]string)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Capture headers
				for key := range tt.expectedHeaders {
					capturedHeaders[key] = r.Header.Get(key)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			// Create HTTP client with custom round tripper
			client := &http.Client{
				Transport: &headerRoundTripper{
					base:    http.DefaultTransport,
					headers: tt.headers,
				},
			}

			// Make a request
			req, err := http.NewRequest("GET", server.URL, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer resp.Body.Close()

			// Verify headers were sent
			for key, expectedValue := range tt.expectedHeaders {
				if capturedHeaders[key] != expectedValue {
					t.Errorf("Header %s: expected %q, got %q", key, expectedValue, capturedHeaders[key])
				}
			}
		})
	}
}

// TestConnectHTTPMCPServer_WithHeaders tests HTTP MCP server connection with custom headers
func TestConnectHTTPMCPServer_WithHeaders(t *testing.T) {
	// Track whether headers were received
	receivedHeaders := make(map[string]string)

	// Create a mock MCP server that captures headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture custom headers
		if auth := r.Header.Get("Authorization"); auth != "" {
			receivedHeaders["Authorization"] = auth
		}
		if custom := r.Header.Get("X-Custom-Header"); custom != "" {
			receivedHeaders["X-Custom-Header"] = custom
		}

		// Return a minimal MCP response based on the request
		w.Header().Set("Content-Type", "application/json")

		// For this test, we'll just return a basic response
		// Real MCP servers would need proper initialize/initialized flow
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo": map[string]any{
					"name":    "test-server",
					"version": "1.0.0",
				},
			},
		}

		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
		URL: server.URL,
		Headers: map[string]string{
			"Authorization":   "Bearer test-token-123",
			"X-Custom-Header": "custom-value",
		}}, Name: "test-http-server",
	}

	ctx := context.Background()

	// Note: This test will attempt to connect but may fail due to MCP protocol requirements
	// The main goal is to verify headers are being sent
	_, err := connectHTTPMCPServer(ctx, config, false)

	// We expect an error because our mock server doesn't implement full MCP protocol
	// But we can verify that headers were sent
	if len(receivedHeaders) == 0 {
		t.Error("Expected headers to be sent to server, but none were received")
	}

	// Verify specific headers were received
	if receivedHeaders["Authorization"] != "Bearer test-token-123" {
		t.Errorf("Authorization header: expected %q, got %q",
			"Bearer test-token-123", receivedHeaders["Authorization"])
	}

	if receivedHeaders["X-Custom-Header"] != "custom-value" {
		t.Errorf("X-Custom-Header: expected %q, got %q",
			"custom-value", receivedHeaders["X-Custom-Header"])
	}

	// The error is expected since we don't have a full MCP implementation
	if err == nil {
		t.Log("Connection succeeded (unexpected but acceptable)")
	} else {
		// Verify it's a connection/protocol error, not a missing headers error
		errStr := err.Error()
		if strings.Contains(errStr, "header") && strings.Contains(errStr, "missing") {
			t.Errorf("Error suggests missing headers: %v", err)
		}
		t.Logf("Expected connection error due to mock server: %v", err)
	}
}

// TestConnectHTTPMCPServer_NoHeaders tests that connection works without headers
func TestConnectHTTPMCPServer_NoHeaders(t *testing.T) {
	requestCount := 0

	// Create a mock MCP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")

		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo": map[string]any{
					"name":    "test-server",
					"version": "1.0.0",
				},
			},
		}

		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
		URL:     server.URL,
		Headers: map[string]string{}}, Name: "test-http-server-no-headers",

	// Empty headers
	}

	ctx := context.Background()

	// Attempt connection - we expect it to fail due to mock server limitations
	// but it should not panic or error due to nil/empty headers
	_, err := connectHTTPMCPServer(ctx, config, false)

	// Verify at least one request was made (headers didn't prevent connection attempt)
	if requestCount == 0 {
		t.Error("Expected at least one request to be made to the server")
	}

	// Error is expected due to mock server, but not a nil pointer error
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "nil pointer") || strings.Contains(errStr, "panic") {
			t.Errorf("Unexpected nil pointer or panic error: %v", err)
		}
		t.Logf("Expected error due to mock server: %v", err)
	}
}

// TestConnectHTTPMCPServer_NilHeaders tests that nil headers don't cause issues
func TestConnectHTTPMCPServer_NilHeaders(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer server.Close()

	config := parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
		URL:     server.URL,
		Headers: nil}, Name: "test-http-server-nil-headers",

	// Nil headers
	}

	ctx := context.Background()

	// This should not panic
	_, err := connectHTTPMCPServer(ctx, config, false)

	if requestCount == 0 {
		t.Error("Expected at least one request to be made to the server")
	}

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "nil pointer") || strings.Contains(errStr, "panic") {
			t.Errorf("Unexpected nil pointer or panic error: %v", err)
		}
	}
}
