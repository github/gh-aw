//go:build !integration

package cli

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertJSONWorkflowToMarkdown_Basic(t *testing.T) {
	t.Parallel()
	wf := &JSONWorkflow{
		ID:           "my-workflow",
		Name:         "My Workflow",
		Description:  "Does something useful",
		Instructions: "Step 1: Do A\nStep 2: Do B",
		Engine:       "copilot",
	}
	gen, err := ConvertJSONWorkflowToMarkdown(wf, ConvertOptions{})
	require.NoError(t, err)
	assert.Equal(t, "my-workflow", gen.Filename)
	assert.Contains(t, gen.Markdown, "description: Does something useful")
	assert.Contains(t, gen.Markdown, "engine: copilot")
	assert.Contains(t, gen.Markdown, "# My Workflow")
	assert.Contains(t, gen.Markdown, "Step 1: Do A")
	assert.Empty(t, gen.Warnings)
}

func TestConvertJSONWorkflowToMarkdown_FallbackToName(t *testing.T) {
	t.Parallel()
	wf := &JSONWorkflow{
		Name:         "Weekly Research",
		Instructions: "Do research",
	}
	gen, err := ConvertJSONWorkflowToMarkdown(wf, ConvertOptions{})
	require.NoError(t, err)
	assert.Equal(t, "weekly-research", gen.Filename)
}

func TestConvertJSONWorkflowToMarkdown_NameOverride(t *testing.T) {
	t.Parallel()
	wf := &JSONWorkflow{
		ID:   "original-id",
		Name: "Original Name",
	}
	gen, err := ConvertJSONWorkflowToMarkdown(wf, ConvertOptions{NameOverride: "custom-name"})
	require.NoError(t, err)
	assert.Equal(t, "custom-name", gen.Filename)
}

func TestConvertJSONWorkflowToMarkdown_NoIDOrName(t *testing.T) {
	t.Parallel()
	wf := &JSONWorkflow{
		Instructions: "Just instructions",
	}
	gen, err := ConvertJSONWorkflowToMarkdown(wf, ConvertOptions{})
	require.NoError(t, err)
	assert.Equal(t, "imported-workflow", gen.Filename)
}

func TestConvertJSONWorkflowToMarkdown_GUIDIDWithNoName(t *testing.T) {
	t.Parallel()
	wf := &JSONWorkflow{
		ID:           "b5a3f76a-3d8f-4790-b7e2-f2886f784345",
		Instructions: "Just instructions",
	}
	gen, err := ConvertJSONWorkflowToMarkdown(wf, ConvertOptions{})
	require.NoError(t, err)
	assert.Equal(t, "imported-workflow", gen.Filename, "GUID id should not be used as filename")
}

func TestConvertJSONWorkflowToMarkdown_Tags(t *testing.T) {
	t.Parallel()
	wf := &JSONWorkflow{
		ID:   "tagged",
		Tags: []string{"automation", "ci"},
	}
	gen, err := ConvertJSONWorkflowToMarkdown(wf, ConvertOptions{})
	require.NoError(t, err)
	assert.Contains(t, gen.Markdown, "tags:")
	assert.Contains(t, gen.Markdown, "- automation")
	assert.Contains(t, gen.Markdown, "- ci")
}

func TestConvertJSONWorkflowToMarkdown_OnField(t *testing.T) {
	t.Parallel()
	wf := &JSONWorkflow{
		ID: "triggered",
		On: map[string]any{"push": nil, "pull_request": nil},
	}
	gen, err := ConvertJSONWorkflowToMarkdown(wf, ConvertOptions{})
	require.NoError(t, err)
	assert.Contains(t, gen.Markdown, "on:")
}

func TestConvertJSONWorkflowToMarkdown_ExtraFieldsPreserved(t *testing.T) {
	t.Parallel()
	raw := `{"id":"extra-wf","name":"Extra WF","unknown_field":"some_value","another":42}`
	var wf JSONWorkflow
	require.NoError(t, json.Unmarshal([]byte(raw), &wf))

	gen, err := ConvertJSONWorkflowToMarkdown(&wf, ConvertOptions{})
	require.NoError(t, err)

	// Unknown fields must appear as comments in the markdown.
	assert.Contains(t, gen.Markdown, "# Unsupported fields", "expected comment header for unsupported fields")
	assert.Contains(t, gen.Markdown, "# ", "expected comment lines")
	// Warnings must be reported for each unknown field.
	assert.Len(t, gen.Warnings, 2, "expected one warning per unknown field")
}

