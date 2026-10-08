package cli

// This file implements an MCP receiving middleware that transforms raw JSON schema
// "additional properties" validation errors into helpful, user-friendly messages
// with "Did you mean?" suggestions.
//
// Background: When the MCP SDK validates tool arguments against the input schema
// (which uses additionalProperties: false), it emits a raw message like:
//
//	validating "arguments": validating root: unexpected additional properties ["workflow-name"]
//
// This is surfaced directly to users, leaking internal validation details without
// any guidance on the correct parameter name.  The middleware here intercepts
// those tool-error results and replaces the message with a helpful alternative.

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var mcpArgValidationLog = logger.New("cli:mcp_argument_validation")

// toolParamEntry holds the valid parameter names for a single MCP tool.
type toolParamEntry = []string

// argumentValidationMiddleware returns a mcp.Middleware that intercepts tool-call
// results containing "unexpected additional properties" validation errors and
// replaces them with a helpful message that names the unknown parameter, suggests
// a close match, and points to the tool's --help output.
//
// toolParams maps tool names to their list of valid JSON parameter names.  It is
// provided by the caller as a hardcoded registry (see mcpToolParams).
func argumentValidationMiddleware(toolParams map[string]toolParamEntry) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if err != nil || method != "tools/call" {
				return result, err
			}

			// Check whether the result is a tool error containing a schema
			// "additional properties" validation message.
			toolResult, ok := result.(*mcp.CallToolResult)
			if !ok || !toolResult.IsError {
				return result, err
			}

			// Extract the error text from the first TextContent element.
			if len(toolResult.Content) == 0 {
				return result, err
			}
			textContent, ok := toolResult.Content[0].(*mcp.TextContent)
			if !ok {
				return result, err
			}
			errMsg := textContent.Text

			if !strings.Contains(errMsg, "unexpected additional properties") {
				return result, err
			}

			// Parse the unknown parameter names from the error text.
			unknownParams := extractUnknownParams(errMsg)
			if len(unknownParams) == 0 {
				return result, err
			}

			// Determine the tool name from the request so we can look up valid params.
			toolName := extractMCPToolName(req)
			validParams, ok := toolParams[toolName]
			if !ok {
				// The tool name is not in the hardcoded registry (mcpToolParams).
				// This should not happen in practice because the registry is
				// exhaustive. Escalating to a protocol-level internal error
				// makes any registry gap fail loudly as a server bug instead of
				// surfacing as an ordinary tool-level validation failure.
				return nil, newMCPError(jsonrpc.CodeInternalError, fmt.Sprintf("unknown MCP tool: %q", toolName), nil)
			}

			mcpArgValidationLog.Printf("Intercepted unknown param error: tool=%s, unknown_params=%v", toolName, unknownParams)

			// Build a helpful replacement message.
			helpMsg := buildHelpfulParamError(toolName, unknownParams, validParams)

			// Return a modified tool result with the helpful message, preserving IsError.
			replaced := *toolResult
			replaced.Content = []mcp.Content{&mcp.TextContent{Text: helpMsg}}
			return &replaced, nil
		}
	}
}

// extractMCPToolName retrieves the tool name from a MCP Request by casting the
// request params to *mcp.CallToolParamsRaw.  Returns an empty string if the cast
// fails.
func extractMCPToolName(req mcp.Request) string {
	if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
		return p.Name
	}
	return ""
}

// extractUnknownParams parses the JSON-schema validation error string to extract
// the list of unknown parameter names.
//
// The expected format (from jsonschema-go) is:
//
//	unexpected additional properties ["name1" "name2"]
//
// which uses %q-style quoting for a []string.
var additionalPropsRE = regexp.MustCompile(`unexpected additional properties (.+)$`)
var quotedStringRE = regexp.MustCompile(`"([^"]+)"`)

func extractUnknownParams(errMsg string) []string {
	m := additionalPropsRE.FindStringSubmatch(errMsg)
	if m == nil {
		return nil
	}
	raw := m[1]
	matches := quotedStringRE.FindAllStringSubmatch(raw, -1)
	var params []string
	for _, sm := range matches {
		if sm[1] != "" {
			params = append(params, sm[1])
		}
	}
	return params
}

// freeformEvaluationParamNames lists normalized (lowercase, no hyphen/underscore)
// parameter names that indicate a caller is trying to invoke ad hoc, freeform
// scenario evaluation (persona walkthroughs, "what workflow would you create
// for this scenario?" requests) through a CLI/MCP tool parameter rather than
// through the intended interface.
//
// None of the registered MCP tools (compile, audit, status, update, ...) accept
// a freeform prompt: they operate on existing workflow files. Ad hoc evaluation
// is a conversation-mode capability of the installed "agentic-workflows" custom
// agent, not a tool parameter, so these unknown parameters get a clearer
// product-gap message instead of a generic "did you mean" suggestion.
var freeformEvaluationParamNames = map[string]bool{
	"prompt":   true,
	"scenario": true,
	"query":    true,
}

