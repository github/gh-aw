package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
)

var jsonWorkflowLog = logger.New("cli:jsonworkflow_to_markdown")

// JSONWorkflow is a generic JSON workflow definition for import.
// All fields are optional; unrecognised top-level keys are collected in Extra
// and preserved as a YAML comment block so that no information is silently
// discarded.
type JSONWorkflow struct {
	// Identification
	ID   string `json:"id"`
	Name string `json:"name"`
	// Human-readable description → frontmatter description:
	Description string `json:"description"`
	// Main body / prompt text → markdown body after frontmatter.
	// Instructions takes precedence when both are set.
	Instructions string `json:"instructions"`
	// Prompt maps to the markdown body like Instructions does.
	// Instructions takes precedence when both are set.
	Prompt string `json:"prompt"`
	// Preferred AI engine → frontmatter engine:
	Engine string `json:"engine"`
	// On is a generic trigger configuration → frontmatter on: (passed through
	// as-is).  Takes precedence over Triggers when both are set.
	On any `json:"on"`
	// Triggers is a structured trigger block that is converted to the gh-aw
	// "on:" frontmatter field via convertTriggersToOn.
	// The On field takes precedence when both are set.
	Triggers *JSONWorkflowTriggers `json:"triggers"`
	// Tools lists tool IDs → frontmatter tools: (converted via convertToolsToConfig).
	Tools []string `json:"tools"`
	// Permissions maps GitHub Actions permission scopes to access levels
	// (e.g. {"issues": "write"}) → frontmatter permissions:
	Permissions map[string]string `json:"permissions"`
	// Tags → frontmatter tags:
	Tags []string `json:"tags"`
	// Extra holds any top-level keys not listed above so they can be preserved
	// as a comment block.
	Extra map[string]any `json:"-"`
}

// JSONWorkflowTriggers is the structured trigger block for a JSON workflow.
//
// Supported trigger kinds and their gh-aw "on:" mapping:
//
//	interval  hourly/daily/weekly  →  "hourly" / "daily" / "weekly" (string shorthand)
//	                                   or schedule: [{cron: "..."}] when combined with others
//	issues    opened               →  on: issues: types: [opened]
//	issues    opened + query       →  same + warning (query has no gh-aw equivalent)
//	workflow_run                   →  on: workflow_run: workflows: [...] types: [completed]
type JSONWorkflowTriggers struct {
	Interval    *IntervalTrigger    `json:"interval,omitempty"`
	Issues      *IssueTrigger       `json:"issues,omitempty"`
	WorkflowRun *WorkflowRunTrigger `json:"workflow_run,omitempty"`
}

// IntervalTrigger schedules the workflow.  Types is one or more of
// "hourly", "daily", "weekly".
type IntervalTrigger struct {
	Types []string `json:"types"`
}

// IssueTrigger fires when a GitHub issue is opened.  Types must contain
// "opened".  Query is an optional issue-search filter with no gh-aw equivalent.
type IssueTrigger struct {
	Types []string `json:"types"`
	Query string   `json:"query,omitempty"`
}

// WorkflowRunTrigger fires when a workflow run completes.  Workflows lists the
// workflow names to watch; Conclusions filters by result (e.g. "failure").
type WorkflowRunTrigger struct {
	Types       []string `json:"types"`
	Workflows   []string `json:"workflows"`
	Conclusions []string `json:"conclusions"`
}

// UnmarshalJSON implements json.Unmarshaler so that unknown keys are captured in Extra.
func (w *JSONWorkflow) UnmarshalJSON(data []byte) error {
	// Decode into the typed fields via a type alias (avoids infinite recursion).
	type jsonWorkflowAlias JSONWorkflow
	var alias jsonWorkflowAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*w = JSONWorkflow(alias)

	// Capture all top-level keys into a raw map.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	knownKeys := map[string]struct {
	}{
		"id": {}, "name": {}, "description": {},
		"instructions": {}, "prompt": {}, "engine": {},
		"on": {}, "triggers": {}, "tools": {}, "permissions": {}, "tags": {},
		// Metadata fields returned by APIs that should be ignored during import.
		"created_at": {}, "created_by": {}, "disabled": {}, "disabled_state": {}, "updated_at": {},
	}
	for k, v := range raw {
		if !setutil.Contains(knownKeys, k) {
			if w.Extra == nil {
				w.Extra = make(map[string]any)
			}
			var decoded any
			if err := json.Unmarshal(v, &decoded); err != nil {
				w.Extra[k] = string(v)
			} else {
				w.Extra[k] = decoded
			}
		}
	}
	return nil
}

