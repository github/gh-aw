package connectors_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
)

func TestTeamsConnectorDenyBeforeCredentials(t *testing.T) {
	plan := teamsPlan(t)
	tests := []struct {
		name       string
		capability string
		operation  string
		args       map[string]any
		phase      string
		write      bool
		origins    []string
	}{
		{"missing-team", "context", "channel-thread-root", map[string]any{}, "context", false, []string{"https://graph.microsoft.com"}},
		{"wrong-team", "context", "channel-thread-root", teamsChangedArgs(teamsReadArgs(), "team", "other"), "context", false, []string{"https://graph.microsoft.com"}},
		{"wrong-channel", "mcp", "get-channel-message", teamsChangedArgs(teamsReadArgs(), "channel", "other"), "agent", false, []string{"https://graph.microsoft.com"}},
		{"wrong-thread", "context", "channel-thread-root", teamsChangedArgs(teamsReadArgs(), "thread", "other"), "context", false, []string{"https://graph.microsoft.com"}},
		{"wrong-conversation", "safe-output", "reply-to-activity", teamsChangedArgs(teamsWriteArgs(), "conversation", "other"), "safe-output", true, []string{"https://smba.trafficmanager.net"}},
		{"wrong-activity", "safe-output", "reply-to-activity", teamsChangedArgs(teamsWriteArgs(), "activity", "other"), "safe-output", true, []string{"https://smba.trafficmanager.net"}},
		{"unbounded-text", "safe-output", "reply-to-activity", teamsChangedArgs(teamsWriteArgs(), "text", strings.Repeat("x", 4001)), "safe-output", true, []string{"https://smba.trafficmanager.net"}},
		{"no-service-url-input", "safe-output", "reply-to-activity", teamsChangedArgs(teamsWriteArgs(), "service-url", "https://evil.example"), "safe-output", true, []string{"https://smba.trafficmanager.net"}},
		{"agent-cannot-send", "safe-output", "reply-to-activity", teamsWriteArgs(), "agent", true, []string{"https://smba.trafficmanager.net"}},
		{"host-denied-send", "safe-output", "reply-to-activity", teamsWriteArgs(), "safe-output", false, []string{"https://smba.trafficmanager.net"}},
		{"host-denied-origin", "context", "channel-thread-root", teamsReadArgs(), "context", false, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			credentials, requests := 0, 0
			runtime := connectors.Runtime{Phase: test.phase, AllowedOrigins: test.origins, AllowWrites: test.write,
				ResolveSecret: func(context.Context, string, string) (string, error) {
					credentials++
					return "", errors.New("must not resolve")
				},
				Client: &http.Client{Transport: teamsTransport(func(*http.Request) (*http.Response, error) {
					requests++
					return nil, errors.New("must not request")
				})}}
			if _, err := plan.Execute(t.Context(), runtime, test.capability, test.operation, test.args); err == nil {
				t.Fatal("unsafe request accepted")
			}
			if credentials != 0 || requests != 0 {
				t.Fatal("denied request crossed the credential or HTTP boundary")
			}
		})
	}
}

func teamsChangedArgs(args map[string]any, key string, value any) map[string]any {
	args[key] = value
	return args
}

func TestTeamsConnectorResponseRejections(t *testing.T) {
	root := string(teamsData(t, "message.json"))
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"permission-denied", http.StatusForbidden, `{"error":{"code":"Forbidden"}}`},
		{"throttled", http.StatusTooManyRequests, `{"error":{"code":"TooManyRequests"}}`},
		{"empty-success", http.StatusOK, `{}`},
		{"malformed-success", http.StatusOK, `not JSON`},
		{"oversized", http.StatusOK, strings.Repeat("x", 65537)},
		{"wrong-channel", http.StatusOK, strings.Replace(root, "approved-channel", "other-channel", 1)},
		{"wrong-root", http.StatusOK, strings.Replace(root, "1730000000000", "1730000000999", 1)},
		{"non-message", http.StatusOK, strings.Replace(root, `"messageType": "message"`, `"messageType": "systemEventMessage"`, 1)},
		{"bot-authored-root", http.StatusOK, strings.Replace(root, `"user":`, `"application":`, 1)},
		{"reply-not-root", http.StatusOK, strings.Replace(root, `"replyToId": null`, `"replyToId": "1730000000999"`, 1)},
	}
	plan := teamsPlan(t)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			runtime := teamsRuntime("context", func(request *http.Request) (*http.Response, error) {
				requests++
				response := teamsResponse(request, test.status, []byte(test.body))
				response.Header.Set("Retry-After", "30")
				return response, nil
			})
			if _, err := plan.Execute(t.Context(), runtime, "context", "channel-thread-root", teamsReadArgs()); err == nil {
				t.Fatal("failed or out-of-scope Graph response accepted")
			}
			if requests != 1 {
				t.Fatal("failure must not cause an implicit retry")
			}
		})
	}
}

