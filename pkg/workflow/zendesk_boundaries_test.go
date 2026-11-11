//go:build !integration

package workflow

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func TestZendeskConnectorArgumentRejections(t *testing.T) {
	read := zendeskPlan(t, "agent")
	write := zendeskPlan(t, "safe-output")
	calls, secrets := 0, 0
	runtime := connectors.Runtime{
		AllowedOrigins: []string{"https://example.zendesk.com"}, AllowWrites: true,
		ResolveSecret: func(context.Context, string, string) (string, error) {
			secrets++
			return "unexpected-resolution", nil
		},
		Client: &http.Client{Transport: zendeskTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("unexpected request")
		})},
	}
	for _, test := range []struct {
		name, input string
		value       any
	}{
		{"another tenant", "subdomain", "other"},
		{"uppercase tenant", "subdomain", "Example"},
		{"tenant URL", "subdomain", "https://example.zendesk.com"},
		{"tenant path", "subdomain", "example/other"},
		{"oversized tenant", "subdomain", strings.Repeat("a", 64)},
		{"another ticket", "ticket", "35437"},
		{"numeric ticket", "ticket", 35436},
		{"leading zero", "ticket", "035436"},
		{"zero ticket", "ticket", "0"},
		{"negative ticket", "ticket", "-1"},
		{"oversized ticket", "ticket", "1234567890123456"},
		{"ticket traversal", "ticket", "../35436"},
		{"ticket escape", "ticket", "%3335436"},
		{"ticket query", "ticket", "35436?include=users"},
		{"ticket newline", "ticket", "35436\n"},
		{"temporary reference", "ticket", "#aw_ticket"},
		{"agent selected URL", "url", "https://other.invalid"},
		{"pagination", "page", "2"},
		{"arbitrary fields", "fields", "requester"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := zendeskReadArgs()
			args[test.input] = test.value
			runtime.Phase = "agent"
			_, err := read.Execute(t.Context(), runtime, "mcp", "get-ticket", args)
			require.Error(t, err)
			args["text"] = "Internal triage."
			_, err = write.PrepareSafeOutput("add-internal-note", args)
			require.Error(t, err)
			runtime.Phase = "safe-output"
			_, err = write.Execute(t.Context(), runtime, "safe-output", "add-internal-note", args)
			require.Error(t, err)
		})
	}
	for _, input := range []string{"subdomain", "ticket"} {
		args := zendeskReadArgs()
		delete(args, input)
		runtime.Phase = "agent"
		_, err := read.Execute(t.Context(), runtime, "mcp", "get-ticket", args)
		require.Error(t, err)
	}
	for _, text := range []any{"", " \t\r\n\f\v", "{{ticket.requester.email}}", "An unmatched { brace", "Closing }", strings.Repeat("x", 4001), 42, nil} {
		args := zendeskNoteArgs()
		args["text"] = text
		_, err := write.PrepareSafeOutput("add-internal-note", args)
		require.Error(t, err)
	}
	for _, input := range []string{"public", "author_id", "status", "html_body", "uploads", "ticket", "text"} {
		args := zendeskNoteArgs()
		if input == "ticket" || input == "text" {
			delete(args, input)
		} else {
			args[input] = true
		}
		_, err := write.PrepareSafeOutput("add-internal-note", args)
		require.Error(t, err)
	}
	for _, text := range []string{strings.Repeat("x", 4000), strings.Repeat("界", 4000), strings.Repeat("\U0001F4AC", 4000), "First\nSecond <&> $(not-code)"} {
		args := zendeskNoteArgs()
		args["text"] = text
		_, err := write.PrepareSafeOutput("add-internal-note", args)
		require.NoError(t, err)
	}
	require.Zero(t, calls)
	require.Zero(t, secrets)
}