// buildHelpfulParamError constructs a human-readable error message that:
//   - names each unknown parameter
//   - suggests the closest valid parameter (if a good match is found)
//   - directs the user to the tool's --help output
//   - for parameters that suggest freeform scenario evaluation (e.g. "prompt"),
//     points to the ad hoc evaluation mode documented for the custom agent
//     instead of a generic "did you mean" suggestion
func buildHelpfulParamError(toolName string, unknownParams []string, validParams []string) string {
	var sb strings.Builder

	for i, param := range unknownParams {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "Unknown parameter '%s'.", param)
		if freeformEvaluationParamNames[normalizeParamName(param)] {
			sb.WriteString(" The MCP tools operate on existing workflow files and do not accept a freeform prompt. For ad hoc scenario evaluation (persona walkthroughs, \"what workflow would you create for this scenario?\"), invoke the agentic-workflows custom agent directly in conversation mode; see the \"Ad Hoc Scenario Evaluation\" section of github-agentic-workflows.md.")
		} else if suggestion := findSimilarParam(param, validParams); suggestion != "" {
			fmt.Fprintf(&sb, " Did you mean '%s'?", suggestion)
		}
	}

	if toolName != "" {
		fmt.Fprintf(&sb, "\nRun 'agenticworkflows %s --help' for usage.", toolName)
	}

	return sb.String()
}

// findSimilarParam returns the valid parameter name most similar to unknown, or
// an empty string if no parameter is close enough.
//
// Similarity is measured by the ratio of the longest-common-prefix length to the
// shorter of the two normalized strings.  A threshold of 0.7 (70%) is required.
//
// Normalization: lowercase, hyphens and underscores removed.
func findSimilarParam(unknown string, validParams []string) string {
	if len(validParams) == 0 {
		return ""
	}

	mcpArgValidationLog.Printf("Finding similar param for %q among %d candidates", unknown, len(validParams))

	normUnknown := normalizeParamName(unknown)

	type candidate struct {
		name  string
		score float64
	}
	var best candidate

	for _, p := range validParams {
		normP := normalizeParamName(p)

		// Exact normalized match wins immediately.
		if normP == normUnknown {
			return p
		}

		lcp := longestCommonPrefixLen(normUnknown, normP)
		shorter := min(len(normP), len(normUnknown))
		if shorter == 0 {
			continue
		}
		score := float64(lcp) / float64(shorter)
		if score > best.score {
			best = candidate{name: p, score: score}
		}
	}

	const threshold = 0.7
	if best.score >= threshold {
		mcpArgValidationLog.Printf("Found similar param: %q -> %q (score=%.2f)", unknown, best.name, best.score)
		return best.name
	}
	mcpArgValidationLog.Printf("No similar param found for %q (best score=%.2f, threshold=%.2f)", unknown, best.score, threshold)
	return ""
}

// normalizeParamName lowercases name and removes hyphens and underscores, so
// that "workflow-name", "workflow_name", and "workflowname" all compare equal.
func normalizeParamName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, "_", "")
	return name
}

// longestCommonPrefixLen returns the length of the longest common prefix of a
// and b.
func longestCommonPrefixLen(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// jsonFieldNames extracts the JSON field names from the exported fields of a
// struct value using reflection.  The json tag name (the part before the first
// comma) is used; fields whose tag is "-" or that have no json tag are skipped.
// The returned slice is sorted for deterministic output.
func jsonFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for field := range t.Fields() {
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// mcpToolParams returns the registry of valid parameter names for every tool
// registered in the MCP server.  The map key is the MCP tool name; the value
// is the sorted list of valid JSON parameter names derived automatically from
// the corresponding *Args struct's json tags via reflection.
//
// Each *Args struct is declared at package scope (rather than locally inside its
// register*Tool function) precisely so it can be referenced here for reflection.
// Adding a new field to any *Args struct automatically includes it here —
// no manual update is required.
func mcpToolParams() map[string]toolParamEntry {
	return map[string]toolParamEntry{
		"status":      jsonFieldNames(statusArgs{}),
		"compile":     jsonFieldNames(compileArgs{}),
		"logs":        jsonFieldNames(logsArgs{}),
		"audit":       jsonFieldNames(auditArgs{}),
		"audit-diff":  jsonFieldNames(auditDiffArgs{}),
		"checks":      jsonFieldNames(checksArgs{}),
		"mcp-inspect": jsonFieldNames(mcpInspectArgs{}),
		"work-queue":  jsonFieldNames(workQueueArgs{}),
		"add":         jsonFieldNames(addArgs{}),
		"update":      jsonFieldNames(updateArgs{}),
		"fix":         jsonFieldNames(fixArgs{}),
	}
}
