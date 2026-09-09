package workflow

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
)

// TestParseGradersFromFrontmatter_Absent verifies nil return when graders absent.
func TestParseGradersFromFrontmatter_Absent(t *testing.T) {
	var c Compiler
	cfg, err := c.parseGradersFromFrontmatter(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when graders absent")
	}
}

// TestParseGradersFromFrontmatter_Nil verifies nil return when graders is nil.
func TestParseGradersFromFrontmatter_Nil(t *testing.T) {
	var c Compiler
	cfg, err := c.parseGradersFromFrontmatter(map[string]any{"graders": nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when graders is nil")
	}
}

// TestParseGradersFromFrontmatter_ZeroConfig verifies {} enables all built-ins.
func TestParseGradersFromFrontmatter_ZeroConfig(t *testing.T) {
	var c Compiler
	cfg, err := c.parseGradersFromFrontmatter(map[string]any{"graders": map[string]any{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if !cfg.HasGraders() {
		t.Fatal("expected HasGraders to be true")
	}
	ids := cfg.EnabledGraderIDs()
	if len(ids) != len(BuiltinGraderIDs) {
		t.Fatalf("expected %d enabled graders, got %d", len(BuiltinGraderIDs), len(ids))
	}
}

func TestBuiltinGraderRegistry_WorkingSetRebuildFactor(t *testing.T) {
	meta, ok := builtinGraderMetaByID["working-set-rebuild-factor"]
	if !ok {
		t.Fatal("expected working-set-rebuild-factor to be registered")
	}
	if meta.Unit != "factor" {
		t.Fatalf("expected factor unit, got %s", meta.Unit)
	}
	if meta.Direction != "lower_is_better" {
		t.Fatalf("expected lower_is_better direction, got %s", meta.Direction)
	}
	if meta.Min == nil || *meta.Min != 1 {
		t.Fatalf("expected minimum value 1, got %v", meta.Min)
	}
}

// TestParseGradersFromFrontmatter_DisableOne verifies selective disable.
func TestParseGradersFromFrontmatter_DisableOne(t *testing.T) {
	var c Compiler
	cfg, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"loops": map[string]any{"enabled": false},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	ids := cfg.EnabledGraderIDs()
	for _, id := range ids {
		if id == "loops" {
			t.Fatal("loops should be disabled")
		}
	}
	// Should have all built-ins minus loops
	if len(ids) != len(BuiltinGraderIDs)-1 {
		t.Fatalf("expected %d enabled graders, got %d", len(BuiltinGraderIDs)-1, len(ids))
	}
}

// TestParseGradersFromFrontmatter_CustomGrader verifies custom grader with script.
func TestParseGradersFromFrontmatter_CustomGrader(t *testing.T) {
	var c Compiler
	cfg, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"my-metric": map[string]any{
				"script": "trace.toolCalls.length",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	g, ok := cfg.Graders["my-metric"]
	if !ok {
		t.Fatal("expected my-metric grader")
	}
	if g.Script != "trace.toolCalls.length" {
		t.Fatalf("unexpected script: %s", g.Script)
	}
}

func TestParseGradersFromFrontmatter_OperationalValueGrader(t *testing.T) {
	var c Compiler
	for _, runPath := range []string{
		".github/workflows/graders/example-operational-value.sh",
		".github/workflows/graders/..secret.sh",
		"./graders/example-operational-value.sh",
		"scripts/example-operational-value.sh",
	} {
		t.Run(runPath, func(t *testing.T) {
			cfg, err := c.parseGradersFromFrontmatter(map[string]any{
				"graders": map[string]any{
					"operational-value": map[string]any{
						"run": runPath,
					},
				},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			grader := cfg.Graders["operational-value"]
			if grader.Run != runPath {
				t.Fatalf("unexpected operational-value run path: %q", grader.Run)
			}
			if grader.Unit != "ratio" || grader.Direction != "higher_is_better" {
				t.Fatalf("unexpected operational-value defaults: unit=%q direction=%q", grader.Unit, grader.Direction)
			}
			if grader.Min == nil || *grader.Min != 0 || grader.Max == nil || *grader.Max != 1 {
				t.Fatalf("expected operational-value range [0,1], got min=%v max=%v", grader.Min, grader.Max)
			}
		})
	}
}

func TestParseGradersFromFrontmatter_OperationalValueGraderValidation(t *testing.T) {
	var c Compiler
	tests := []struct {
		name    string
		entry   map[string]any
		errText string
	}{
		{name: "missing run", entry: map[string]any{}, errText: "requires a 'run' field"},
		{name: "path traversal", entry: map[string]any{"run": ".github/workflows/graders/../secret.sh"}, errText: "workspace-relative"},
		{name: "absolute path", entry: map[string]any{"run": "/tmp/operational-value.sh"}, errText: "workspace-relative"},
		{name: "wrong extension", entry: map[string]any{"run": ".github/workflows/graders/operational-value.js"}, errText: "workspace-relative"},
		{name: "inline script", entry: map[string]any{"run": ".github/workflows/graders/operational-value.sh", "script": "return 1"}, errText: "cannot have an inline script"},
		{name: "direction", entry: map[string]any{"run": ".github/workflows/graders/operational-value.sh", "direction": "lower_is_better"}, errText: "direction must be 'higher_is_better'"},
		{name: "minimum", entry: map[string]any{"run": ".github/workflows/graders/operational-value.sh", "min": 0.1}, errText: "range must be min: 0 and max: 1"},
		{name: "maximum", entry: map[string]any{"run": ".github/workflows/graders/operational-value.sh", "max": 2.0}, errText: "range must be min: 0 and max: 1"},
		{name: "threshold below range", entry: map[string]any{"run": ".github/workflows/graders/operational-value.sh", "threshold": -0.1}, errText: "threshold must be between 0 and 1"},
		{name: "threshold above range", entry: map[string]any{"run": ".github/workflows/graders/operational-value.sh", "threshold": 1.1}, errText: "threshold must be between 0 and 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := c.parseGradersFromFrontmatter(map[string]any{
				"graders": map[string]any{"operational-value": test.entry},
			})
			if err == nil || !strings.Contains(err.Error(), test.errText) {
				t.Fatalf("expected error containing %q, got %v", test.errText, err)
			}
		})
	}
}

func TestIsValidOperationalValueEvaluatorRunPath(t *testing.T) {
	t.Parallel()

	validCases := []string{
		"evaluator.sh",
		".github/workflows/graders/test.sh",
		"./graders/test.sh",
		"./evaluator.sh",
		"scripts/nested/evaluator.sh",
	}
	for _, tc := range validCases {
		t.Run("valid_"+tc, func(t *testing.T) {
			assert.True(t, IsValidOperationalValueEvaluatorRunPath(tc), "expected %q to be valid", tc)
		})
	}

	invalidCases := []string{
		"",
		"/abs/path.sh",
		"//evil.com/test.sh",
		"\\win\\path.sh",
		"path\\with\\backslash.sh",
		".",
		"..",
		"./",
		"../test.sh",
		"./../test.sh",
		".github/workflows/../secret.sh",
		".github/workflows//double_slash.sh",
		"evaluator.js",
		"evaluator.sh/",
		"./.",
		"./..",
		"./evaluator.js",
	}
	for _, tc := range invalidCases {
		t.Run("invalid_"+tc, func(t *testing.T) {
			assert.False(t, IsValidOperationalValueEvaluatorRunPath(tc), "expected %q to be invalid", tc)
		})
	}
}

func TestParseGradersFromFrontmatter_RunRejectedForOtherGraders(t *testing.T) {
	var c Compiler
	_, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"custom": map[string]any{
				"run": ".github/workflows/graders/operational-value.sh",
			},
		},
	})
	if err == nil {
		t.Fatal("expected run to be rejected for a non-operational-value grader")
	}
}

func TestMergeImportedGradersFrontmatter(t *testing.T) {
	frontmatter := map[string]any{
		"graders": map[string]any{
			"local-score": map[string]any{
				"script": "return 2",
			},
			"shared-score": map[string]any{
				"script": "return 3",
			},
		},
	}
	importedGraders := `{"shared-score":{"script":"return 1","unit":"count","direction":"lower_is_better"}}`

	merged, err := mergeImportedGradersFrontmatter(frontmatter, importedGraders)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var c Compiler
	cfg, err := c.parseGradersFromFrontmatter(merged)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected merged graders config")
	}
	if _, ok := cfg.Graders["local-score"]; !ok {
		t.Fatal("expected local-score grader")
	}
	if got := cfg.Graders["shared-score"].Script; got != "return 3" {
		t.Fatalf("expected local shared-score override, got %q", got)
	}
}