func TestZendeskConnectorExecutionAuthority(t *testing.T) {
	plan := zendeskPlan(t, "safe-output")
	secrets, calls := 0, 0
	runtime := connectors.Runtime{
		Phase: "agent", AllowWrites: true, AllowedOrigins: []string{"https://example.zendesk.com"},
		ResolveSecret: func(context.Context, string, string) (string, error) {
			secrets++
			return "must-not-resolve", nil
		},
		Client: &http.Client{Transport: zendeskTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("must-not-send")
		})},
	}
	proposal, err := plan.PrepareSafeOutput("add-internal-note", zendeskNoteArgs())
	require.NoError(t, err)
	_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *proposal)
	require.Error(t, err)
	runtime.Phase, runtime.AllowWrites = "safe-output", false
	_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *proposal)
	require.Error(t, err)
	runtime.AllowWrites = true
	runtime.AllowedOrigins = []string{"https://other.zendesk.com"}
	_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *proposal)
	require.Error(t, err)
	runtime.AllowedOrigins = []string{"https://example.zendesk.com"}
	proposal.Arguments = maps.Clone(proposal.Arguments)
	proposal.Arguments["public"] = true
	_, err = plan.ExecuteSafeOutput(t.Context(), runtime, *proposal)
	require.Error(t, err, "execution must revalidate a tampered proposal")
	require.Zero(t, secrets)
	require.Zero(t, calls)
}

func TestZendeskConnectorResponseEvidence(t *testing.T) {
	read, write := zendeskPlan(t, "agent"), zendeskPlan(t, "safe-output")
	ticket, note := zendeskFixture(t, "ticket.json"), zendeskFixture(t, "note.json")
	for _, test := range []struct {
		name, phase, body string
	}{
		{"read empty", "agent", ""},
		{"read missing ticket", "agent", `{}`},
		{"read invalid JSON", "agent", `not JSON`},
		{"read trailing document", "agent", ticket + "{}"},
		{"read other ticket", "agent", strings.Replace(ticket, `"id": 35436`, `"id": 35437`, 1)},
		{"read string ID", "agent", strings.Replace(ticket, `"id": 35436`, `"id": "35436"`, 1)},
		{"read wrong canonical URL", "agent", strings.Replace(ticket, "example.zendesk.com", "other.zendesk.com", 1)},
		{"read missing subject", "agent", strings.Replace(ticket, `"subject": "Actions triage fixture",`, "", 1)},
		{"read null subject", "agent", strings.Replace(ticket, `"subject": "Actions triage fixture"`, `"subject": null`, 1)},
		{"read wrong status type", "agent", strings.Replace(ticket, `"status": "open"`, `"status": 7`, 1)},
		{"write empty", "safe-output", ""},
		{"write only ticket", "safe-output", `{"ticket":{"id":35436}}`},
		{"write invalid JSON", "safe-output", `not JSON`},
		{"write missing audit ID", "safe-output", strings.Replace(note, `"id": 70001,`, "", 1)},
		{"write other ticket", "safe-output", strings.Replace(note, `"id": 35436`, `"id": 35437`, 1)},
		{"write wrong audit ticket", "safe-output", strings.Replace(note, `"ticket_id": 35436`, `"ticket_id": 35437`, 1)},
		{"write string audit ID", "safe-output", strings.Replace(note, `"id": 70001`, `"id": "70001"`, 1)},
		{"write missing event ID", "safe-output", strings.Replace(note, `"id": 70002,`, "", 1)},
		{"write wrong event type", "safe-output", strings.Replace(note, `"type": "Comment"`, `"type": "Change"`, 1)},
		{"write public event", "safe-output", strings.Replace(note, `"public": false`, `"public": true`, 1)},
		{"write string visibility", "safe-output", strings.Replace(note, `"public": false`, `"public": "false"`, 1)},
		{"write missing visibility", "safe-output", strings.Replace(note, `"public": false,`, "", 1)},
		{"write transformed body", "safe-output", strings.Replace(note, "GitHub Actions triage recorded.", "Provider-normalized note.", 1)},
		{"write different first event", "safe-output", strings.Replace(note, `"events": [`, `"events": [{"id":70000,"type":"Notification","body":"First"} ,`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			runtime := zendeskRuntime(t, test.phase, func(*http.Request) (int, string) {
				calls++
				return 200, test.body
			})
			if test.phase == "agent" {
				_, err := read.Execute(t.Context(), runtime, "mcp", "get-ticket", zendeskReadArgs())
				require.Error(t, err)
			} else {
				_, err := write.Execute(t.Context(), runtime, "safe-output", "add-internal-note", zendeskNoteArgs())
				require.Error(t, err)
			}
			require.Equal(t, 1, calls, "unverified writes must never be retried")
		})
	}
}

