package connectors

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type retryTransport func(*http.Request) (*http.Response, error)

func (transport retryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestConnectorDeclaredReadRetries(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		after      string
		transport  bool
		policy     bool
		wantCalls  int
		wantError  bool
		eventually bool
	}{
		{"transient-success", 503, "", false, true, 3, false, true},
		{"exhausted", 503, "", false, true, 3, true, false},
		{"not-declared", 503, "", false, false, 1, true, false},
		{"not-retryable", 403, "", false, true, 1, true, false},
		{"transport-uncertain", 0, "", true, true, 1, true, false},
		{"long-retry-after", 429, "3600", false, true, 1, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := fixturePlan(t, "https://api.example.com")
			spec := original.spec
			if test.policy {
				tool := spec.MCP["get-issue"]
				tool.Retry = &RetryPolicy{MaxAttempts: 3, BackoffMS: 1, RetryOn: []int{429, 503}}
				spec.MCP["get-issue"] = tool
			}
			plan := writePlan(t, spec, original.connection, original.binding)
			calls, resolutions := 0, 0
			runtime := testRuntime(&http.Client{Transport: retryTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "Bearer agent-only", request.Header.Get("Authorization"))
				if test.transport {
					return nil, errors.New("synthetic transport failure")
				}
				status := test.status
				if test.eventually && calls == 3 {
					status = 200
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{test.after}}, Body: io.NopCloser(strings.NewReader(`{"id":"ISSUE-1","title":"An approved issue title"}`))}, nil
			})}, "https://api.example.com", "agent")
			resolver := runtime.ResolveSecret
			runtime.ResolveSecret = func(ctx context.Context, phase, name string) (string, error) {
				resolutions++
				return resolver(ctx, phase, name)
			}
			_, err := plan.Execute(context.Background(), runtime, "mcp", "get-issue", map[string]any{"issue": "ISSUE-1"})
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.wantCalls, calls)
			require.Equal(t, 1, resolutions, "retry must not switch or reacquire credentials")
		})
	}
}

func TestConnectorRetryPolicyValidation(t *testing.T) {
	policy := &RetryPolicy{MaxAttempts: 3, BackoffMS: 1, RetryOn: []int{429, 503}}
	require.NoError(t, validateRetryPolicy(policy, Request{Method: "GET"}, "agent"))
	require.Error(t, validateRetryPolicy(policy, Request{Method: "POST"}, "safe-output"))
	policy.Idempotent = true
	require.Error(t, validateRetryPolicy(policy, Request{Method: "POST"}, "safe-output"), "creation and append operations must not retry")
	require.NoError(t, validateRetryPolicy(policy, Request{Method: "PUT"}, "safe-output"))
	policy.RetryOn = []int{401}
	require.Error(t, validateRetryPolicy(policy, Request{Method: "GET"}, "agent"))
	policy.RetryOn = []int{503, 503}
	require.Error(t, validateRetryPolicy(policy, Request{Method: "GET"}, "agent"))
	policy.RetryOn = []int{503}
	policy.MaxAttempts = 5
	require.Error(t, validateRetryPolicy(policy, Request{Method: "GET"}, "agent"))
}

func TestConnectorTitleMinimumCodePoints(t *testing.T) {
	input := Input{Type: "string", Sanitize: "title", MaxLength: 128}
	require.NoError(t, validateInputs(map[string]Input{"title": input}))
	for _, character := range []string{"a", "\U0001f4ac"} {
		require.Error(t, validateArgument(input, strings.Repeat(character, 19)))
		require.NoError(t, validateArgument(input, strings.Repeat(character, 20)))
		require.NoError(t, validateArgument(input, strings.Repeat(character, 128)))
		require.Error(t, validateArgument(input, strings.Repeat(character, 129)))
	}
	input.MaxLength = 19
	require.Error(t, validateInputs(map[string]Input{"title": input}))
}