func TestConvertJSONWorkflowToMarkdown_NilInput(t *testing.T) {
	t.Parallel()
	_, err := ConvertJSONWorkflowToMarkdown(nil, ConvertOptions{})
	require.Error(t, err)
}

func TestConvertJSONWorkflowToMarkdown_FrontmatterValid(t *testing.T) {
	t.Parallel()
	wf := &JSONWorkflow{
		ID:           "valid-fm",
		Description:  "description with: colon",
		Instructions: "body",
	}
	gen, err := ConvertJSONWorkflowToMarkdown(wf, ConvertOptions{})
	require.NoError(t, err)
	// Colons in description values must be quoted.
	assert.Contains(t, gen.Markdown, `"description with: colon"`)
}

func TestConvertJSONWorkflowToMarkdown_NewlineInDescription(t *testing.T) {
	t.Parallel()
	wf := &JSONWorkflow{
		ID:          "newline-desc",
		Description: "line one\nline two",
	}
	gen, err := ConvertJSONWorkflowToMarkdown(wf, ConvertOptions{})
	require.NoError(t, err)
	// Newlines must be escaped, not embedded literally, so the frontmatter stays valid.
	assert.Contains(t, gen.Markdown, `"line one\nline two"`)
	assert.NotContains(t, gen.Markdown, "line one\nline two")
}

func TestYamlQuoteString_BackslashN(t *testing.T) {
	t.Parallel()
	// A literal backslash followed by 'n' (not a newline) must survive round-trip
	// as '\\n' inside a double-quoted YAML scalar.
	result := yamlQuoteString(`has\nbackslash`)
	// No quoting needed for a plain backslash-n, but if quoted it must be \\n.
	// Either way the result must not collapse the two characters into a single newline.
	assert.NotContains(t, result, "\n")
	assert.Contains(t, result, `\n`)
}

func TestJSONWorkflow_UnmarshalJSON_CapturesExtra(t *testing.T) {
	t.Parallel()
	raw := `{"id":"w","name":"N","unknown_key":"val","nested":{"a":1}}`
	var wf JSONWorkflow
	require.NoError(t, json.Unmarshal([]byte(raw), &wf))
	assert.Equal(t, "w", wf.ID)
	assert.Equal(t, "N", wf.Name)
	assert.Contains(t, wf.Extra, "unknown_key")
	assert.Contains(t, wf.Extra, "nested")
}

func TestJSONWorkflow_UnmarshalJSON_IgnoresMetadataFields(t *testing.T) {
	t.Parallel()
	raw := `{"id":"w","created_by":{"login":"octocat"},"disabled":true,"disabled_state":null,"updated_at":"2026-01-01T00:00:00Z","unknown_key":"val"}`
	var wf JSONWorkflow
	require.NoError(t, json.Unmarshal([]byte(raw), &wf))

	assert.Contains(t, wf.Extra, "unknown_key")
	assert.NotContains(t, wf.Extra, "created_by")
	assert.NotContains(t, wf.Extra, "disabled")
	assert.NotContains(t, wf.Extra, "disabled_state")
	assert.NotContains(t, wf.Extra, "updated_at")
}

func TestToKebabCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"My Workflow", "my-workflow"},
		{"my_workflow", "my-workflow"},
		{"My Workflow Name!", "my-workflow-name"},
		{"already-kebab", "already-kebab"},
		{"  spaces  ", "spaces"},
		{"Mixed_Case And Underscores", "mixed-case-and-underscores"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, toKebabCase(tc.input))
		})
	}
}