// ConvertOptions configures ConvertJSONWorkflowToMarkdown.
type ConvertOptions struct {
	// NameOverride, when non-empty, replaces the filename derived from the JSON.
	NameOverride string
}

// GeneratedWorkflow is the output of ConvertJSONWorkflowToMarkdown.
type GeneratedWorkflow struct {
	// Filename is the kebab-cased base name (without .md extension).
	Filename string
	// Markdown is the complete file content: YAML frontmatter followed by the prompt body.
	Markdown string
	// Warnings lists fields that could not be fully translated.
	Warnings []string
}

// ConvertJSONWorkflowToMarkdown converts a JSONWorkflow into a gh-aw markdown workflow
// file.  The conversion is best-effort and deterministic: any field that cannot be
// mapped to a known frontmatter key or body section is preserved as a YAML comment
// block inside the frontmatter, and a corresponding warning is added to
// GeneratedWorkflow.Warnings.
func ConvertJSONWorkflowToMarkdown(a *JSONWorkflow, opts ConvertOptions) (*GeneratedWorkflow, error) {
	if a == nil {
		return nil, errors.New("JSONWorkflow must not be nil")
	}

	var warnings []string

	// ── Derive filename ─────────────────────────────────────────────────────────
	filename := opts.NameOverride
	if filename == "" {
		filename = filenameFromJSONWorkflow(a)
	}

	jsonWorkflowLog.Printf("Converting JSON workflow: id=%q name=%q filename=%q", a.ID, a.Name, filename)

	// ── Build frontmatter ────────────────────────────────────────────────────────
	var fm strings.Builder
	fm.WriteString("---\n")

	if a.Description != "" {
		fm.WriteString("description: ")
		fm.WriteString(yamlQuoteString(a.Description))
		fm.WriteString("\n")
	}

	if a.Engine != "" {
		fm.WriteString("engine: ")
		fm.WriteString(a.Engine)
		fm.WriteString("\n")
	}

	// "on:" is resolved from On (explicit, takes precedence) or converted from Triggers.
	onVal, triggerWarnings := resolveOnValue(a)
	warnings = append(warnings, triggerWarnings...)
	if onVal != nil {
		if s, ok := onVal.(string); ok {
			// Scalar shorthand e.g. "on: hourly"
			fm.WriteString("on: ")
			fm.WriteString(s)
			fm.WriteString("\n")
		} else {
			onYAML, err := marshalFrontmatterValue(onVal)
			if err == nil {
				fm.WriteString("on:\n")
				for line := range strings.SplitSeq(onYAML, "\n") {
					if line == "" {
						continue
					}
					fm.WriteString("  ")
					fm.WriteString(line)
					fm.WriteString("\n")
				}
			} else {
				warnings = append(warnings, fmt.Sprintf("could not serialize 'on' field: %v", err))
			}
		}
	}

	if len(a.Tools) > 0 {
		toolsConfig, toolWarnings := convertToolsToConfig(a.Tools, a.Engine)
		warnings = append(warnings, toolWarnings...)
		if len(toolsConfig) > 0 {
			toolsYAML, err := marshalFrontmatterValue(toolsConfig)
			if err == nil {
				fm.WriteString("tools:\n")
				for line := range strings.SplitSeq(toolsYAML, "\n") {
					if line == "" {
						continue
					}
					fm.WriteString("  ")
					fm.WriteString(line)
					fm.WriteString("\n")
				}
			} else {
				warnings = append(warnings, fmt.Sprintf("could not serialize 'tools' field: %v", err))
			}
		}
	}

	if len(a.Permissions) > 0 {
		permYAML, err := marshalFrontmatterValue(a.Permissions)
		if err == nil {
			fm.WriteString("permissions:\n")
			for line := range strings.SplitSeq(permYAML, "\n") {
				if line == "" {
					continue
				}
				fm.WriteString("  ")
				fm.WriteString(line)
				fm.WriteString("\n")
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("could not serialize 'permissions' field: %v", err))
		}
	}

	if len(a.Tags) > 0 {
		fm.WriteString("tags:\n")
		for _, tag := range a.Tags {
			fm.WriteString("  - ")
			fm.WriteString(yamlQuoteString(tag))
			fm.WriteString("\n")
		}
	}

	// Emit unknown fields as YAML comments so the file remains valid YAML while
	// preserving the original data for the operator to inspect.
	if len(a.Extra) > 0 {
		extraYAML, err := marshalFrontmatterValue(a.Extra)
		if err == nil {
			fm.WriteString("# Unsupported fields preserved from source JSON:\n")
			for line := range strings.SplitSeq(extraYAML, "\n") {
				if line == "" {
					continue
				}
				fm.WriteString("# ")
				fm.WriteString(line)
				fm.WriteString("\n")
			}
			// Sort keys for deterministic warning output.
			extraKeys := sliceutil.SortedKeys(a.Extra)
			for _, k := range extraKeys {
				warnings = append(warnings, fmt.Sprintf("field %q has no gh-aw frontmatter equivalent and was preserved as a comment", k))
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("could not serialize unsupported fields: %v", err))
		}
	}

	fm.WriteString("---\n")

	// ── Build body ───────────────────────────────────────────────────────────────
	var body strings.Builder

	// Heading from name (or ID as fallback).
	heading := a.Name
	if heading != "" {
		body.WriteString("# ")
		body.WriteString(heading)
		body.WriteString("\n\n")
	}

	if a.Instructions != "" {
		body.WriteString(strings.TrimRight(a.Instructions, "\n"))
		body.WriteString("\n")
	} else if a.Prompt != "" {
		// Prompt is the fallback body text when no Instructions field is present.
		body.WriteString(strings.TrimRight(a.Prompt, "\n"))
		body.WriteString("\n")
	}

	markdown := fm.String() + "\n" + body.String()

	jsonWorkflowLog.Printf("Generated workflow %q: %d byte(s), %d warning(s)", filename, len(markdown), len(warnings))

	return &GeneratedWorkflow{
		Filename: filename,
		Markdown: markdown,
		Warnings: warnings,
	}, nil
}