func TestCompileWorkflowMergesImportedGraders(t *testing.T) {
	tmpDir := t.TempDir()
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("failed to create shared dir: %v", err)
	}

	sharedPath := filepath.Join(sharedDir, "grader.md")
	sharedContent := `---
graders:
  imported-score:
    script: |
      return trace.toolCalls.length
    unit: count
    direction: lower_is_better
---

<!-- imported grader -->
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0o644); err != nil {
		t.Fatalf("failed to write shared grader: %v", err)
	}

	workflowPath := filepath.Join(tmpDir, "workflow.md")
	workflowContent := `---
on: workflow_dispatch
engine: copilot
strict: false
permissions:
  contents: read
imports:
  - shared/grader.md
---

# Workflow
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o644); err != nil {
		t.Fatalf("failed to write workflow: %v", err)
	}

	compiler := NewCompiler(WithVersion("dev"))
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow() error = %v", err)
	}

	compiled, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	if err != nil {
		t.Fatalf("failed to read compiled workflow: %v", err)
	}
	yaml := string(compiled)
	if !strings.Contains(yaml, "Run graders") {
		t.Fatal("expected imported graders to emit the grader step")
	}

	match := regexp.MustCompile(`await main\('([^']+)', '([^']+)'\);`).FindStringSubmatch(yaml)
	if len(match) != 3 {
		t.Fatal("expected encoded grader manifest and execution spec in compiled workflow")
	}
	manifestJSON, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		t.Fatalf("failed to decode manifest: %v", err)
	}
	var manifest graderManifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		t.Fatalf("failed to unmarshal manifest: %v", err)
	}
	found := false
	for _, entry := range manifest.Graders {
		if entry.ID == "imported-score" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected imported grader ID in compiled manifest")
	}
}

