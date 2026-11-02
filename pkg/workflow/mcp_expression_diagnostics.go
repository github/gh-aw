package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/sliceutil"
)

var (
	mcpTransportReferencePattern = regexp.MustCompile(`\$\{GH_AW_MCP_(?:EXPR_[A-F0-9]{64}|ENV_[A-Za-z_][A-Za-z0-9_]*)\}`)
	mcpGatewayReferencePattern   = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)
)

func mcpConfigExpressionDiagnostics(toolName string, config map[string]any) ([]string, error) {
	var warnings []string
	for _, field := range []string{"url", "args", "entrypointArgs", "mounts", "headers", "env", "container", "entrypoint", "command", "registry", "proxy-args", "ca-cert-file", "allowed"} {
		preserveLegacy := field == "headers" || field == "env"
		replaceTemplates := preserveLegacy || field == "url" || field == "args" || field == "entrypointArgs" || field == "mounts"
		err := inspectMCPStringValues("mcp-servers."+toolName+"."+field, config[field], func(path, value string) error {
			rendered := value
			if replaceTemplates {
				rendered, _ = replaceMCPExpressions(value, preserveLegacy)
			} else if field == "ca-cert-file" {
				rendered = replaceMCPCACertFileExpressions(value)
			}
			normalized := normalizeMCPShellReferences(rendered)
			// A sentinel prevents removal from joining literal fragments into a
			// reference that might not exist once the dynamic value is known.
			literal := mcpTransportReferencePattern.ReplaceAllString(normalized, "__GH_AW_RUNTIME_VALUE__")
			if mcpTransportReferencePattern.MatchString(normalized) && mcpGatewayReferencePattern.MatchString(literal) {
				return fmt.Errorf("%s: cannot mix transported expressions or runner variables with gateway environment references in one value; MCP startup would reject it. Use a single Actions expression when all inputs are available on the runner, or keep gateway-only references in a separate configuration value", path)
			}
			if hasUnsupportedMCPShellExpansion(value) {
				warnings = append(warnings, path+": MCP configuration is data, not a shell script; shell computations and parameter operators remain literal. Compute the value in a setup step and reference its output with an Actions expression, or supply a fully evaluated value")
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return warnings, nil
}

func inspectMCPStringValues(path string, value any, inspect func(string, string) error) error {
	switch value := value.(type) {
	case string:
		return inspect(path, value)
	case []any:
		for index, item := range value {
			if err := inspectMCPStringValues(fmt.Sprintf("%s[%d]", path, index), item, inspect); err != nil {
				return err
			}
		}
	case []string:
		for index, item := range value {
			if err := inspect(fmt.Sprintf("%s[%d]", path, index), item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, key := range sliceutil.SortedKeys(value) {
			if err := inspectMCPStringValues(path+"."+key, value[key], inspect); err != nil {
				return err
			}
		}
	case map[string]string:
		for _, key := range sliceutil.SortedKeys(value) {
			if err := inspect(path+"."+key, value[key]); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasUnsupportedMCPShellExpansion(value string) bool {
	literal := ExpressionPattern.ReplaceAllString(value, "__GH_AW_ACTIONS_VALUE__")
	escaped := false
	for i, ch := range literal {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '`' {
			return true
		}
		if ch != '$' || i+1 >= len(literal) {
			continue
		}
		next := literal[i+1] //nolint:uncheckedsliceindex // Range offsets are nonnegative and the preceding guard bounds i+1.
		if next == '(' || strings.ContainsRune("?$#@*!-0123456789", rune(next)) {
			return true
		}
		if next == '{' {
			end := strings.IndexByte(literal[i:], '}')
			if end >= 0 {
				reference := literal[i : i+end+1]
				if mcpShellReferencePattern.FindString(reference) != reference {
					return true
				}
			}
		}
	}
	return false
}
