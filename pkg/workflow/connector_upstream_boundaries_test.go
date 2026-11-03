//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestConnectorArtifactUploadRequiresCompletedRedaction(t *testing.T) {
	compiler, data, path := parseAzureDevOpsConsumer(t, "safe-outputs:\n  staged: true\n  azure-devops-upload-attachment:\n")
	data.SecretMasking = &SecretMaskingConfig{Steps: []map[string]any{
		{"id": "connector_fixture_mask", "continue-on-error": true, "run": "exit 1"},
	}}
	require.True(t, usesSafeOutputsArtifactStaging(data.SafeOutputs))
	require.Nil(t, data.SafeOutputs.UploadArtifact, "a connector upload must not expose public upload_artifact")
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	var document struct {
		Jobs map[string]struct {
			Steps []struct {
				ID   string `yaml:"id"`
				Name string `yaml:"name"`
				If   string `yaml:"if"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &document))
	seen := map[string]bool{}
	foundStaging := false
	agentUploads := 0
	for _, step := range document.Jobs["agent"].Steps {
		if step.ID != "" {
			seen[step.ID] = true
		}
		ids := []string{"redact_secrets", "connector_fixture_mask", "validate_recovery_envelopes"}
		switch step.Name {
		case "Upload upload-artifact staging":
			foundStaging = true
		case "Upload agent artifacts", "Upload agent output fallback artifact":
			agentUploads++
			ids = append(ids, "validate_final_recovery_envelopes")
		default:
			continue
		}
		outcomes := map[string]string{}
		for _, id := range ids {
			require.True(t, seen[id], "%s must precede %s", id, step.Name)
			require.Contains(t, step.If, "steps."+id+".outcome == 'success'")
			outcomes["steps."+id+".outcome"] = "success"
		}
		condition, err := ParseExpression(step.If)
		require.NoError(t, err)
		require.True(t, evaluateRedactionUploadGate(t, condition, outcomes))
		for property := range outcomes {
			for _, outcome := range []string{"failure", "skipped", "cancelled", ""} {
				outcomes[property] = outcome
				require.False(t, evaluateRedactionUploadGate(t, condition, outcomes))
			}
			outcomes[property] = "success"
		}
	}
	require.True(t, foundStaging, "the selected binary effect needs its bounded staging channel")
	require.Equal(t, 2, agentUploads)
}

func TestConnectorDeployedCJSDependencies(t *testing.T) {
	setup, err := os.ReadFile("../../actions/setup/setup.sh")
	require.NoError(t, err)
	for _, group := range []string{"MCP_SCRIPTS_FILES", "SAFE_OUTPUTS_FILES"} {
		t.Run(group, func(t *testing.T) {
			roots := []string{"connector_runtime.cjs", "connector_effect_bindings.cjs", "connector_webhook.cjs"}
			if group == "SAFE_OUTPUTS_FILES" {
				roots = append(roots, "connector_content.cjs")
			}
			required := connectorCJSDependencies(t, roots)
			_, section, found := strings.Cut(string(setup), group+"=(")
			require.True(t, found)
			section, _, found = strings.Cut(section, "\n)")
			require.True(t, found)
			for _, name := range required {
				require.Contains(t, section, `"`+name+`"`, "compiled connector hosts must ship their transitive shared modules")
			}
		})
	}
}

func connectorCJSDependencies(t *testing.T, roots []string) []string {
	t.Helper()
	pattern := regexp.MustCompile(`\brequire\(["']\./[^"']+\.cjs["']\)`)
	seen := map[string]bool{}
	var required []string
	var visit func(string)
	visit = func(name string) {
		if seen[name] {
			return
		}
		require.False(t, name == ".." || strings.HasPrefix(name, "../"))
		seen[name] = true
		required = append(required, name)
		source, err := os.ReadFile(filepath.Join("../../actions/setup/js", name))
		require.NoError(t, err)
		for _, expression := range pattern.FindAllString(string(source), -1) {
			local := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(expression, "require("), ")"), `"'`)
			visit(filepath.ToSlash(filepath.Join(filepath.Dir(name), local)))
		}
	}
	for _, root := range roots {
		visit(root)
	}
	return required
}