// TestParseGradersFromFrontmatter_InvalidType verifies error for wrong type.
func TestParseGradersFromFrontmatter_InvalidType(t *testing.T) {
	var c Compiler
	_, err := c.parseGradersFromFrontmatter(map[string]any{"graders": "invalid"})
	if err == nil {
		t.Fatal("expected error for string graders value")
	}
}

// TestParseGradersFromFrontmatter_ForbiddenScript verifies forbidden patterns in scripts.
func TestParseGradersFromFrontmatter_ForbiddenScript(t *testing.T) {
	var c Compiler
	forbidden := []string{
		"require('fs')",
		"import('os')",
		"fetch('http://evil.com')",
		"eval('bad')",
		"process.exit(1)",
	}
	for _, script := range forbidden {
		_, err := c.parseGradersFromFrontmatter(map[string]any{
			"graders": map[string]any{
				"bad-grader": map[string]any{"script": script},
			},
		})
		if err == nil {
			t.Fatalf("expected error for forbidden script: %s", script)
		}
		if !strings.Contains(err.Error(), "forbidden pattern") {
			t.Fatalf("expected forbidden pattern error, got: %v", err)
		}
	}
}

// TestParseGradersFromFrontmatter_BuiltinScriptRejected verifies built-in cannot have script.
func TestParseGradersFromFrontmatter_BuiltinScriptRejected(t *testing.T) {
	var c Compiler
	_, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"retries": map[string]any{"script": "1 + 1"},
		},
	})
	if err == nil {
		t.Fatal("expected error for built-in with script")
	}
}

// TestParseGradersFromFrontmatter_CustomWithoutScript verifies custom requires script.
func TestParseGradersFromFrontmatter_CustomWithoutScript(t *testing.T) {
	var c Compiler
	_, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"no-script": map[string]any{},
		},
	})
	if err == nil {
		t.Fatal("expected error for custom grader without script")
	}
}

// TestParseGradersFromFrontmatter_CustomNullRejected verifies null custom graders are rejected at parse time.
func TestParseGradersFromFrontmatter_CustomNullRejected(t *testing.T) {
	var c Compiler
	_, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"my-custom": nil,
		},
	})
	if err == nil {
		t.Fatal("expected error for null custom grader")
	}
	if !strings.Contains(err.Error(), "requires a 'script' field") {
		t.Fatalf("expected missing-script error, got: %v", err)
	}
}

// TestParseGradersFromFrontmatter_ScriptLengthUsesCharacters verifies limits align to character count.
func TestParseGradersFromFrontmatter_ScriptLengthUsesCharacters(t *testing.T) {
	var c Compiler
	validMultibyteScript := "return '" + strings.Repeat("é", 1366) + "'.length"
	if _, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"unicode-ok": map[string]any{"script": validMultibyteScript},
		},
	}); err != nil {
		t.Fatalf("expected multibyte script under 4096 characters to pass, got: %v", err)
	}

	tooLongScript := "return '" + strings.Repeat("a", 4097) + "'.length"
	_, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"too-long": map[string]any{"script": tooLongScript},
		},
	})
	if err == nil {
		t.Fatal("expected error for script longer than 4096 characters")
	}
	if !strings.Contains(err.Error(), "maximum length of 4096 characters") {
		t.Fatalf("expected script-length error, got: %v", err)
	}
}

