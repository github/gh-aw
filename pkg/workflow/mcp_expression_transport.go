package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

const mcpExpressionEnvPrefix = "GH_AW_MCP_EXPR_"

var mcpShellReferencePattern = regexp.MustCompile(`\\?\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))`)

// Unescaped shell references used to expand in the heredoc. Resolve them from
// the runner environment after JSON parsing instead; escaped gateway references
// remain deferred to the gateway.
func normalizeMCPShellReferences(value string) string {
	return mcpShellReferencePattern.ReplaceAllStringFunc(value, func(reference string) string {
		if strings.HasPrefix(reference, `\`) {
			if !strings.HasPrefix(reference, `\${`) {
				return reference[1:]
			}
			return reference
		}
		name := strings.Trim(reference[1:], "{}")
		return `\${GH_AW_MCP_ENV_` + name + "}"
	})
}

// MCP expressions are resolved after JSON parsing, not by the expanding shell
// heredoc. This also resolves URL ports before the gateway validates URLs.
func replaceMCPExpressions(value string, preserveLegacy bool) (string, map[string]string) {
	bindings := make(map[string]string)
	if preserveLegacy {
		legacy := ReplaceTemplateExpressionsWithEnvVars(value)
		normalized := normalizeMCPShellReferences(legacy)
		// A gateway reference cannot share a transported value: the runtime
		// rejects references left after substitution to prevent second-pass expansion.
		if !ExpressionPattern.MatchString(legacy) &&
			!strings.Contains(normalized, `\${GH_AW_MCP_ENV_`) &&
			!strings.Contains(normalized, `\${`+mcpExpressionEnvPrefix) {
			maps.Copy(bindings, ExtractSecretsFromValue(value))
			maps.Copy(bindings, ExtractEnvExpressionsFromValue(value))
			return legacy, bindings
		}
	}
	return ExpressionPattern.ReplaceAllStringFunc(value, func(full string) string {
		if _, ok := actionslanguageservice.WrappedExpression(full); !ok {
			return full
		}
		content := strings.TrimSpace(full[3 : len(full)-2])
		hash := sha256.Sum256([]byte(content))
		name := mcpExpressionEnvPrefix + strings.ToUpper(hex.EncodeToString(hash[:]))
		bindings[name] = "${{ " + content + " }}"
		return `\${` + name + "}"
	}), bindings
}

func collectMCPConfigExpressionBindings(config *parser.RegistryMCPServerConfig) map[string]string {
	bindings := make(map[string]string)
	values := []string{config.URL}
	values = append(values, config.Args...)
	values = append(values, config.EntrypointArgs...)
	values = append(values, config.Mounts...)
	for _, value := range values {
		_, extracted := replaceMCPExpressions(value, false)
		maps.Copy(bindings, extracted)
	}
	for _, values := range []map[string]string{config.Headers, config.Env} {
		for _, value := range values {
			_, extracted := replaceMCPExpressions(value, true)
			maps.Copy(bindings, extracted)
		}
	}
	return bindings
}