// filenameFromJSONWorkflow derives a kebab-cased filename slug from the workflow's name
// or id fields (name takes priority). A GUID-like id is not used as a filename.
func filenameFromJSONWorkflow(a *JSONWorkflow) string {
	candidate := a.Name
	if candidate == "" {
		return "imported-workflow"
	}
	return stringutil.SanitizeForFilename(toKebabCase(candidate))
}

// toKebabCase converts a string to kebab-case:
//   - whitespace and underscores → "-"
//   - sequences of non-alphanumeric chars → single "-"
//   - result is lower-cased
//
// nonAlphanumSeq matches one or more consecutive non-alphanumeric characters.
// It is compiled once at package init because regex compilation is expensive and
// the pattern is immutable.
var nonAlphanumSeq = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func toKebabCase(s string) string {
	// Normalize whitespace and underscores to dashes first.
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = nonAlphanumSeq.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return strings.ToLower(s)
}

// yamlQuoteString wraps s in double quotes if it contains characters that would
// require YAML quoting, otherwise returns it as-is.
func yamlQuoteString(s string) string {
	// Simple heuristic: quote if s contains a colon, hash, newline, or leading/trailing
	// whitespace – all of which require quoting in YAML plain scalars.
	if strings.ContainsAny(s, ":#\n\r") || s != strings.TrimSpace(s) || s == "" {
		// Escape backslashes and double-quotes inside the value, then escape
		// literal newlines/carriage-returns so the result is a valid single-line
		// YAML double-quoted scalar.
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		escaped = strings.ReplaceAll(escaped, "\n", `\n`)
		escaped = strings.ReplaceAll(escaped, "\r", `\r`)
		return `"` + escaped + `"`
	}
	return s
}

// marshalFrontmatterValue serializes v as indented YAML (without the leading "---").
func marshalFrontmatterValue(v any) (string, error) {
	raw, err := yaml.MarshalWithOptions(v, yaml.Indent(2), yaml.IndentSequence(true))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(raw), "\n"), nil
}

// resolveOnValue returns the value to use for the "on:" frontmatter key.
// a.On takes precedence; a.Triggers is used as a fallback.
func resolveOnValue(a *JSONWorkflow) (any, []string) {
	if a.On != nil {
		return a.On, nil
	}
	if a.Triggers != nil {
		return convertTriggersToOn(a.Triggers)
	}
	return nil, nil
}

