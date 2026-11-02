package connectors_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
)

func linearOutputArguments(name string) map[string]any {
	args := map[string]any{"body": "Untrusted ${{ text }}; $(not-code)"}
	if name == "create-issue" {
		args["team-id"], args["project-id"], args["title"] = linearTeam, linearProject, "Title"
	} else {
		args["issue-id"] = linearIssue
		if name == "update-issue" {
			args["title"] = "Title"
		}
	}
	return args
}

func linearMutationResponse(name string) map[string]any {
	if name == "add-comment" {
		return map[string]any{"data": map[string]any{"commentCreate": map[string]any{
			"success": true, "comment": map[string]any{"id": "comment-id", "issue": map[string]any{"id": linearIssue}},
		}}}
	}
	root := "issueUpdate"
	if name == "create-issue" {
		root = "issueCreate"
	}
	return map[string]any{"data": map[string]any{root: map[string]any{
		"success": true, "issue": map[string]any{
			"id": linearIssue, "identifier": "ENG-12", "title": "Title",
			"team": map[string]any{"id": linearTeam}, "project": map[string]any{"id": linearProject},
		},
	}}}
}

func TestLinearConnectorSafeOutputs(t *testing.T) {
	plan := linearPlan(t)
	spec := loadLinear(t)
	for _, name := range []string{"create-issue", "update-issue", "add-comment"} {
		t.Run(name, func(t *testing.T) {
			args := linearOutputArguments(name)
			calls := 0
			transport := linearTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.String() != "https://api.linear.app/graphql" || request.Method != http.MethodPost || request.Header.Get("Authorization") != "fixture-safe-output" {
					t.Error("write endpoint, method or phase credential changed")
				}
				var document struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
					t.Fatal(err)
				}
				if document.Query != spec.SafeOutputs[name].Request.GraphQL || document.Variables["body"] != args["body"] {
					t.Error("fixed GraphQL document or untrusted variable content changed")
				}
				for variable, binding := range spec.SafeOutputs[name].Request.Query {
					if !reflect.DeepEqual(document.Variables[variable], args[binding.Input]) {
						t.Errorf("incorrect binding for %s", variable)
					}
				}
				return linearResponse(request, 200, string(encodeLinear(t, linearMutationResponse(name)))), nil
			})
			staged, err := plan.PrepareSafeOutput(name, args)
			if err != nil || calls != 0 {
				t.Fatal("staging must be credential-free and nonmutating", err)
			}
			result, err := plan.ExecuteSafeOutput(t.Context(), linearRuntime(t, "safe-output", transport), *staged)
			if err != nil || result.StatusCode != http.StatusOK || calls != 1 {
				t.Fatalf("write fixture failed: %+v %v calls=%d", result, err, calls)
			}
			staged.Digest = "different-plan"
			if _, err := plan.ExecuteSafeOutput(t.Context(), linearRuntime(t, "safe-output", transport), *staged); err == nil || calls != 1 {
				t.Fatal("digest mismatch must fail before execution")
			}
		})
	}
}

func TestLinearConnectorMutationResponseFailures(t *testing.T) {
	plan := linearPlan(t)
	for _, name := range []string{"create-issue", "update-issue", "add-comment"} {
		for _, failure := range []string{"false success", "missing success", "missing id", "missing entity", "wrong target", "GraphQL partial failure", "network failure"} {
			t.Run(name+"/"+failure, func(t *testing.T) {
				response := linearMutationResponse(name)
				root, entity := "issueUpdate", "issue"
				if name == "create-issue" {
					root = "issueCreate"
				}
				if name == "add-comment" {
					root, entity = "commentCreate", "comment"
				}
				payload := response["data"].(map[string]any)[root].(map[string]any)
				switch failure {
				case "false success":
					payload["success"] = false
				case "missing success":
					delete(payload, "success")
				case "missing id":
					delete(payload[entity].(map[string]any), "id")
				case "missing entity":
					delete(payload, entity)
				case "wrong target":
					result := payload[entity].(map[string]any)
					switch name {
					case "create-issue":
						result["team"] = map[string]any{"id": "other-team"}
					case "add-comment":
						result["issue"] = map[string]any{"id": "other-issue"}
					default:
						result["id"] = "other-issue"
					}
				case "GraphQL partial failure":
					response["errors"] = []any{map[string]any{"message": "fixture secret must not be echoed"}}
				}
				calls := 0
				transport := linearTransport(func(request *http.Request) (*http.Response, error) {
					calls++
					if failure == "network failure" {
						return nil, errors.New("fixture secret must not be echoed")
					}
					return linearResponse(request, 200, string(encodeLinear(t, response))), nil
				})
				staged, err := plan.PrepareSafeOutput(name, linearOutputArguments(name))
				if err != nil {
					t.Fatal(err)
				}
				_, err = plan.ExecuteSafeOutput(t.Context(), linearRuntime(t, "safe-output", transport), *staged)
				if err == nil || calls != 1 || strings.Contains(err.Error(), "fixture secret") {
					t.Fatalf("write failure must fail without retries or body leakage: %v calls=%d", err, calls)
				}
			})
		}
	}
}

