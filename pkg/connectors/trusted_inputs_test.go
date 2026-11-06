package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const trustedTeamID = "00000000-0000-4000-8000-000000000001"
const trustedProjectID = "00000000-0000-4000-8000-000000000002"

func trustedInputService(t *testing.T, origin string) *Service {
	t.Helper()
	uuid := Input{Type: "string", Required: true, MaxLength: 36, Pattern: `^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`}
	spec := ConnectorSpec{APIVersion: APIVersion, Kind: "ConnectorSpec", ID: "example.com/issues",
		Endpoints: map[string]string{"api": origin}, Credentials: map[string]Credential{
			"writer": {Phase: "safe-output", Header: "Authorization", Scheme: "bearer", Scopes: []string{"issues:write"}},
		}, SafeOutputs: map[string]Operation{"create-issue": {
			Credential: "writer", Inputs: map[string]Input{"team-id": uuid, "project-id": uuid, "title": {Type: "string", Required: true, MaxLength: 80}},
			Resources: map[string]string{"team-id": "teams", "project-id": "projects"}, MaxResponseBytes: 1024,
			Request: Request{Endpoint: "api", Method: "POST", Path: "/issues", Body: map[string]Value{
				"team": {Input: "team-id"}, "project": {Input: "project-id"}, "title": {Input: "title"},
			}},
			TrustedInputs: map[string]TrustedInput{"team-id": {Secret: "TEAM_ID"}, "project-id": {Value: "810f57a7e383"}},
			Preflight: &TrustedPreflight{
				Inputs: map[string]Input{"team-id": uuid, "project-id": {Type: "string", Required: true, MaxLength: 12, Pattern: `^[a-f0-9]{12}$`}},
				Request: Request{Endpoint: "api", Method: "POST", Path: "/graphql", GraphQL: "query TrustedProject($team: String!, $project: String!) { team(id: $team) { id } project(id: $project) { id teams(first: 50) { nodes { id } } } }",
					Query: map[string]Value{"team": {Input: "team-id"}, "project": {Input: "project-id"}}},
				MaxResponseBytes: 2048, ResponseRequired: []string{"/data/team/id", "/data/project/id"},
				ResponseInputs:   map[string]string{"/data/team/id": "team-id"},
				ResponseContains: map[string]string{"/data/project/teams/nodes/*/id": "team-id"},
				ResponseTypes:    map[string]string{"/data/team/id": "string", "/data/project/id": "string"},
				Project:          map[string]string{"project-id": "/data/project/id"},
			},
		}}}
	service, err := NewService(spec, Connection{APIVersion: APIVersion, Kind: "ConnectorConnection", Connector: spec.ID,
		Credentials: map[string]SecretRef{"writer": {Secret: "WRITE_TOKEN"}}, Resources: map[string][]string{}})
	require.NoError(t, err)
	return service
}