// TestParseGradersFromFrontmatter_InvalidID verifies ID validation.
func TestParseGradersFromFrontmatter_InvalidID(t *testing.T) {
	var c Compiler
	_, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"UPPER_CASE": map[string]any{"script": "1"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

// TestParseGradersFromFrontmatter_DuplicateNormalizedID verifies whitespace variants are rejected.
func TestParseGradersFromFrontmatter_DuplicateNormalizedID(t *testing.T) {
	var c Compiler
	_, err := c.parseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"retry-test":  map[string]any{"script": "return 1"},
			" retry-test": map[string]any{"script": "return 2"},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate normalized id error")
	}
	if !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("expected duplicate id error, got: %v", err)
	}
}

// TestParseGradersFromFrontmatter_AllDisabledError verifies error when all disabled.
func TestParseGradersFromFrontmatter_AllDisabledError(t *testing.T) {
	var c Compiler
	graders := map[string]any{}
	for _, id := range BuiltinGraderIDs {
		graders[id] = map[string]any{"enabled": false}
	}
	_, err := c.parseGradersFromFrontmatter(map[string]any{"graders": graders})
	if err == nil {
		t.Fatal("expected error when all graders disabled")
	}
}

// TestGradersConfig_EnabledGraderIDs_Order verifies stable ordering.
func TestGradersConfig_EnabledGraderIDs_Order(t *testing.T) {
	cfg := &GradersConfig{
		Graders: map[string]*GraderDefinition{
			"zebra-metric":      {ID: "zebra-metric", Script: "1"},
			"alpha-metric":      {ID: "alpha-metric", Script: "1"},
			"retries":           {ID: "retries"},
			"tool-success-rate": {ID: "tool-success-rate"},
		},
	}
	ids := cfg.EnabledGraderIDs()
	// Built-ins first in canonical order, then custom alphabetically
	if ids[0] != "tool-success-rate" {
		t.Fatalf("expected tool-success-rate first, got %s", ids[0])
	}
	if ids[1] != "retries" {
		t.Fatalf("expected retries second, got %s", ids[1])
	}
	if ids[2] != "alpha-metric" {
		t.Fatalf("expected alpha-metric third, got %s", ids[2])
	}
	if ids[3] != "zebra-metric" {
		t.Fatalf("expected zebra-metric fourth, got %s", ids[3])
	}
}

// TestBuildGraderManifest verifies manifest serialization.
func TestBuildGraderManifest(t *testing.T) {
	enabled := true
	disabled := false
	cfg := &GradersConfig{
		Graders: map[string]*GraderDefinition{
			"tool-success-rate": {ID: "tool-success-rate", Enabled: &enabled},
			"retries":           {ID: "retries", Enabled: &disabled},
			"my-custom":         {ID: "my-custom", Script: "trace.toolCalls.length"},
		},
	}
	entries := buildGraderManifest(cfg)
	if entries == nil {
		t.Fatal("expected non-nil manifest")
	}
	if entries.Version != 1 {
		t.Fatalf("expected version 1, got %d", entries.Version)
	}
	if len(entries.Graders) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries.Graders))
	}

	// Verify JSON serialization round-trips
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("json marshal error: %v", err)
	}
	var decoded graderManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if decoded.Version != 1 {
		t.Fatalf("expected decoded version 1, got %d", decoded.Version)
	}
	if len(decoded.Graders) != 3 {
		t.Fatalf("expected 3 decoded entries, got %d", len(decoded.Graders))
	}
}