// TestConvertJSONWorkflowToMarkdown_IntervalTrigger tests the importer against
// a real-world payload from the JSON workflow API with an interval trigger.
// prompt → body, triggers.interval → "on: hourly" shorthand.
func TestConvertJSONWorkflowToMarkdown_IntervalTrigger(t *testing.T) {
	t.Parallel()
	// Anonymised payload – login replaced with the octocat placeholder.
	raw := `{
		"id": "b5a3f76a-3d8f-4790-b7e2-f2886f784345",
		"name": "haiku",
		"description": "Format and linter",
		"prompt": "Write a haiku",
		"disabled_state": null,
		"triggers": {"interval": {"types": ["hourly"]}},
		"created_at": "2026-05-19T02:56:23.763410743Z",
		"updated_at": "2026-05-19T02:56:23.763410743Z",
		"created_by": {"id": 1, "login": "octocat", "node_id": "MDQ6VXNlcjE=", "url": "https://github.com/octocat"}
	}`

	var wf JSONWorkflow
	require.NoError(t, json.Unmarshal([]byte(raw), &wf), "unmarshal must succeed")

	gen, err := ConvertJSONWorkflowToMarkdown(&wf, ConvertOptions{})
	require.NoError(t, err)

	assert.Equal(t, "haiku", gen.Filename, "filename from name")
	assert.Contains(t, gen.Markdown, "description: Format and linter", "description in frontmatter")
	assert.Contains(t, gen.Markdown, "# haiku", "heading from name")

	// interval → string shorthand
	assert.Contains(t, gen.Markdown, "on: hourly", "interval trigger → shorthand")

	// prompt → body text
	assert.Contains(t, gen.Markdown, "Write a haiku", "prompt in body")
	assert.NotContains(t, gen.Markdown, "# prompt:", "prompt must NOT appear as comment")

	// triggers is a known field now – no comment or warning for it
	assert.NotContains(t, gen.Markdown, "# triggers:", "triggers must NOT appear as comment")

	// Warnings only for genuinely unknown fields (ignored metadata is dropped silently).
	for _, field := range []string{"prompt", "triggers", "created_at", "updated_at", "created_by", "disabled_state"} {
		for _, w := range gen.Warnings {
			assert.NotContains(t, w, field, "unexpected warning mentioning %q: %s", field, w)
		}
	}
}

// TestConvertJSONWorkflowToMarkdown_MultiTriggerWithTools tests a richer payload:
// multi-trigger (issues + workflow_run), tools migration, permissions, and
// disabled/metadata fields going to Extra.
func TestConvertJSONWorkflowToMarkdown_MultiTriggerWithTools(t *testing.T) {
	t.Parallel()
	// Anonymised payload – login replaced with octocat.
	raw := `{
		"id": "0be2cc4b-de12-43fe-ada7-55ef6dc8f3ba",
		"name": "issue triage",
		"description": "test",
		"prompt": "Summarize issue.",
		"disabled": true,
		"disabled_state": {"disabled_at": "2026-05-19T17:37:05.839761776Z", "reason": "disabled_by_user"},
		"triggers": {
			"issues": {"query": "label:bug", "types": ["opened"]},
			"workflow_run": {"conclusions": ["failure"], "types": ["completed"], "workflows": ["haiku"]}
		},
		"tools": ["github/update_issue_body"],
		"permissions": {"issues": "write"},
		"created_at": "2026-05-19T17:37:00.145883739Z",
		"updated_at": "2026-05-19T17:37:05.839761776Z",
		"created_by": {"id": 1, "login": "octocat", "node_id": "MDQ6VXNlcjE=", "url": "https://github.com/octocat"}
	}`

	var wf JSONWorkflow
	require.NoError(t, json.Unmarshal([]byte(raw), &wf), "unmarshal must succeed")

	gen, err := ConvertJSONWorkflowToMarkdown(&wf, ConvertOptions{})
	require.NoError(t, err)

	assert.Equal(t, "issue-triage", gen.Filename, "filename from name")
	assert.Contains(t, gen.Markdown, "# issue triage", "heading from name")

	// Multi-trigger → map form (not string shorthand)
	assert.Contains(t, gen.Markdown, "on:", "on: block present")
	assert.Contains(t, gen.Markdown, "issues:", "issues trigger present")
	assert.Contains(t, gen.Markdown, "workflow_run:", "workflow_run trigger present")
	assert.Contains(t, gen.Markdown, "haiku", "workflow name present")
	assert.NotContains(t, gen.Markdown, "on: hourly", "must not use shorthand for multi-trigger")

	// tools → gh-aw toolsets
	assert.Contains(t, gen.Markdown, "tools:", "tools block present")
	assert.Contains(t, gen.Markdown, "issues", "issues toolset from update_issue_body")

	// permissions → frontmatter
	assert.Contains(t, gen.Markdown, "permissions:", "permissions block present")
	assert.Contains(t, gen.Markdown, "write", "issues:write permission")

	// prompt → body
	assert.Contains(t, gen.Markdown, "Summarize issue.", "prompt in body")

	// Warnings for query and conclusions (no gh-aw equivalent)
	hasQueryWarn, hasConclusionsWarn := false, false
	for _, w := range gen.Warnings {
		if strings.Contains(w, "query") {
			hasQueryWarn = true
		}
		if strings.Contains(w, "conclusions") {
			hasConclusionsWarn = true
		}
	}
	assert.True(t, hasQueryWarn, "expected warning about issues.query")
	assert.True(t, hasConclusionsWarn, "expected warning about workflow_run.conclusions")

	// created_at is now a silently-ignored metadata field; no warning expected.
	assert.NotContains(t, gen.Markdown, "# Unsupported fields preserved from source JSON:", "no extra comment header when no unknown fields remain")
	for _, field := range []string{"created_at", "disabled", "disabled_state", "updated_at", "created_by"} {
		for _, w := range gen.Warnings {
			assert.NotContains(t, w, field, "unexpected warning mentioning %q: %s", field, w)
		}
	}
}