func TestZendeskConnectorResponseByteBoundaries(t *testing.T) {
	for _, phase := range []string{"agent", "safe-output"} {
		plan := zendeskPlan(t, phase)
		body := strings.TrimSpace(zendeskFixture(t, "ticket.json"))
		if phase == "safe-output" {
			body = strings.TrimSpace(zendeskFixture(t, "note.json"))
		}
		for _, size := range []int{65536, 65537} {
			t.Run(phase+"/"+strconv.Itoa(size), func(t *testing.T) {
				template := strings.TrimSuffix(body, "}") + `,"padding":""}`
				response := strings.TrimSuffix(body, "}") + `,"padding":"` + strings.Repeat("x", size-len(template)) + `"}`
				require.Len(t, response, size)
				calls := 0
				runtime := zendeskRuntime(t, phase, func(*http.Request) (int, string) {
					calls++
					return 200, response
				})
				capability, operation, args := "mcp", "get-ticket", zendeskReadArgs()
				if phase == "safe-output" {
					capability, operation, args = "safe-output", "add-internal-note", zendeskNoteArgs()
				}
				result, err := plan.Execute(t.Context(), runtime, capability, operation, args)
				if size == 65536 {
					require.NoError(t, err)
					require.NotContains(t, string(result.Data), "padding")
				} else {
					require.ErrorContains(t, err, "byte bound")
				}
				require.Equal(t, 1, calls)
			})
		}
	}
}

func TestZendeskConnectorErrorsNeverRetry(t *testing.T) {
	for _, phase := range []string{"agent", "safe-output"} {
		plan := zendeskPlan(t, phase)
		capability, operation, args := "mcp", "get-ticket", zendeskReadArgs()
		if phase == "safe-output" {
			capability, operation, args = "safe-output", "add-internal-note", zendeskNoteArgs()
		}
		for _, status := range []int{302, 400, 401, 403, 404, 409, 422, 429, 500} {
			t.Run(phase+"/"+strconv.Itoa(status), func(t *testing.T) {
				calls := 0
				runtime := zendeskRuntime(t, phase, func(*http.Request) (int, string) {
					calls++
					return status, `{"error":"private provider diagnostic"}`
				})
				_, err := plan.Execute(t.Context(), runtime, capability, operation, args)
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private provider diagnostic")
				require.Equal(t, 1, calls)
			})
		}
		calls := 0
		runtime := zendeskRuntime(t, phase, nil)
		runtime.Client.Transport = zendeskTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("private transport diagnostic")
		})
		_, err := plan.Execute(t.Context(), runtime, capability, operation, args)
		require.ErrorContains(t, err, "outcome may be unknown")
		require.NotContains(t, err.Error(), "private transport diagnostic")
		require.Equal(t, 1, calls)
	}
}

func TestZendeskConnectorNoteBodyIsData(t *testing.T) {
	plan := zendeskPlan(t, "safe-output")
	args := zendeskNoteArgs()
	args["text"] = "First <&>\nSecond $(not-code) \u754c"
	runtime := zendeskRuntime(t, "safe-output", func(request *http.Request) (int, string) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"ticket":{"comment":{"body":"First <&>\nSecond $(not-code) 界","public":false}}}`, string(body))
		return 200, strings.Replace(zendeskFixture(t, "note.json"), "GitHub Actions triage recorded.", `First <&>\nSecond $(not-code) 界`, 1)
	})
	_, err := plan.Execute(t.Context(), runtime, "safe-output", "add-internal-note", args)
	require.NoError(t, err)
}

func TestZendeskConnectorCredentialErrorsFailClosed(t *testing.T) {
	for _, phase := range []string{"agent", "safe-output"} {
		plan := zendeskPlan(t, phase)
		capability, operation, args := "mcp", "get-ticket", zendeskReadArgs()
		if phase == "safe-output" {
			capability, operation, args = "safe-output", "add-internal-note", zendeskNoteArgs()
		}
		for _, secret := range []string{"", "bad\r\nheader"} {
			resolutions, calls := 0, 0
			runtime := zendeskRuntime(t, phase, func(*http.Request) (int, string) {
				calls++
				return 200, "{}"
			})
			runtime.ResolveSecret = func(context.Context, string, string) (string, error) {
				resolutions++
				return secret, nil
			}
			_, err := plan.Execute(t.Context(), runtime, capability, operation, args)
			require.ErrorContains(t, err, "credential resolution failed")
			require.Equal(t, 1, resolutions, "never try another credential")
			require.Zero(t, calls)
		}
	}
}