func TestTeamsConnectorWritesRequireEvidenceAndNeverRetry(t *testing.T) {
	plan := teamsPlan(t)
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusTooManyRequests} {
		requests := 0
		runtime := teamsRuntime("safe-output", func(request *http.Request) (*http.Response, error) {
			requests++
			return teamsResponse(request, status, []byte(`{}`)), nil
		})
		if _, err := plan.Execute(t.Context(), runtime, "safe-output", "reply-to-activity", teamsWriteArgs()); err == nil {
			t.Fatal("missing activity ID or failed send must not look successful")
		}
		if requests != 1 {
			t.Fatal("potentially ambiguous bot write was retried")
		}
	}
}

func TestTeamsConnectorStagedOutputRevalidation(t *testing.T) {
	plan := teamsPlan(t)
	runtime := teamsRuntime("safe-output", func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid staged output must not issue a request")
		return nil, errors.New("unexpected request")
	})
	staged, err := plan.PrepareSafeOutput("reply-to-activity", teamsWriteArgs())
	if err != nil {
		t.Fatal(err)
	}
	staged.Arguments["conversation"] = "other"
	if _, err := plan.ExecuteSafeOutput(t.Context(), runtime, *staged); err == nil {
		t.Fatal("staged conversation tampering accepted")
	}
	staged.Digest = "another-plan"
	if _, err := plan.ExecuteSafeOutput(t.Context(), runtime, *staged); err == nil {
		t.Fatal("staged request from another plan accepted")
	}
}

func TestTeamsConnectorDoesNotFollowPagination(t *testing.T) {
	plan := teamsPlan(t)
	requests := 0
	runtime := teamsRuntime("context", func(request *http.Request) (*http.Response, error) {
		requests++
		return teamsResponse(request, http.StatusOK, []byte(`{"value":[],"@odata.nextLink":"https://evil.example/steal"}`)), nil
	})
	result, err := plan.Execute(t.Context(), runtime, "context", "channel-thread-replies", teamsReadArgs())
	if err != nil || string(result.Data) != `{"replies":[]}` || requests != 1 {
		t.Fatalf("pagination must be excluded, not followed: %+v %v calls=%d", result, err, requests)
	}
}

func TestTeamsConnectorExactBounds(t *testing.T) {
	plan := teamsPlan(t)
	for _, length := range []int{4000, 4001} {
		_, err := plan.PrepareSafeOutput("reply-to-activity", teamsChangedArgs(teamsWriteArgs(), "text", strings.Repeat("x", length)))
		if (err == nil) != (length == 4000) {
			t.Fatalf("text length %d produced unexpected error: %v", length, err)
		}
	}
	var document map[string]any
	if err := json.Unmarshal(teamsData(t, "message.json"), &document); err != nil {
		t.Fatal(err)
	}
	document["padding"] = ""
	base, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, length := range []int{65536, 65537} {
		document["padding"] = strings.Repeat("x", length-len(base))
		body, err := json.Marshal(document)
		if err != nil || len(body) != length {
			t.Fatal("response threshold fixture construction failed", err)
		}
		runtime := teamsRuntime("context", func(request *http.Request) (*http.Response, error) {
			return teamsResponse(request, http.StatusOK, body), nil
		})
		_, err = plan.Execute(t.Context(), runtime, "context", "channel-thread-root", teamsReadArgs())
		if (err == nil) != (length == 65536) {
			t.Fatalf("response length %d produced unexpected error: %v", length, err)
		}
	}
}