func TestConvertTriggersToOn_SkipsIncompleteWorkflowRun(t *testing.T) {
	t.Parallel()
	on, warnings := convertTriggersToOn(&JSONWorkflowTriggers{
		WorkflowRun: &WorkflowRunTrigger{
			Workflows: []string{"haiku"},
		},
	})

	assert.Nil(t, on)
	assert.Contains(t, warnings, `triggers.workflow_run requires non-empty workflows and types; skipped`)
}

func TestGenericURLWorkflowName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/my-workflow.md", "my-workflow"},
		{"https://example.com/workflows/daily-report.json", "daily-report"},
		{"https://example.com/", "imported-workflow"},
		{"https://example.com/workflow.yaml", "workflow"},
		// url.Parse treats bare strings as relative paths; "not-a-url" has no extension.
		{"not-a-url", "not-a-url"},
		// Spaces and mixed-case should be kebab-cased.
		{"https://example.com/My%20Workflow.json", "my-workflow"},
		{"https://example.com/My_Workflow.md", "my-workflow"},
		{"https://example.com/Weekly Report.json", "weekly-report"},
	}
	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			assert.Equal(t, tc.want, genericURLWorkflowName(tc.url))
		})
	}
}

func TestRewriteAutomationsURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantURL string
		wantOK  bool
	}{
		{
			name:    "dotcom automations UI URL",
			input:   "https://github.com/dmgardiner25/urban-spork/agents/automations/1a352b54-80f0-41ed-bf50-9e90e6b9d768",
			wantURL: "https://api.githubcopilot.com/agents/repos/dmgardiner25/urban-spork/automations/1a352b54-80f0-41ed-bf50-9e90e6b9d768",
			wantOK:  true,
		},
		{
			name:   "regular github.com file URL — no rewrite",
			input:  "https://github.com/owner/repo/blob/main/workflow.md",
			wantOK: false,
		},
		{
			name:   "GHE host — no rewrite",
			input:  "https://mycompany.ghe.com/owner/repo/agents/automations/1a352b54-80f0-41ed-bf50-9e90e6b9d768",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := url.Parse(tc.input)
			got, ok := rewriteAutomationsURL(u)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantURL, got)
			}
		})
	}
}

func TestParseWorkflowSpec_AutomationsURL(t *testing.T) {
	t.Parallel()
	spec, err := parseWorkflowSpec("https://github.com/dmgardiner25/urban-spork/agents/automations/1a352b54-80f0-41ed-bf50-9e90e6b9d768")
	require.NoError(t, err)
	assert.Equal(t, "https://api.githubcopilot.com/agents/repos/dmgardiner25/urban-spork/automations/1a352b54-80f0-41ed-bf50-9e90e6b9d768", spec.RawURL)
	assert.Equal(t, "1a352b54-80f0-41ed-bf50-9e90e6b9d768", spec.WorkflowName, "workflow name is GUID (will be overridden from JSON name field at fetch time)")
}

