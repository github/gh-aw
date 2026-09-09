package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
)

// expandLinearTool converts the first-class tools.linear shorthand into the
// generic remote HTTP MCP representation used by all engines and the gateway.
func expandLinearTool(tools map[string]any) error {
	value, exists := tools["linear"]
	if !exists {
		return nil
	}
	if value == nil {
		value = map[string]any{}
	}
	config, ok := value.(map[string]any)
	if !ok {
		return NewValidationError(
			"tools.linear",
			fmt.Sprintf("%v", value),
			"'tools.linear' must be an object",
			"Example:\n\ntools:\n  linear:\n    token: ${{ secrets.LINEAR_API_KEY }}",
		)
	}

	token, allowed, err := validateLinearToolConfig(config)
	if err != nil {
		return err
	}
	linearMCP := map[string]any{
		"type": "http",
		"url":  constants.LinearMCPReadOnlyURL,
		"headers": map[string]any{
			"Authorization": "Bearer " + token,
		},
	}
	if len(allowed) > 0 {
		linearMCP["allowed"] = allowed
	}
	if required, exists := config["required"]; exists {
		linearMCP["required"] = required
	}
	tools["linear"] = linearMCP
	return nil
}

func validateLinearToolConfig(config map[string]any) (string, []string, error) {
	knownFields := map[string]struct{}{
		"token": {}, "toolsets": {}, "allowed": {}, "required": {},
	}
	for field := range config {
		if _, known := knownFields[field]; !known {
			return "", nil, NewValidationError(
				"tools.linear."+field,
				fmt.Sprintf("%v", config[field]),
				fmt.Sprintf("unknown Linear tool property %q", field),
				"Valid properties are: token, toolsets, allowed, required.",
			)
		}
	}

	token := constants.LinearMCPDefaultTokenExpr
	if value, exists := config["token"]; exists {
		var ok bool
		token, ok = value.(string)
		if !ok || strings.TrimSpace(token) == "" {
			return "", nil, NewValidationError(
				"tools.linear.token",
				fmt.Sprintf("%v", value),
				"'token' must be a GitHub Actions secret reference",
				"Example:\n\ntools:\n  linear:\n    token: ${{ secrets.CUSTOM_LINEAR_TOKEN }}",
			)
		}
	}
	if !parser.IsSimpleSecretExpression(token) {
		return "", nil, NewValidationError(
			"tools.linear.token",
			"[redacted]",
			"'token' must be a GitHub Actions secret reference so the Linear credential is not embedded in the compiled workflow",
			"Store the credential as a repository secret, then use:\n\ntools:\n  linear:\n    token: ${{ secrets.LINEAR_API_KEY }}",
		)
	}

	if value, exists := config["required"]; exists {
		if _, valid := value.(bool); !valid {
			return "", nil, errors.New("tools.linear.required must be a boolean")
		}
	}
	allowed, err := parseLinearAllowed(config["allowed"])
	if err != nil {
		return "", nil, err
	}
	if toolsetsValue, exists := config["toolsets"]; exists {
		toolsetTools, err := parser.ParseLinearToolsets(toolsetsValue)
		if err != nil {
			return "", nil, err
		}
		if len(allowed) == 0 {
			allowed = toolsetTools
		} else if err := parser.ValidateLinearAllowedForToolsets(allowed, toolsetTools); err != nil {
			return "", nil, err
		}
	}
	return token, allowed, nil
}

func parseLinearAllowed(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	allowed, valid := value.([]any)
	if !valid || len(allowed) == 0 {
		return nil, errors.New("tools.linear.allowed must be a non-empty array of tool names")
	}
	result := make([]string, 0, len(allowed))
	for _, tool := range allowed {
		name, valid := tool.(string)
		if !valid || strings.TrimSpace(name) == "" {
			return nil, errors.New("tools.linear.allowed must contain only non-empty tool names")
		}
		result = append(result, name)
	}
	return result, nil
}