func TestBuildGraderManifest_OperationalValueGrader(t *testing.T) {
	grader := &GraderDefinition{
		ID:  "operational-value",
		Run: ".github/workflows/graders/example-operational-value.sh",
	}
	grader.evaluatorContent = "#!/usr/bin/env bash\necho '{}'\n"
	cfg := &GradersConfig{Graders: map[string]*GraderDefinition{"operational-value": grader}}

	manifest := buildGraderManifest(cfg)
	if len(manifest.Graders) != 1 {
		t.Fatalf("expected one grader, got %d", len(manifest.Graders))
	}
	if manifest.Graders[0].Source != "operational-value" {
		t.Fatalf("expected operational-value source, got %q", manifest.Graders[0].Source)
	}
	if manifest.Graders[0].Digest != grader.EvaluatorDigest() {
		t.Fatalf("expected frozen evaluator digest, got %q", manifest.Graders[0].Digest)
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal operational-value manifest: %v", err)
	}
	if !strings.Contains(string(manifestJSON), `"run":".github/workflows/graders/example-operational-value.sh"`) || strings.Contains(string(manifestJSON), `"evaluator"`) {
		t.Fatalf("expected manifest to use run field, got %s", manifestJSON)
	}

	execSpec := buildGraderExecSpec(cfg)
	if len(execSpec) != 1 || execSpec[0].Run != grader.evaluatorContent {
		t.Fatal("expected frozen evaluator in execution spec")
	}
	if execSpec[0].Script != "" {
		t.Fatal("operational-value grader must not be serialized as an inline script")
	}
	execSpecJSON, err := json.Marshal(execSpec)
	if err != nil {
		t.Fatalf("marshal operational-value execution spec: %v", err)
	}
	if !strings.Contains(string(execSpecJSON), `"run":`) || strings.Contains(string(execSpecJSON), `"evaluator"`) {
		t.Fatalf("expected execution spec to use run field, got %s", execSpecJSON)
	}
}

// TestGenerateGradersStep_Absent verifies no step when graders nil.
func TestGenerateGradersStep_Absent(t *testing.T) {
	c := &Compiler{}
	var yaml strings.Builder
	data := &WorkflowData{Graders: nil}
	c.generateGradersStep(&yaml, data)
	if yaml.Len() != 0 {
		t.Fatal("expected no output when graders nil")
	}
}

// TestGenerateGradersStep_Present verifies step is emitted.
func TestGenerateGradersStep_Present(t *testing.T) {
	c := &Compiler{}
	initActionPinCacheForTest(c)
	var yaml strings.Builder
	data := &WorkflowData{
		Graders: &GradersConfig{
			Graders: map[string]*GraderDefinition{
				"retries": {ID: "retries"},
			},
		},
	}
	c.generateGradersStep(&yaml, data)
	output := yaml.String()
	if !strings.Contains(output, "Run graders") {
		t.Fatal("expected step name 'Run graders'")
	}
	if !strings.Contains(output, "if: always()") {
		t.Fatal("expected always() condition")
	}
	if !strings.Contains(output, "trace_graders.cjs") {
		t.Fatal("expected trace_graders.cjs require")
	}
	if !strings.Contains(output, "actions/github-script") {
		t.Fatal("expected actions/github-script usage")
	}
	if !strings.Contains(output, "await main('") {
		t.Fatal("expected main invocation with encoded payloads")
	}
	if strings.Contains(output, "{\"version\"") {
		t.Fatal("expected manifest to be encoded, not embedded as raw JSON")
	}
}

func TestGenerateGradersStep_OperationalValueUsesActivationRunMetadata(t *testing.T) {
	c := &Compiler{}
	initActionPinCacheForTest(c)
	var yaml strings.Builder
	data := operationalValueGraderWorkflowData(".github/workflows/graders/example-operational-value.sh")

	c.generateGradersStep(&yaml, data)

	output := yaml.String()
	assert.Contains(t, output, "GH_AW_RUN_CREATED_AT: ${{ needs.activation.outputs.run_created_at }}")
	assert.Contains(t, output, "GH_TOKEN: ${{ github.token }}")
	assert.NotContains(t, output, "getWorkflowRun")
}

func TestGenerateGradersStep_LargeOperationalValueEvaluatorStaysWithinExpressionLimit(t *testing.T) {
	c := &Compiler{}
	initActionPinCacheForTest(c)
	var yaml strings.Builder
	grader := &GraderDefinition{ID: "operational-value"}
	grader.evaluatorContent = "#!/usr/bin/env bash\n" + strings.Repeat("printf value\\n\n", 1800)
	data := &WorkflowData{Graders: &GradersConfig{Graders: map[string]*GraderDefinition{
		"operational-value": grader,
	}}}

	c.generateGradersStep(&yaml, data)

	assert.Contains(t, yaml.String(), "await main(graderManifestB64, graderExecB64)")
	for lineNumber, line := range strings.Split(yaml.String(), "\n") {
		if len(line) > MaxExpressionSize {
			t.Fatalf("generated line %d is %d bytes; maximum is %d", lineNumber+1, len(line), MaxExpressionSize)
		}
	}
}