// TestConvertJSONWorkflowToMarkdown_CompilerRoundTrip verifies that a real-world
// automation payload (anonymised) converts to markdown that the gh-aw compiler
// accepts without errors and produces valid GitHub Actions YAML.
//
// Payload mirrors the automation fetched from the Copilot agents API:
//
//	tools:      ["github/list_code_scanning_alerts"]
//	triggers:   workflow_run on "bar" (completed), conclusions: ["failure"]
//	permissions: security-events: read
func TestConvertJSONWorkflowToMarkdown_CompilerRoundTrip(t *testing.T) {
	t.Parallel()
	// Anonymised real-world payload – sensitive fields replaced with placeholders.
	raw := `{
		"id":          "4a803d2b-ef80-44ed-9a01-4617a2984ed2",
		"name":        "test",
		"description": "foo",
		"prompt":      "do something",
		"disabled":    true,
		"disabled_state": {"disabled_at": "2026-05-20T15:14:55.440451459Z", "reason": "disabled_by_user"},
		"permissions": {"security-events": "read"},
		"tools":       ["github/list_code_scanning_alerts", "web_search"],
		"triggers":    {"workflow_run": {"conclusions": ["failure"], "types": ["completed"], "workflows": ["bar"]}},
		"created_at":  "2026-05-20T15:14:50.047316109Z",
		"updated_at":  "2026-05-20T15:14:55.440451459Z",
		"created_by":  {"id": 1, "login": "octocat", "node_id": "MDQ6VXNlcjE=", "url": "https://github.com/octocat"}
	}`

	var wf JSONWorkflow
	require.NoError(t, json.Unmarshal([]byte(raw), &wf), "unmarshal must succeed")

	gen, err := ConvertJSONWorkflowToMarkdown(&wf, ConvertOptions{})
	require.NoError(t, err, "conversion must succeed")
	assert.Equal(t, "test", gen.Filename)

	// Validate the markdown is proper YAML frontmatter (no JSON-style braces).
	assert.NotContains(t, gen.Markdown, `"workflow_run": {`, "must not contain JSON-style map syntax")
	assert.NotContains(t, gen.Markdown, `"toolsets": [`, "must not contain JSON-style array syntax")

	// Check key frontmatter fields are present in clean YAML form.
	assert.Contains(t, gen.Markdown, "description: foo")
	assert.Contains(t, gen.Markdown, "workflow_run:")
	assert.Contains(t, gen.Markdown, "- bar")
	assert.Contains(t, gen.Markdown, "toolsets:")
	assert.Contains(t, gen.Markdown, "- code_security")
	assert.Contains(t, gen.Markdown, "security-events: read")
	assert.NotContains(t, gen.Markdown, "web-search:")
	assert.Contains(t, gen.Warnings, `tool "web_search" was skipped because Copilot native web search is unavailable; use a web-search MCP server or select a supported engine`)

	// ── Compiler round-trip ───────────────────────────────────────────────────
	// The converted markdown must compile to valid GitHub Actions YAML without
	// any errors.
	compiler := workflow.NewCompiler()
	compiler.SetSkipValidation(false)
	compiler.SetRepositorySlug("octocat/test-repo")

	workflowData, err := compiler.ParseWorkflowString(gen.Markdown, "test.md")
	require.NoError(t, err, "compiler must parse the converted markdown without errors")

	yamlOutput, err := compiler.CompileToYAML(workflowData, "test.md")
	require.NoError(t, err, "compiler must produce valid YAML from the converted markdown")

	// Spot-check the compiled YAML output.
	assert.Contains(t, yamlOutput, "workflow_run:", "compiled YAML must contain workflow_run trigger")
	assert.Contains(t, yamlOutput, "do something", "compiled YAML must contain the prompt body")
}

func TestConvertJSONWorkflowToMarkdown_WebSearchForSupportedEngine(t *testing.T) {
	t.Parallel()
	gen, err := ConvertJSONWorkflowToMarkdown(&JSONWorkflow{
		Name:   "supported-search",
		Engine: "codex",
		Tools:  []string{"web_search"},
	}, ConvertOptions{})
	require.NoError(t, err)
	assert.Contains(t, gen.Markdown, "web-search:")
	assert.Empty(t, gen.Warnings)
}