// convertTriggersToOn maps a JSONWorkflowTriggers block to a value suitable for
// the gh-aw "on:" frontmatter field, plus any conversion warnings.
//
// Mapping rules (from GET /agents/automations/triggers):
//
//	interval  single type          →  string shorthand ("hourly" / "daily" / "weekly")
//	interval  single type + other  →  schedule: [{cron: "<expr>"}] inside a map
//	issues    opened               →  issues: {types: [opened]}
//	issues    query present        →  warning (query has no gh-aw equivalent)
//	workflow_run                   →  workflow_run: {workflows: [...], types: [completed]}
//	workflow_run  conclusions      →  warning (conclusions has no gh-aw equivalent)
func convertTriggersToOn(t *JSONWorkflowTriggers) (any, []string) {
	if t == nil {
		return nil, nil
	}

	jsonWorkflowLog.Printf("Converting triggers: interval=%t issues=%t workflow_run=%t",
		t.Interval != nil, t.Issues != nil, t.WorkflowRun != nil)

	var warnings []string
	// parts accumulates the trigger map entries.
	parts := map[string]any{}

	if t.Interval != nil && len(t.Interval.Types) > 0 {
		intervalType := t.Interval.Types[0]
		if len(t.Interval.Types) > 1 {
			warnings = append(warnings, fmt.Sprintf(
				"triggers.interval has multiple types %v; only the first (%q) will be used",
				t.Interval.Types, intervalType))
		}
		switch intervalType {
		case "hourly", "daily", "weekly":
			parts["_interval"] = intervalType
		default:
			warnings = append(warnings, fmt.Sprintf("triggers.interval type %q is not supported; skipped", intervalType))
		}
	}

	if t.Issues != nil && len(t.Issues.Types) > 0 {
		issueEntry := map[string]any{"types": t.Issues.Types}
		if t.Issues.Query != "" {
			warnings = append(warnings, `triggers.issues.query has no gh-aw equivalent; add a skip-if-match block manually if needed`)
		}
		parts["issues"] = issueEntry
	}

	if t.WorkflowRun != nil {
		if len(t.WorkflowRun.Workflows) == 0 || len(t.WorkflowRun.Types) == 0 {
			warnings = append(warnings, `triggers.workflow_run requires non-empty workflows and types; skipped`)
		} else {
			wfEntry := map[string]any{
				"workflows": t.WorkflowRun.Workflows,
				"types":     t.WorkflowRun.Types,
			}
			if len(t.WorkflowRun.Conclusions) > 0 {
				warnings = append(warnings, `triggers.workflow_run.conclusions has no gh-aw equivalent; review the generated "on:" block`)
			}
			parts["workflow_run"] = wfEntry
		}
	}

	if len(parts) == 0 {
		return nil, warnings
	}

	// Single interval trigger → use the string shorthand directly.
	intervalType, hasInterval := parts["_interval"]
	if hasInterval && len(parts) == 1 {
		interval, ok := intervalType.(string)
		if !ok {
			warnings = append(warnings, "triggers.interval must resolve to a string value; skipped")
			return nil, warnings
		}
		return interval, warnings
	}

	// Multiple triggers or non-interval: build a map.
	// Convert interval shorthand to a schedule cron entry.
	if hasInterval {
		delete(parts, "_interval")
		interval, ok := intervalType.(string)
		if !ok {
			warnings = append(warnings, "triggers.interval must resolve to a string value; schedule trigger skipped")
		} else {
			parts["schedule"] = []any{map[string]any{"cron": intervalToFuzzySchedule(interval)}}
		}
	}

	return parts, warnings
}

// intervalToFuzzySchedule returns a gh-aw fuzzy schedule expression for a
// JSON interval type.  These expressions are understood by gh-aw's schedule
// parser (pkg/parser/schedule_parser.go) and compiled to randomised cron
// entries at workflow compilation time — they must NOT be raw cron strings.
func intervalToFuzzySchedule(interval string) string {
	switch interval {
	case "hourly":
		return "every 1h"
	case "daily":
		return "daily"
	case "weekly":
		return "weekly"
	default:
		return "daily"
	}
}

