package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/sliceutil"
)

// Consume quoted literals with references so bracket access cannot disguise a
// secret context. Only statically named, non-secret policy inputs are supported.
var goRepositoryPolicyTokenPattern = regexp.MustCompile(`'(?:[^']|'')*'|[A-Za-z_][A-Za-z0-9_-]*(?:\s*\.\s*[A-Za-z_][A-Za-z0-9_-]*|\s*\[\s*'(?:[^']|'')*'\s*\])*`)
var goRepositoryPolicyBracketPattern = regexp.MustCompile(`\s*\[\s*'([^']*)'\s*\]`)
var goRepositoryPolicyDotPattern = regexp.MustCompile(`\s*\.\s*`)

func validateGoRepositoryPolicyExpressions(policy map[string]any) error {
	for _, field := range sliceutil.SortedKeys(policy) {
		if err := visitGoRepositoryPolicyStrings(policy[field], validateGoRepositoryPolicyString); err != nil {
			return fmt.Errorf("engine.tool-profile: go-repository policy %s: %w", field, err)
		}
	}
	return nil
}

func visitGoRepositoryPolicyStrings(value any, visit func(string) error) error {
	switch value := value.(type) {
	case string:
		return visit(value)
	case templatableJSONExpression:
		return visit(value.expr)
	case []string:
		for _, item := range value {
			if err := visit(item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range value {
			if err := visitGoRepositoryPolicyStrings(item, visit); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, key := range sliceutil.SortedKeys(value) {
			if err := visitGoRepositoryPolicyStrings(value[key], visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateGoRepositoryPolicyString(value string) error {
	if err := validateBalancedBraces(value); err != nil {
		return err
	}
	if err := validateExpressionSyntax(value); err != nil {
		return err
	}
	remainder := ExpressionPatternDotAll.ReplaceAllString(value, "")
	if strings.Contains(remainder, "${") {
		return errors.New("literal environment placeholders are not supported in profile policy")
	}
	for _, match := range ExpressionPatternDotAll.FindAllStringSubmatch(value, -1) {
		if err := validateGoRepositoryPolicyExpression(match[1]); err != nil {
			return err
		}
	}
	return nil
}

func validateGoRepositoryPolicyExpression(expression string) error {
	for _, match := range goRepositoryPolicyTokenPattern.FindAllStringIndex(expression, -1) {
		token := expression[match[0]:match[1]]
		if strings.HasPrefix(token, "'") {
			if strings.Contains(token, "${") {
				return errors.New("literal environment placeholders are not supported in profile policy expressions")
			}
			continue
		}
		reference := goRepositoryPolicyBracketPattern.ReplaceAllString(token, ".$1")
		reference = strings.ToLower(goRepositoryPolicyDotPattern.ReplaceAllString(reference, "."))
		if strings.HasPrefix(reference, "inputs.") || strings.HasPrefix(reference, "vars.") || strings.HasPrefix(reference, "github.event.inputs.") {
			continue
		}
		switch reference {
		case "github.repository", "github.repository_id", "github.repository_owner", "github.repository_owner_id",
			"github.event.repository.default_branch", "github.event.repository.name", "github.event.repository.full_name",
			"github.ref", "github.ref_name", "github.base_ref", "github.head_ref", "github.sha",
			"github.ref_type", "github.ref_protected", "github.event_name", "github.actor", "github.triggering_actor",
			"github.run_id", "github.run_number", "github.run_attempt", "true", "false", "null":
			continue
		}
		if slices.Contains([]string{"format", "join", "fromjson", "tojson", "contains", "startswith", "endswith"}, reference) &&
			strings.HasPrefix(strings.TrimSpace(expression[match[1]:]), "(") {
			continue
		}
		return fmt.Errorf("expression reference %q is not a supported non-secret policy input; use inputs, vars, or repository/branch metadata, not secrets, env, steps, or needs (job outputs may be secret or unavailable in the agent job)", token)
	}
	return nil
}

// Simple inputs retain the safe-outputs ${ENV} transport. Other expressions use
// the existing typed toJSON slots, evaluated by Actions in env:, never in shell.
func buildGoRepositoryToolConfigRuntimeData(configJSON string) (string, map[string]string, error) {
	sanitized, _, env := buildSafeOutputsConfigRuntimeData(configJSON)
	sanitized = strings.ReplaceAll(sanitized, goRepositoryDefaultBranchExpression, "${"+goRepositoryDefaultBranchEnv+"}")
	env[goRepositoryDefaultBranchEnv] = goRepositoryDefaultBranchExpression
	var config map[string]any
	if err := json.Unmarshal([]byte(sanitized), &config); err != nil {
		return "", nil, fmt.Errorf("decoding Go repository SDK tool config: %w", err)
	}
	profile, ok := config["profile"].(map[string]any)
	if !ok {
		return "", nil, errors.New("Go repository SDK tool config requires a profile object")
	}
	policy, ok := profile["policy"].(map[string]any)
	if !ok {
		return "", nil, errors.New("Go repository SDK tool config requires a policy object")
	}
	for key, value := range policy {
		policy[key] = bindGoRepositoryTypedExpressions(value)
	}
	encoded, err := marshalSafeOutputsConfig(config)
	if err != nil {
		return "", nil, fmt.Errorf("encoding Go repository SDK tool config: %w", err)
	}
	return string(encoded), env, nil
}

func bindGoRepositoryTypedExpressions(value any) any {
	switch value := value.(type) {
	case string:
		matches := ExpressionPatternDotAll.FindAllStringSubmatchIndex(value, -1)
		if len(matches) == 0 {
			return value
		}
		if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(value) {
			return newTemplatableJSONExpression(wrapExpressionWithToJSON(value))
		}
		var format strings.Builder
		var args []string
		offset := 0
		for index, match := range matches {
			format.WriteString(escapeGoRepositoryExpressionFormat(value[offset:match[0]]))
			fmt.Fprintf(&format, "{%d}", index)
			args = append(args, strings.TrimSpace(value[match[2]:match[3]]))
			offset = match[1]
		}
		format.WriteString(escapeGoRepositoryExpressionFormat(value[offset:]))
		expression := "${{ format('" + strings.ReplaceAll(format.String(), "'", "''") + "', " + strings.Join(args, ", ") + ") }}"
		return newTemplatableJSONExpression(wrapExpressionWithToJSON(expression))
	case []any:
		for index, item := range value {
			value[index] = bindGoRepositoryTypedExpressions(item)
		}
		return value
	case map[string]any:
		for key, item := range value {
			value[key] = bindGoRepositoryTypedExpressions(item)
		}
		return value
	default:
		return value
	}
}

func escapeGoRepositoryExpressionFormat(value string) string {
	return strings.NewReplacer("{", "{{", "}", "}}").Replace(value)
}
