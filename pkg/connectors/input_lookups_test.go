package connectors

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func inputLookupPlan(t *testing.T) *Plan {
	t.Helper()
	plan, err := fixturePlan(t, "https://api.example.com").ForPhase("safe-output")
	require.NoError(t, err)
	op := plan.spec.SafeOutputs["add-comment"]
	field := op.Inputs["issue"]
	field.Pattern = "^id-[0-9]+$"
	op.Inputs["issue"] = field
	op.Lookups = []InputLookup{{
		Input: "issue",
		TrustedPreflight: TrustedPreflight{
			Inputs:           map[string]Input{"issue": {Type: "string", Required: true, MaxLength: 32, Pattern: "^ENG-[0-9]+$"}},
			Resources:        map[string]string{"issue": "aliases"},
			Request:          Request{Endpoint: "api", Method: "GET", Path: "/lookup/{target}", PathInputs: map[string]string{"target": "issue"}},
			MaxResponseBytes: 1024, ResponseInputs: map[string]string{"/alias": "issue"},
			ResponseResources: map[string]string{"/id": "issues"}, Project: map[string]string{"issue": "/id"},
		},
	}}
	plan.spec.SafeOutputs["add-comment"] = op
	plan.connection.Resources = map[string][]string{"issues": {"id-1", "id-2"}, "aliases": {"ENG-123"}}
	return plan
}

func TestInputLookupDeclarationRejectionsInBothHosts(t *testing.T) {
	node, runtime := connectorNodeRuntime(t)
	for _, scenario := range []string{
		"too-long", "duplicate", "write", "different-endpoint", "unknown-endpoint",
		"missing-alias-grant", "same-alias-grant", "missing-result-grant", "missing-identity",
		"extra-projection", "relationship-projection", "cycle", "trusted", "receipts",
		"result-limit", "identity-pointers",
	} {
		t.Run(scenario, func(t *testing.T) {
			plan := inputLookupPlan(t)
			op := plan.spec.SafeOutputs["add-comment"]
			read := &op.Lookups[0]
			switch scenario {
			case "too-long":
				for len(op.Lookups) < 5 {
					op.Lookups = append(op.Lookups, *read)
				}
			case "duplicate":
				op.Lookups = append(op.Lookups, *read)
			case "write":
				read.Request.Method = "POST"
				read.Request.GraphQL = "mutation Change { id }"
			case "different-endpoint":
				plan.spec.Endpoints["other"] = "https://other.example.com"
				read.Request.Endpoint = "other"
			case "unknown-endpoint":
				read.Request.Endpoint = "missing"
			case "missing-alias-grant":
				read.Resources = nil
			case "same-alias-grant":
				read.Resources["issue"] = "issues"
			case "missing-result-grant":
				read.ResponseResources = nil
			case "missing-identity":
				read.ResponseInputs = nil
			case "extra-projection":
				read.Project["text"] = "/id"
			case "relationship-projection":
				read.Input = ""
			case "cycle":
				op.Inputs["other"], op.Resources["other"] = op.Inputs["issue"], "issues"
				read.Inputs["other"] = op.Inputs["other"]
				next := *read
				next.Input = "other"
				next.Inputs = map[string]Input{"other": read.Inputs["issue"], "issue": op.Inputs["issue"]}
				next.Resources = map[string]string{"other": "aliases"}
				next.Project = map[string]string{"other": "/id"}
				next.ResponseInputs = map[string]string{"/alias": "other"}
				next.Request = Request{Endpoint: "api", Method: "GET", Path: "/lookup"}
				op.Lookups = append(op.Lookups, next)
			case "trusted":
				op.TrustedInputs = map[string]TrustedInput{"issue": {Value: "id-1"}}
			case "receipts":
				op.CreatedResources = map[string]string{"issue": "create"}
			case "result-limit":
				read.ResponseLimits = map[string]int{"/nodes": 101}
			case "identity-pointers":
				read.ResponseAnyInputs = map[string][]string{"issue": {"/a", "/b", "/c", "/d", "/e"}}
			}
			plan.spec.SafeOutputs["add-comment"] = op
			require.Error(t, plan.spec.Validate())
			frozen, err := freezePlan(plan.spec, plan.connection, plan.binding)
			require.NoError(t, err)
			encoded, err := frozen.Encode()
			require.NoError(t, err)
			command := exec.CommandContext(t.Context(), node, "-e", `
const runtime = require(process.argv[1]);
try { runtime.decodePlan(require("node:fs").readFileSync(0, "utf8")); process.exitCode = 1; }
catch { process.stdout.write("rejected"); }
`, runtime)
			command.Stdin = strings.NewReader(encoded)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.Equal(t, "rejected", string(output))
		})
	}
}

func TestInputLookupStagingRevalidatesPlanAndGrants(t *testing.T) {
	original := inputLookupPlan(t)
	require.NoError(t, original.spec.Validate())
	service, err := NewService(original.spec, original.connection)
	require.NoError(t, err)
	plan, err := service.Select(Selection{SafeOutputs: []string{"add-comment"}})
	require.NoError(t, err)
	plan, err = plan.ForPhase("safe-output")
	require.NoError(t, err)
	args := map[string]any{"issue": "ENG-123", "text": "note"}
	request, err := plan.PrepareSafeOutput("add-comment", args)
	require.NoError(t, err)
	require.Equal(t, []string{"issue"}, request.UnresolvedInputs)
	require.Equal(t, "ENG-123", request.Arguments["issue"])
	args["issue"] = "ENG-999"
	require.Equal(t, "ENG-123", request.Arguments["issue"], "intent owns a frozen argument copy")
	secretCalls := 0
	runtime := Runtime{Phase: "safe-output", AllowWrites: true, AllowedOrigins: plan.Origins(),
		ResolveSecret: func(context.Context, string, string) (string, error) {
			secretCalls++
			return "", errors.New("must not resolve")
		}}
	_, err = plan.Execute(t.Context(), runtime, "safe-output", "add-comment", args)
	require.Error(t, err)
	require.Zero(t, secretCalls)
	encoded, err := plan.Encode()
	require.NoError(t, err)
	require.Equal(t, original.connection.Resources, plan.connection.Resources)
	decoded, err := DecodePlan(encoded)
	require.NoError(t, err)
	request.Digest = strings.Repeat("0", 64)
	_, err = decoded.ExecuteSafeOutput(t.Context(), runtime, *request)
	require.ErrorContains(t, err, "digest")
	require.Zero(t, secretCalls)
}