// jsonToolToToolset maps a bare tool ID (without "github/" prefix) to the gh-aw
// GitHub MCP toolset name.  The toolset names match the group IDs returned by
// GET /agents/automations/tools.
var jsonToolToToolset = map[string]string{
	// Issues
	"issue_read": "issues", "list_issues": "issues", "search_issues": "issues",
	"create_issue": "issues", "update_issue_title": "issues", "update_issue_body": "issues",
	"update_issue_assignees": "issues", "update_issue_labels": "issues",
	"update_issue_milestone": "issues", "update_issue_type": "issues",
	"update_issue_state": "issues", "add_sub_issue": "issues",
	"remove_sub_issue": "issues", "reprioritize_sub_issue": "issues",
	"set_issue_fields": "issues", "add_issue_comment": "issues",
	// Pull Requests
	"pull_request_read": "pull-requests", "list_pull_requests": "pull-requests",
	"search_pull_requests": "pull-requests", "create_pull_request": "pull-requests",
	"update_pull_request_title": "pull-requests", "update_pull_request_body": "pull-requests",
	"update_pull_request_state": "pull-requests", "update_pull_request_draft_state": "pull-requests",
	"merge_pull_request": "pull-requests", "update_pull_request_branch": "pull-requests",
	"request_pull_request_reviewers": "pull-requests", "create_pull_request_review": "pull-requests",
	"submit_pending_pull_request_review": "pull-requests", "delete_pending_pull_request_review": "pull-requests",
	"add_pull_request_review_comment": "pull-requests", "resolve_review_thread": "pull-requests",
	"unresolve_review_thread": "pull-requests", "add_reply_to_pull_request_comment": "pull-requests",
	// Repos
	"get_file_contents": "repos", "search_code": "repos", "search_repositories": "repos",
	"list_branches": "repos", "list_commits": "repos", "get_commit": "repos",
	"list_tags": "repos", "get_tag": "repos", "create_branch": "repos",
	"create_or_update_file": "repos", "push_files": "repos", "delete_file": "repos",
	// Actions
	"actions_list": "actions", "actions_get": "actions", "get_job_logs": "actions",
	// Code Security
	"list_code_scanning_alerts": "code_security", "get_code_scanning_alert": "code_security",
	"list_secret_scanning_alerts": "code_security", "get_secret_scanning_alert": "code_security",
}

// jsonGeneralTools are built-in agent capabilities that need no explicit gh-aw
// configuration and produce no warning when encountered.
var jsonGeneralTools = map[string]bool{
	"read": true, "edit": true, "search": true,
}

// convertToolsToConfig maps a list of JSON tool IDs to a gh-aw tools config map
// (the value under the "tools:" frontmatter key) and any conversion warnings.
//
// Tool ID format: bare ("issue_read") or prefixed ("github/issue_read").
// Mapping:
//
//	github/*         →  tools: github: toolsets: [<toolset>]
//	execute          →  tools: bash: "*"  (+ warning)
//	web_search       →  tools: web-search: (for non-Copilot engines)
//	read/edit/search →  (built-in capability, no config needed, no warning)
func convertToolsToConfig(tools []string, engine string) (map[string]any, []string) {
	if len(tools) == 0 {
		return nil, nil
	}

	var warnings []string
	toolsets := map[string]struct {
	}{}
	config := map[string]any{}
	needsBash := false

	for _, id := range tools {
		bare := strings.TrimPrefix(id, "github/")
		if jsonGeneralTools[bare] {
			continue
		}
		switch bare {
		case "execute":
			needsBash = true
		case "web_search":
			if engine == "" || strings.EqualFold(engine, "copilot") {
				warnings = append(warnings, `tool "web_search" was skipped because Copilot native web search is unavailable; use a web-search MCP server or select a supported engine`)
			} else {
				// JSON uses underscore; gh-aw frontmatter uses hyphen.
				config["web-search"] = nil
			}
		default:
			if ts := jsonToolToToolset[bare]; ts != "" {
				toolsets[ts] = struct {
				}{}
			} else {
				warnings = append(warnings, fmt.Sprintf("tool %q has no known gh-aw mapping and was skipped", id))
			}
		}
	}

	if len(toolsets) > 0 {
		sorted := sliceutil.SortedKeys(toolsets)
		config["github"] = map[string]any{"toolsets": sorted}
	}
	if needsBash {
		config["bash"] = "*"
		warnings = append(warnings, `tool "execute" was mapped to bash: "*"; review the bash permissions`)
	}
	jsonWorkflowLog.Printf("Converted %d tool(s): %d toolset(s), bash=%t, %d warning(s)",
		len(tools), len(toolsets), needsBash, len(warnings))
	return config, warnings
}
