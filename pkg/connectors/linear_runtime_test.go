package connectors_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
)

const (
	linearIssue   = "22222222-2222-4222-8222-222222222222"
	linearOrg     = "11111111-1111-4111-8111-111111111111"
	linearTeam    = "33333333-3333-4333-8333-333333333333"
	linearProject = "44444444-4444-4444-8444-444444444444"
)

type linearTransport func(*http.Request) (*http.Response, error)

func (transport linearTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func linearPlan(t *testing.T) *connectors.Plan {
	t.Helper()
	data, err := os.ReadFile("testdata/providers/linear/binding.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var binding connectors.Binding
	if err := connectors.Decode(data, &binding); err != nil {
		t.Fatal(err)
	}
	plan, err := connectors.Lower("../..", binding)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func linearRuntime(t *testing.T, phase string, transport http.RoundTripper) connectors.Runtime {
	t.Helper()
	return connectors.Runtime{
		Phase: phase, AllowedOrigins: []string{"https://api.linear.app", "https://mcp.linear.app"},
		Client: &http.Client{Transport: transport}, AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, requestedPhase, secret string) (string, error) {
			expected := map[string]string{
				"agent": "LINEAR_READ_API_KEY", "context": "LINEAR_CONTEXT_API_KEY", "safe-output": "LINEAR_WRITE_API_KEY",
			}[phase]
			if requestedPhase != phase || secret != expected {
				t.Errorf("wrong credential boundary: %s %s", requestedPhase, secret)
				return "", errors.New("wrong credential boundary")
			}
			return "fixture-" + phase, nil
		},
	}
}

func linearResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func linearIssueResponse(description any) map[string]any {
	return map[string]any{"data": map[string]any{
		"organization": map[string]any{"id": linearOrg},
		"issue": map[string]any{
			"id": linearIssue, "identifier": "ENG-12", "title": "Issue title", "description": description,
			"team": map[string]any{"id": linearTeam}, "project": map[string]any{"id": linearProject},
		},
	}, "unselected": "do not hydrate"}
}

func encodeLinear(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLinearConnectorTriggerExecution(t *testing.T) {
	plan := linearPlan(t)
	for trigger, native := range map[string]string{"issue-created": "create", "issue-updated": "update", "issue-removed": "remove"} {
		t.Run(trigger, func(t *testing.T) {
			payload := map[string]any{
				"type": "Issue", "action": native, "organizationId": linearOrg,
				"data": map[string]any{"id": linearIssue, "teamId": linearTeam},
				"url":  "https://linear.app/example/issue/ENG-12/example", "description": "not projected",
			}
			result, err := plan.ProjectEvent(trigger, native, encodeLinear(t, payload))
			if err != nil || result.Operational || !result.Untrusted || len(result.Fields) != 4 || result.Fields["issue-id"] != linearIssue {
				t.Fatalf("unexpected fixture projection: %+v %v", result, err)
			}
			for _, invalid := range []map[string]any{
				{"organizationId": "other-org"}, {"organizationId": nil}, {"type": "IssueLabel"},
				{"action": "closed"}, {"data": map[string]any{"id": linearIssue, "teamId": "other-team"}},
				{"data": map[string]any{"id": linearIssue}}, {"data": map[string]any{}},
			} {
				copy := maps.Clone(payload)
				maps.Copy(copy, invalid)
				if _, err := plan.ProjectEvent(trigger, native, encodeLinear(t, copy)); err == nil {
					t.Fatal("invalid source evidence accepted", invalid)
				}
			}
			if _, err := plan.ProjectEvent(trigger, "invented", encodeLinear(t, payload)); err == nil {
				t.Fatal("undeclared native event accepted")
			}
		})
	}
	if !errors.Is(plan.ActivateTriggers(), connectors.ErrIngressUnavailable) {
		t.Fatal("configuration must not activate unauthenticated ingress")
	}
}

func TestLinearConnectorReadsAndContext(t *testing.T) {
	plan := linearPlan(t)
	for _, description := range []any{"Untrusted ${{ text }}; $(not-code)", nil} {
		for capability, operation := range map[string]string{"mcp": "get-issue", "context": "issue-details"} {
			t.Run(capability+"/"+string(encodeLinear(t, description)), func(t *testing.T) {
				phase := "agent"
				if capability == "context" {
					phase = "context"
				}
				calls := 0
				transport := linearTransport(func(request *http.Request) (*http.Response, error) {
					calls++
					if request.URL.String() != "https://api.linear.app/graphql" || request.Method != http.MethodPost || request.Header.Get("Authorization") != "fixture-"+phase {
						t.Error("read endpoint, method or phase credential changed")
					}
					var body struct {
						Query     string         `json:"query"`
						Variables map[string]any `json:"variables"`
					}
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if !strings.HasPrefix(body.Query, "query ") || !reflect.DeepEqual(body.Variables, map[string]any{"id": linearIssue}) {
						t.Error("query document or variable binding changed")
					}
					return linearResponse(request, 200, string(encodeLinear(t, linearIssueResponse(description)))), nil
				})
				result, err := plan.Execute(t.Context(), linearRuntime(t, phase, transport), capability, operation, map[string]any{"issue-id": linearIssue})
				if err != nil || !result.Untrusted || calls != 1 {
					t.Fatalf("read failed: %+v %v calls=%d", result, err, calls)
				}
				var projected map[string]any
				if err := json.Unmarshal(result.Data, &projected); err != nil {
					t.Fatal(err)
				}
				if len(projected) != 4 || projected["issue-id"] != linearIssue || projected["issue"].(map[string]any)["description"] != description {
					t.Fatal("unexpected context projection", projected)
				}
			})
		}
	}
}

func TestLinearConnectorReadFailures(t *testing.T) {
	plan := linearPlan(t)
	tests := []struct {
		name   string
		change func(map[string]any)
	}{
		{"GraphQL partial failure", func(response map[string]any) { response["errors"] = []any{map[string]any{"message": "access denied"}} }},
		{"missing issue", func(response map[string]any) { delete(response["data"].(map[string]any), "issue") }},
		{"wrong organization", func(response map[string]any) {
			response["data"].(map[string]any)["organization"] = map[string]any{"id": "other"}
		}},
		{"wrong issue", func(response map[string]any) {
			response["data"].(map[string]any)["issue"].(map[string]any)["id"] = "other"
		}},
		{"wrong team", func(response map[string]any) {
			response["data"].(map[string]any)["issue"].(map[string]any)["team"] = map[string]any{"id": "other"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := linearIssueResponse("description")
			test.change(response)
			transport := linearTransport(func(request *http.Request) (*http.Response, error) {
				return linearResponse(request, 200, string(encodeLinear(t, response))), nil
			})
			if _, err := plan.Execute(t.Context(), linearRuntime(t, "context", transport), "context", "issue-details", map[string]any{"issue-id": linearIssue}); err == nil {
				t.Fatal("unsafe context accepted")
			}
		})
	}
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"deleted", "{}", 404}, {"forbidden", "{}", 403}, {"rate limited", "{}", 429},
		{"malformed", "{", 200}, {"oversize", strings.Repeat("x", 131073), 200},
		{"redirect", "{}", 302},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			transport := linearTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				response := linearResponse(request, test.status, test.body)
				response.Header.Set("Location", "https://untrusted.invalid")
				return response, nil
			})
			if _, err := plan.Execute(t.Context(), linearRuntime(t, "context", transport), "context", "issue-details", map[string]any{"issue-id": linearIssue}); err == nil || calls != 1 {
				t.Fatalf("failure must not retry or fall back: %v calls=%d", err, calls)
			}
		})
	}
}
