package cli

import (
	"testing"
)

func TestNewMCPInspectHTTPClient(t *testing.T) {
	headers := map[string]string{"X-Test": "value"}
	client := newMCPInspectHTTPClient(headers)

	if client.Timeout != MCPServerHTTPTimeout {
		t.Errorf("client timeout = %s, want %s", client.Timeout, MCPServerHTTPTimeout)
	}

	transport, ok := client.Transport.(*headerRoundTripper)
	if !ok {
		t.Fatalf("client transport has type %T, want *headerRoundTripper", client.Transport)
	}
	if transport.base == nil {
		t.Fatal("client transport has no base transport")
	}
	if got := transport.headers["X-Test"]; got != "value" {
		t.Errorf("transport header = %q, want %q", got, "value")
	}
}
