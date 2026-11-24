//go:build !integration

package parser

import "testing"

func TestMainWorkflowSchemaThreatDetectionCACert(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		value any
		valid bool
	}{
		{name: "absolute certificate path", value: "/tmp/detection-ca.pem", valid: true},
		{name: "empty path", value: "", valid: true},
		{name: "explicitly disable inheritance", value: false, valid: true},
		{name: "relative path rejected", value: "tmp/detection-ca.pem"},
		{name: "boolean rejected", value: true},
		{name: "number rejected", value: 42},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on":     "push",
				"engine": "claude",
				"safe-outputs": map[string]any{
					"create-issue":     nil,
					"threat-detection": map[string]any{"ca-cert": tt.value},
				},
			}, "/tmp/gh-aw/threat-detection-ca-cert.md")
			if (err == nil) != tt.valid {
				t.Errorf("validation error = %v, want valid=%v", err, tt.valid)
			}
		})
	}
}

func TestMainWorkflowSchemaThreatDetectionEngineVersion(t *testing.T) {
	t.Parallel()

	detection := map[string]any{"engine": map[string]any{"id": "copilot", "version": "1.2.3"}}
	frontmatter := map[string]any{
		"on":     "push",
		"engine": "copilot",
		"safe-outputs": map[string]any{
			"create-issue": nil, "threat-detection": detection,
		},
	}
	const path = "threat-detection.md"
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, path); err != nil {
		t.Fatalf("detection engine version should validate: %v", err)
	}
	detection["artifact-base-url"] = "https://artifacts.example.com/detector"
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, path); err == nil {
		t.Fatal("obsolete detector release mirror should not validate")
	}
}