func TestTrustedResourcePreflight(t *testing.T) {
	requests, secrets := 0, 0
	response := `{"data":{"team":{"id":"` + trustedTeamID + `"},"project":{"id":"` + trustedProjectID + `","teams":{"nodes":[{"id":"` + trustedTeamID + `"}]}}}}`
	status := http.StatusOK
	redirect := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, "Bearer fixture-writer", r.Header.Get("Authorization"))
		if r.URL.Path == "/graphql" {
			if redirect != "" {
				w.Header().Set("Location", redirect)
			}
			w.WriteHeader(status)
			_, err := io.WriteString(w, response)
			assert.NoError(t, err)
			return
		}
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, trustedTeamID, body["team"])
		assert.Equal(t, trustedProjectID, body["project"])
		_, err := io.WriteString(w, `{"id":"created"}`)
		assert.NoError(t, err)
	}))
	defer server.Close()
	service := trustedInputService(t, server.URL)
	plan, err := service.Select(Selection{SafeOutputs: []string{"create-issue"}})
	require.NoError(t, err)
	plan, err = plan.ForPhase("safe-output")
	require.NoError(t, err)
	require.Equal(t, []string{"TEAM_ID", "WRITE_TOKEN"}, plan.Secrets())
	schema, err := plan.InputSchema("safe-output", "create-issue")
	require.NoError(t, err)
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, properties, "team-id")
	assert.NotContains(t, properties, "project-id")
	runtime := Runtime{Phase: "safe-output", AllowWrites: true, Client: server.Client(), AllowedOrigins: []string{server.URL},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			secrets++
			require.Equal(t, "safe-output", phase)
			if name == "TEAM_ID" {
				return trustedTeamID, nil
			}
			require.Equal(t, "WRITE_TOKEN", name)
			return "fixture-writer", nil
		}}
	host := CreatedResourceHost{Run: "trusted-run", Key: []byte(strings.Repeat("x", 32))}
	args := map[string]any{"title": "Haiku"}
	_, err = plan.PrepareSafeOutputIntent("create-issue", args)
	require.NoError(t, err)
	assert.Zero(t, secrets)
	assert.Zero(t, requests)
	_, err = plan.Execute(t.Context(), runtime, "safe-output", "create-issue", map[string]any{"title": "Haiku", "team-id": trustedTeamID, "project-id": trustedProjectID})
	require.ErrorContains(t, err, "must be resolved")
	assert.Zero(t, requests)
	assert.Zero(t, secrets)
	_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", args)
	require.NoError(t, err)
	assert.Equal(t, 2, requests)

	for _, invalid := range []map[string]any{
		{"title": "Haiku", "team-id": trustedTeamID}, {"title": "Haiku", "project-id": trustedProjectID},
		{"title": "Haiku", "receipt": "forged"}, {"title": strings.Repeat("x", 81)},
	} {
		before := secrets
		_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", invalid)
		require.Error(t, err)
		assert.Equal(t, before, secrets)
		assert.Equal(t, 2, requests)
	}
	goodResponse := response
	for _, invalid := range []string{
		`{}`, `{"errors":[{"message":"private diagnostic"}]}`, `{"data":{"team":null,"project":null}}`,
		strings.Replace(goodResponse, `"id":"`+trustedTeamID+`"`, `"id":"wrong-team"`, 1),
		strings.Replace(goodResponse, `"nodes":[{"id":"`+trustedTeamID+`"}]`, `"nodes":[]`, 1),
		strings.Replace(goodResponse, `"id":"`+trustedProjectID+`"`, `"id":"invalid-uuid"`, 1),
		strings.Replace(goodResponse, `"project":{"id"`, `"project":[{"id"`, 1),
		strings.Repeat("x", 2049),
	} {
		response = invalid
		before := requests
		result, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.NotContains(t, err.Error(), "private diagnostic")
		assert.Equal(t, before+1, requests, "failed preflight must not perform a mutation or retry")
	}
	response, status, redirect = goodResponse, http.StatusFound, server.URL+"/issues"
	before := requests
	_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", args)
	require.Error(t, err)
	assert.Equal(t, before+1, requests)
	runtime.AllowedOrigins = []string{"https://unrelated.example"}
	before = secrets
	_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", args)
	require.Error(t, err)
	assert.Equal(t, before, secrets)

	runtime.AllowedOrigins = []string{server.URL}
	for _, value := range []string{"", "malformed-uuid"} {
		runtime.ResolveSecret = func(context.Context, string, string) (string, error) { return value, nil }
		before := requests
		_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", args)
		require.Error(t, err)
		assert.Equal(t, before, requests)
	}
	runtime.ResolveSecret = func(context.Context, string, string) (string, error) {
		return "", errors.New("private secret diagnostic")
	}
	_, err = plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue", args)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "private secret")
}

func TestTrustedResourceConfigurationRejectsUnsafeAuthority(t *testing.T) {
	for _, change := range []func(*ConnectorSpec){
		func(spec *ConnectorSpec) {
			op := spec.SafeOutputs["create-issue"]
			op.TrustedInputs["team-id"] = TrustedInput{Secret: "${{ secrets.TEAM_ID }}"}
		},
		func(spec *ConnectorSpec) {
			op := spec.SafeOutputs["create-issue"]
			op.TrustedInputs["project-id"] = TrustedInput{Value: "${{ github.event.issue.title }}"}
		},
		func(spec *ConnectorSpec) {
			spec.SafeOutputs["create-issue"].Preflight.Request.GraphQL += " mutation Write { remove }"
		},
		func(spec *ConnectorSpec) { spec.SafeOutputs["create-issue"].Preflight.Request.Endpoint = "other" },
		func(spec *ConnectorSpec) {
			spec.SafeOutputs["create-issue"].Preflight.Inputs["agent-chosen"] = Input{Type: "string", Required: true, MaxLength: 10}
		},
	} {
		service := trustedInputService(t, "https://api.example.com")
		change(&service.spec)
		require.Error(t, service.spec.Validate())
	}
}
