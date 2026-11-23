package connectors

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInferenceProfileValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Inference, *Credential)
		error  string
	}{
		{"valid", func(*Inference, *Credential) {}, ""},
		{"unknown endpoint", func(p *Inference, _ *Credential) { p.Endpoint = "unknown" }, "unknown endpoint"},
		{"unknown protocol", func(p *Inference, _ *Credential) { p.Protocol = "anthropic-messages" }, "protocol"},
		{"traversal", func(p *Inference, _ *Credential) { p.Path = "/models/../admin" }, "fixed absolute"},
		{"escaped traversal", func(p *Inference, _ *Credential) { p.Path = "/%2e%2e/admin" }, "fixed absolute"},
		{"query", func(p *Inference, _ *Credential) { p.Path = "/v1?token=value" }, "fixed absolute"},
		{"expression", func(p *Inference, _ *Credential) { p.Path = "/${{ inputs.path }}" }, "fixed absolute"},
		{"no model grant", func(p *Inference, _ *Credential) { p.Models = "" }, "model resource"},
		{"agent credential", func(_ *Inference, c *Credential) { c.Phase = "agent" }, "inference-phase"},
		{"writer credential", func(_ *Inference, c *Credential) { c.Phase = "safe-output" }, "inference-phase"},
		{"api-key header", func(_ *Inference, c *Credential) { c.Header = "X-API-Key" }, "Authorization/bearer"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile := Inference{Endpoint: "models", Path: "/default/models/openai/v1", Protocol: "openai-responses", Credential: "inference", Models: "models"}
			credential := Credential{Phase: "inference", Header: "Authorization", Scheme: "bearer", Scopes: []string{"model runtime access"}}
			tc.change(&profile, &credential)
			spec := ConnectorSpec{
				APIVersion: APIVersion, Kind: "ConnectorSpec", ID: "example.com/inference",
				Endpoints:   map[string]string{"models": "https://gateway.example.com"},
				Credentials: map[string]Credential{"inference": credential},
				Inference:   map[string]Inference{"responses": profile},
			}
			err := spec.Validate()
			if tc.error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.error)
			}
		})
	}
}

func TestInferenceIsPrunedFromReaderAndWriterPlans(t *testing.T) {
	original := fixturePlan(t, "https://api.example.com")
	original.spec.Endpoints["inference"] = "https://gateway.example.com"
	original.spec.Credentials["model"] = Credential{Phase: "inference", Header: "Authorization", Scheme: "bearer", Scopes: []string{"inference"}}
	original.spec.Inference = map[string]Inference{
		"responses": {Endpoint: "inference", Path: "/v1", Protocol: "openai-responses", Credential: "model", Models: "models"},
	}
	original.connection.Credentials["model"] = SecretRef{Secret: "INFERENCE_ONLY_KEY"}
	original.connection.Resources["models"] = []string{"approved-model"}
	service, err := NewService(original.spec, original.connection)
	require.NoError(t, err)
	plan, err := service.Select(Selection{MCP: original.binding.MCP, SafeOutputs: original.binding.SafeOutputs})
	require.NoError(t, err)
	for _, phase := range []string{"agent", "safe-output"} {
		t.Run(phase, func(t *testing.T) {
			restricted, err := plan.ForPhase(phase)
			require.NoError(t, err)
			encoded, err := restricted.EncodeJSON()
			require.NoError(t, err)
			require.NotContains(t, encoded, "INFERENCE_ONLY_KEY")
			require.NotContains(t, encoded, "gateway.example.com")
			require.NotContains(t, encoded, "approved-model")
			require.Empty(t, restricted.spec.Inference)
			_, err = DecodePlan(encoded)
			require.NoError(t, err)
			runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
			require.NoError(t, err)
			command := exec.Command("node", "-e", "require(process.argv[1]).decodePlan(process.env.INFERENCE_PRUNING_PLAN)", runtime)
			command.Env = append(os.Environ(), "INFERENCE_PRUNING_PLAN="+encoded)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "pruned plan must retain Go/Node parity: %s", output)
		})
	}
}
