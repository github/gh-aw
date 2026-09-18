//go:build !integration

package parser

import "testing"

func TestMainWorkflowSchemaThreatDetectionArtifactBaseURL(t *testing.T) {
	t.Parallel()

	frontmatter := func(baseURL string) map[string]any {
		return map[string]any{
			"on":     "push",
			"engine": "copilot",
			"safe-outputs": map[string]any{
				"create-issue": nil,
				"threat-detection": map[string]any{
					"artifact-base-url": baseURL,
				},
			},
		}
	}

	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(
		frontmatter("https://artifacts.example.com/threat-detect/releases/download"),
		"/tmp/gh-aw/threat-detection-artifact-mirror-valid.md",
	); err != nil {
		t.Fatalf("expected HTTPS artifact mirror to validate: %v", err)
	}

	for _, invalid := range []string{
		"http://artifacts.example.com/threat-detect",
		"file:///tmp/threat-detect",
		"artifacts.example.com/threat-detect",
	} {
		if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(
			frontmatter(invalid),
			"/tmp/gh-aw/threat-detection-artifact-mirror-invalid.md",
		); err == nil {
			t.Errorf("expected unsafe artifact mirror %q to fail schema validation", invalid)
		}
	}
}
