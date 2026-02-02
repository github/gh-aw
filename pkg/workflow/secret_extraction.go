package workflow

import (
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var secretLog = logger.New("workflow:secret_extraction")

// Pre-compiled regex for secret extraction (performance optimization)
// Matches: ${{ secrets.SECRET_NAME }} or ${{ secrets.SECRET_NAME || 'default' }}
var secretExprPattern = regexp.MustCompile(`\$\{\{\s*secrets\.([A-Z_][A-Z0-9_]*)\s*(?:\|\|.*?)?\s*\}\}`)

// SecretExpression represents a parsed secret expression
type SecretExpression struct {
	VarName  string // The secret variable name (e.g., "DD_API_KEY")
	FullExpr string // The full expression (e.g., "${{ secrets.DD_API_KEY }}")
}

// ExtractSecretName extracts just the secret name from a GitHub Actions expression
// Examples:
//   - "${{ secrets.DD_API_KEY }}" -> "DD_API_KEY"
//   - "${{ secrets.DD_SITE || 'datadoghq.com' }}" -> "DD_SITE"
//   - "plain value" -> ""
func ExtractSecretName(value string) string {
	matches := secretExprPattern.FindStringSubmatch(value)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// ExtractSecretsFromValue extracts all GitHub Actions secret expressions from a string value
// Returns a map of environment variable names to their full secret expressions
// Examples:
//   - "${{ secrets.DD_API_KEY }}" -> {"DD_API_KEY": "${{ secrets.DD_API_KEY }}"}
//   - "${{ secrets.DD_SITE || 'datadoghq.com' }}" -> {"DD_SITE": "${{ secrets.DD_SITE || 'datadoghq.com' }}"}
//   - "Bearer ${{ secrets.TOKEN }}" -> {"TOKEN": "${{ secrets.TOKEN }}"}
func ExtractSecretsFromValue(value string) map[string]string {
	secrets := make(map[string]string)

	// Pattern to match ${{ secrets.VARIABLE_NAME }} or ${{ secrets.VARIABLE_NAME || 'default' }}
	// We need to extract the variable name and the full expression
	start := 0
	for {
		// Find the start of an expression
		startIdx := strings.Index(value[start:], "${{ secrets.")
		if startIdx == -1 {
			break
		}
		startIdx += start

		// Find the end of the expression
		endIdx := strings.Index(value[startIdx:], "}}")
		if endIdx == -1 {
			break
		}
		endIdx += startIdx + 2 // Include the closing }}

		// Extract the full expression
		fullExpr := value[startIdx:endIdx]

		// Extract the variable name from "secrets.VARIABLE_NAME" or "secrets.VARIABLE_NAME ||"
		secretsPart := strings.TrimPrefix(fullExpr, "${{ secrets.")
		secretsPart = strings.TrimSuffix(secretsPart, "}}")
		secretsPart = strings.TrimSpace(secretsPart)

		// Find the variable name (everything before space, ||, or end)
		varName := secretsPart
		if spaceIdx := strings.IndexAny(varName, " |"); spaceIdx != -1 {
			varName = varName[:spaceIdx]
		}

		// Store the variable name and full expression
		if varName != "" {
			secrets[varName] = fullExpr
			secretLog.Printf("Extracted secret: %s", varName)
		}

		start = endIdx
	}

	if len(secrets) > 0 {
		secretLog.Printf("Extracted %d secrets from value", len(secrets))
	}
	return secrets
}

// ExtractSecretsFromMap extracts all secrets from a map of string values
// Returns a map of environment variable names to their full secret expressions
// Example:
//
//	Input: {"DD_API_KEY": "${{ secrets.DD_API_KEY }}", "DD_SITE": "${{ secrets.DD_SITE || 'default' }}"}
//	Output: {"DD_API_KEY": "${{ secrets.DD_API_KEY }}", "DD_SITE": "${{ secrets.DD_SITE || 'default' }}"}
func ExtractSecretsFromMap(values map[string]string) map[string]string {
	secretLog.Printf("Extracting secrets from map with %d entries", len(values))
	allSecrets := make(map[string]string)

	for _, value := range values {
		secrets := ExtractSecretsFromValue(value)
		for varName, expr := range secrets {
			allSecrets[varName] = expr
		}
	}

	secretLog.Printf("Extracted total of %d unique secrets from map", len(allSecrets))
	return allSecrets
}

// ReplaceSecretsWithEnvVars replaces secret expressions in a value with environment variable references
// Example: "${{ secrets.DD_API_KEY }}" -> "\${DD_API_KEY}"
// The backslash is used to escape the ${} for proper JSON rendering in Copilot configs
func ReplaceSecretsWithEnvVars(value string, secrets map[string]string) string {
	result := value
	for varName, secretExpr := range secrets {
		// Replace ${{ secrets.VAR }} with \${VAR} (backslash-escaped for copilot JSON config)
		result = strings.ReplaceAll(result, secretExpr, "\\${"+varName+"}")
	}
	return result
}
