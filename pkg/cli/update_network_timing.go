package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http/httptrace"
	"os"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/console"
)

func reportUpdateNetworkOperation(operation string) func(error) {
	fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Network: starting "+operation))
	start := time.Now()
	return func(err error) {
		status := "succeeded"
		if err != nil {
			status = "failed"
		}
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Network: %s took %s (%s)", operation, time.Since(start).Round(time.Millisecond), status)))
	}
}

func traceUpdateGitHubRequest(ctx context.Context, endpoint string) (context.Context, func()) {
	timing := &updateRequestTiming{start: time.Now(), connectStarts: make(map[string]time.Time)}
	trace := &httptrace.ClientTrace{
		DNSStart:             timing.onDNSStart,
		DNSDone:              timing.onDNSDone,
		ConnectStart:         timing.onConnectStart,
		ConnectDone:          timing.onConnectDone,
		TLSHandshakeStart:    timing.onTLSHandshakeStart,
		TLSHandshakeDone:     timing.onTLSHandshakeDone,
		GotConn:              timing.onGotConn,
		GotFirstResponseByte: timing.onGotFirstResponseByte,
	}
	return httptrace.WithClientTrace(ctx, trace), func() { timing.report(endpoint) }
}

type updateRequestTiming struct {
	mu                                       sync.Mutex
	start                                    time.Time
	dnsStart, tlsStart                       time.Time
	dnsTime, connectTime, tlsTime, firstByte time.Duration
	reused                                   bool
	connectStarts                            map[string]time.Time
}

func (t *updateRequestTiming) onDNSStart(httptrace.DNSStartInfo) {
	t.mu.Lock()
	t.dnsStart = time.Now()
	t.mu.Unlock()
}

func (t *updateRequestTiming) onDNSDone(httptrace.DNSDoneInfo) {
	t.mu.Lock()
	t.dnsTime += time.Since(t.dnsStart)
	t.mu.Unlock()
}

func (t *updateRequestTiming) onConnectStart(network, addr string) {
	t.mu.Lock()
	t.connectStarts[network+"|"+addr] = time.Now()
	t.mu.Unlock()
}

func (t *updateRequestTiming) onConnectDone(network, addr string, _ error) {
	t.mu.Lock()
	key := network + "|" + addr
	if started, ok := t.connectStarts[key]; ok {
		t.connectTime += time.Since(started)
		delete(t.connectStarts, key)
	}
	t.mu.Unlock()
}

func (t *updateRequestTiming) onTLSHandshakeStart() {
	t.mu.Lock()
	t.tlsStart = time.Now()
	t.mu.Unlock()
}

func (t *updateRequestTiming) onTLSHandshakeDone(tls.ConnectionState, error) {
	t.mu.Lock()
	t.tlsTime += time.Since(t.tlsStart)
	t.mu.Unlock()
}

func (t *updateRequestTiming) onGotConn(info httptrace.GotConnInfo) {
	t.mu.Lock()
	t.reused = info.Reused
	t.mu.Unlock()
}

func (t *updateRequestTiming) onGotFirstResponseByte() {
	t.mu.Lock()
	t.firstByte = time.Since(t.start)
	t.mu.Unlock()
}

func (t *updateRequestTiming) report(endpoint string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf(
		"Network: HTTP %s (connection reused: %t, DNS: %s, connect: %s, TLS: %s, first byte: %s)",
		endpoint, t.reused, t.dnsTime.Round(time.Millisecond), t.connectTime.Round(time.Millisecond),
		t.tlsTime.Round(time.Millisecond), t.firstByte.Round(time.Millisecond))))
}
