//go:build !integration

package cli

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"
)

func TestTraceUpdateGitHubRequestCallbacks(t *testing.T) {
	ctx, report := traceUpdateGitHubRequest(context.Background(), "/repos/example/repo")
	trace := httptrace.ContextClientTrace(ctx)
	if trace == nil {
		t.Fatal("missing HTTP client trace")
	}
	trace.DNSStart(httptrace.DNSStartInfo{})
	trace.DNSDone(httptrace.DNSDoneInfo{})
	trace.ConnectStart("tcp", "localhost:443")
	trace.ConnectDone("tcp", "localhost:443", nil)
	trace.TLSHandshakeStart()
	trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
	trace.GotConn(httptrace.GotConnInfo{Reused: true})
	trace.GotFirstResponseByte()
	_, stderr := captureOutput(t, func() error {
		report()
		return nil
	})
	for _, text := range []string{
		"Network: HTTP /repos/example/repo", "connection reused: true",
		"DNS:", "connect:", "TLS:", "first byte:",
	} {
		if !strings.Contains(stderr, text) {
			t.Errorf("missing %q in %q", text, stderr)
		}
	}
}

func TestUpdateRequestTimingConnectTracking(t *testing.T) {
	timing := &updateRequestTiming{connectStarts: map[string]time.Time{
		"tcp|one": time.Now().Add(-time.Second),
		"tcp|two": time.Now(),
	}}
	timing.onConnectDone("tcp", "one", nil)
	if timing.connectTime < time.Second || len(timing.connectStarts) != 1 {
		t.Fatalf("unexpected completed connection state: %+v", timing)
	}
	before := timing.connectTime
	timing.onConnectDone("tcp", "missing", nil)
	if timing.connectTime != before || len(timing.connectStarts) != 1 {
		t.Fatal("an unmatched connection completion must not change timing state")
	}
}