// TestGenerateGradersStep_BeforeArtifactUpload verifies ordering.
func TestGenerateGradersStep_BeforeArtifactUpload(t *testing.T) {
	c := &Compiler{}
	initActionPinCacheForTest(c)
	var yaml strings.Builder

	data := &WorkflowData{
		Graders: &GradersConfig{
			Graders: map[string]*GraderDefinition{
				"retries": {ID: "retries"},
			},
		},
	}

	// Simulate the ordering: graders step then artifact upload
	c.generateGradersStep(&yaml, data)
	yaml.WriteString("      - name: Upload agent artifacts\n")

	output := yaml.String()
	graderIdx := strings.Index(output, "Run graders")
	uploadIdx := strings.Index(output, "Upload agent artifacts")
	if graderIdx < 0 || uploadIdx < 0 {
		t.Fatal("expected both steps to be present")
	}
	if graderIdx >= uploadIdx {
		t.Fatal("graders step must come before artifact upload")
	}
}

// TestCollectGraderArtifactPaths verifies paths include all replay artifacts.
func TestCollectGraderArtifactPaths(t *testing.T) {
	grader := &GraderDefinition{ID: "operational-value"}
	grader.evaluatorContent = "#!/usr/bin/env bash\n"
	paths := collectGraderArtifactPaths(&GradersConfig{
		Graders: map[string]*GraderDefinition{"operational-value": grader},
	})
	if len(paths) != 4 {
		t.Fatalf("expected 4 paths, got %d", len(paths))
	}
	if !strings.Contains(paths[0], "grader_manifest.json") {
		t.Fatal("expected grader_manifest.json in paths")
	}
	if !strings.Contains(paths[1], "grader_payload.json") {
		t.Fatal("expected grader_payload.json in paths")
	}
	if !strings.Contains(paths[2], "grader_results.json") {
		t.Fatal("expected grader_results.json in paths")
	}
	if !strings.Contains(paths[3], "operational_value_evaluator.sh") {
		t.Fatal("expected operational_value_evaluator.sh in paths")
	}
}

func TestCollectGraderArtifactPathsWithoutOperationalValue(t *testing.T) {
	paths := collectGraderArtifactPaths(&GradersConfig{
		Graders: map[string]*GraderDefinition{"retries": {ID: "retries"}},
	})
	if len(paths) != 3 {
		t.Fatalf("expected manifest, payload, and results paths, got %v", paths)
	}
}

// initActionPinCacheForTest sets up minimal action pin resolution for tests.
func initActionPinCacheForTest(c *Compiler) {
	// The Compiler uses getActionPin/getCachedActionPin which resolves from a global
	// cache. In tests, we just verify the step generation logic, the pin is tested separately.
}

// TestCollectGraderArtifactPaths_AgentGradersDir verifies paths use the agent/graders subdirectory.
func TestCollectGraderArtifactPaths_AgentGradersDir(t *testing.T) {
	paths := collectGraderArtifactPaths(nil)
	for _, p := range paths {
		if !strings.Contains(p, "agent/graders/") {
			t.Errorf("expected path to contain agent/graders/, got %q", p)
		}
	}
}

// TestGenerateGraderRedactionStep verifies every grader payload is redacted before upload.
func TestGenerateGraderRedactionStep(t *testing.T) {
	c := &Compiler{stepOrderTracker: NewStepOrderTracker()}
	var yaml strings.Builder

	// Built-in payloads also contain trace data and must be redacted.
	data := &WorkflowData{
		Graders: &GradersConfig{
			Graders: map[string]*GraderDefinition{
				"retries": {ID: "retries"},
			},
		},
	}
	c.generateGraderRedactionStep(&yaml, "", data)
	if !strings.Contains(yaml.String(), "Redact grader outputs") {
		t.Error("expected redaction step for built-in grader payload")
	}

	// Custom script — should emit redaction step
	yaml.Reset()
	data.Graders.Graders["my-custom"] = &GraderDefinition{
		ID:     "my-custom",
		Script: "return {value: 1}",
	}
	c.generateGraderRedactionStep(&yaml, "", data)
	if !strings.Contains(yaml.String(), "Redact grader outputs") {
		t.Error("expected redaction step for custom grader script")
	}
}

// TestGradersConfig_FrontmatterConfigField verifies the FrontmatterConfig struct has a Graders field.
func TestGradersConfig_FrontmatterConfigField(t *testing.T) {
	fc := FrontmatterConfig{
		Graders: map[string]any{},
	}
	if fc.Graders == nil {
		t.Error("expected Graders field to be set")
	}
}
