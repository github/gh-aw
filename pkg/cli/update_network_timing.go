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
	var mu sync.Mutex
	start := time.Now()
	var dnsStart, tlsStart time.Time
	var dnsTime, connectTime, tlsTime, firstByte time.Duration
	var reused bool
	connectStarts := make(map[string]time.Time)
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			mu.Lock()
			dnsStart = time.Now()
			mu.Unlock()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			mu.Lock()
			dnsTime += time.Since(dnsStart)
			mu.Unlock()
		},
		ConnectStart: func(network, addr string) {
			mu.Lock()
			connectStarts[network+"|"+addr] = time.Now()
			mu.Unlock()
		},
		ConnectDone: func(network, addr string, _ error) {
			mu.Lock()
			key := network + "|" + addr
			if started, ok := connectStarts[key]; ok {
				connectTime += time.Since(started)
				delete(connectStarts, key)
			}
			mu.Unlock()
		},
		TLSHandshakeStart: func() {
			mu.Lock()
			tlsStart = time.Now()
			mu.Unlock()
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			mu.Lock()
			tlsTime += time.Since(tlsStart)
			mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			reused = info.Reused
			mu.Unlock()
		},
		GotFirstResponseByte: func() {
			mu.Lock()
			firstByte = time.Since(start)
			mu.Unlock()
		},
	}
	report := func() {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf(
			"Network: HTTP %s (connection reused: %t, DNS: %s, connect: %s, TLS: %s, first byte: %s)",
			endpoint, reused, dnsTime.Round(time.Millisecond), connectTime.Round(time.Millisecond),
			tlsTime.Round(time.Millisecond), firstByte.Round(time.Millisecond))))
	}
	return httptrace.WithClientTrace(ctx, trace), report
}