func TestLinearConnectorDeniesBeforeCredentials(t *testing.T) {
	plan := linearPlan(t)
	secretCalls, requests := 0, 0
	runtime := connectors.Runtime{
		Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://api.linear.app"},
		ResolveSecret: func(context.Context, string, string) (string, error) {
			secretCalls++
			return "fixture", nil
		},
		Client: &http.Client{Transport: linearTransport(func(*http.Request) (*http.Response, error) {
			requests++
			return nil, errors.New("unexpected request")
		})},
	}
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"add-comment", map[string]any{"issue-id": "ENG-12", "body": "body"}},
		{"add-comment", map[string]any{"issue-id": "55555555-5555-4555-8555-555555555555", "body": "body"}},
		{"add-comment", map[string]any{"issue-id": linearIssue, "body": "   "}},
		{"add-comment", map[string]any{"issue-id": linearIssue, "body": strings.Repeat("x", 65001)}},
		{"add-comment", map[string]any{"issue-id": linearIssue, "body": "body", "query": "mutation"}},
		{"add-comment", map[string]any{"issue-id": linearIssue}},
		{"create-issue", map[string]any{"team-id": "other-team", "project-id": linearProject, "title": "Title", "body": "body"}},
		{"create-issue", map[string]any{"team-id": linearTeam, "project-id": "other-project", "title": "Title", "body": "body"}},
		{"create-issue", map[string]any{"team-id": linearTeam, "project-id": linearProject, "title": strings.Repeat("x", 129), "body": "body"}},
		{"delete-issue", map[string]any{"issue-id": linearIssue}},
	} {
		if _, err := plan.Execute(t.Context(), runtime, "safe-output", test.name, test.args); err == nil {
			t.Fatal("unsafe output accepted", test.name, test.args)
		}
	}
	args := linearOutputArguments("add-comment")
	for _, phase := range []string{"agent", "context"} {
		runtime.Phase = phase
		if _, err := plan.Execute(t.Context(), runtime, "safe-output", "add-comment", args); err == nil {
			t.Fatal("incorrect phase accepted")
		}
	}
	runtime.Phase, runtime.AllowWrites = "safe-output", false
	if _, err := plan.Execute(t.Context(), runtime, "safe-output", "add-comment", args); err == nil {
		t.Fatal("write authorization is required")
	}
	runtime.AllowWrites, runtime.AllowedOrigins = true, nil
	if _, err := plan.Execute(t.Context(), runtime, "safe-output", "add-comment", args); err == nil {
		t.Fatal("origin authorization is required")
	}
	if secretCalls != 0 || requests != 0 {
		t.Fatal("denied requests must not resolve credentials or contact providers")
	}
}

func TestLinearConnectorResolution(t *testing.T) {
	plan := linearPlan(t)
	for _, kind := range []string{"team", "project"} {
		for _, outcome := range []string{"unique", "missing", "ambiguous", "out-of-scope"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				field, input, value, id := "teams", "team-key", "ENG", linearTeam
				if kind == "project" {
					field, input, value, id = "projects", "project-slug", "abcdef123456", linearProject
				}
				if outcome == "out-of-scope" {
					id = "55555555-5555-4555-8555-555555555555"
				}
				nodes := []any{map[string]any{"id": id}}
				if outcome == "missing" {
					nodes = nil
				}
				response := map[string]any{"data": map[string]any{
					"organization": map[string]any{"id": linearOrg},
					field:          map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": outcome == "ambiguous"}},
				}}
				transport := linearTransport(func(request *http.Request) (*http.Response, error) {
					return linearResponse(request, 200, string(encodeLinear(t, response))), nil
				})
				_, err := plan.Execute(t.Context(), linearRuntime(t, "agent", transport), "mcp", "resolve-"+kind, map[string]any{input: value})
				if (err == nil) != (outcome == "unique") {
					t.Fatalf("resolution outcome %s: %v", outcome, err)
				}
			})
		}
	}
}
