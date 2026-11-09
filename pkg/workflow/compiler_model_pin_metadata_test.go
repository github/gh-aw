package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelPinMetadataProjection(t *testing.T) {
	engine := map[string]any{
		"id": "custom", "runtime": map[string]any{"id": "copilot", "command": "not inventory"},
		"model": "small", "models": map[string]any{"default": "small", "supported": []any{"large"}},
		"fallback-models": []any{"large"}, "model-provider": "copilot", "detection-engine": "claude",
		"provider":      map[string]any{"id": "copilot", "name": "copilot", "model": "small", "command": "not inventory"},
		"behaviors":     map[string]any{"execute": map[string]any{"run": "not inventory"}},
		"env":           map[string]any{"COPILOT_PROVIDER_BASE_URL": "custom", "OTHER": "not inventory"},
		"model-routing": map[string]any{"allowed-models": []any{"small"}, "run": "not inventory"},
	}
	input := map[string]any{
		"engine": engine, "model": "main", "tools": map[string]any{"bash": []any{"irrelevant"}},
		"models":  map[string]any{"alias": []any{"small"}, "providers": map[string]any{"large": "not inventory"}},
		"imports": []any{"shared.md"}, "safe-outputs": map[string]any{
			"threat-detection": map[string]any{"enabled": false, "model": "detect", "engine": engine, "engine-config": engine, "prompt": "not inventory"},
			"assign-to-agent":  map[string]any{"model": "auto", "instructions": "not inventory"},
		},
		"evals":    map[string]any{"model": "grade", "questions": []any{map[string]any{"model": "one", "question": "not inventory"}, map[string]any{"question": "not inventory"}}},
		"enclaves": []any{map[string]any{"agent": map[string]any{"model": "isolated", "engine": "copilot", "prompt": "not inventory"}}},
	}
	out := projectModelPinMetadata(input).(map[string]any)
	require.NotContains(t, out, "tools")
	require.Equal(t, map[string]any{"alias": []any{"small"}}, out["models"])
	e := out["engine"].(map[string]any)
	require.Equal(t, map[string]any{}, e["behaviors"])
	require.Equal(t, map[string]any{"id": "copilot"}, e["runtime"])
	require.Equal(t, map[string]any{"COPILOT_PROVIDER_BASE_URL": "custom"}, e["env"])
	require.Equal(t, map[string]any{"allowed-models": []any{"small"}}, e["model-routing"])
	require.Equal(t, []any{"large"}, e["fallback-models"])
	require.Equal(t, map[string]any{"model": "grade", "questions": []any{map[string]any{"model": "one"}, map[string]any{}}}, out["evals"])
	require.Equal(t, []any{map[string]any{"agent": map[string]any{"model": "isolated", "engine": "copilot"}}}, out["enclaves"])
	require.Equal(t, false, out["safe-outputs"].(map[string]any)["threat-detection"].(map[string]any)["enabled"])
}
