package lint

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func sourcePolicySeverity(input Input, ruleID string) (Severity, error) {
	if input.Document == nil {
		return Warning, nil
	}

	value, exists := input.Document.Frontmatter["strict"]
	if !exists {
		return Error, nil
	}
	strict, ok := value.(bool)
	if !ok {
		return "", fmt.Errorf("%s requires AW strict to be a boolean; correct the frontmatter before linting", ruleID)
	}
	if strict {
		return Error, nil
	}
	return Warning, nil
}

var errNullImportList = errors.New("import list must not be null")

func authoredImportEntries(frontmatter map[string]any) (entries []any, path string, nested, present bool, err error) {
	path = "imports"
	value, present := frontmatter["imports"]
	if !present {
		return nil, path, false, false, nil
	}
	if imports, ok := value.(map[string]any); ok {
		nested = true
		path += ".aw"
		value, present = imports["aw"]
		if !present {
			return nil, path, nested, false, nil
		}
	}
	switch imports := value.(type) {
	case nil:
		return nil, path, nested, true, errNullImportList
	case []any:
		return imports, path, nested, true, nil
	case []string:
		for _, entry := range imports {
			entries = append(entries, entry)
		}
		return entries, path, nested, true, nil
	default:
		return nil, path, nested, true, fmt.Errorf("%s must be a list", path)
	}
}

func authoredImportsPending(input Input) (bool, error) {
	if input.Document == nil {
		return false, nil
	}
	entries, _, nested, _, err := authoredImportEntries(input.Document.Frontmatter)
	if errors.Is(err, errNullImportList) {
		// Top-level null has historically meant no inherited configuration.
		if nested {
			return false, errors.New("authored policy requires an imports.aw list to determine whether configuration may be inherited")
		}
		return false, nil
	}
	if err != nil {
		return false, errors.New("authored policy requires an imports list or an imports mapping with an aw list to determine whether configuration may be inherited")
	}
	return len(entries) > 0, nil
}

// VersionCheckRule inspects authored policy, not the presence of a generated
// update step or CLI/repository strict-mode overrides.
func VersionCheckRule() Rule {
	return Rule{
		ID: "version-check", Description: "Keep the AW version integrity check enabled",
		ThreatRuleID: "CTR-018",
		Severity:     Warning, Targets: AWAST, Check: checkVersionCheck,
	}
}

func checkVersionCheck(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	if input.Document == nil {
		return nil, nil
	}
	severity, err := sourcePolicySeverity(input, "version-check")
	if err != nil {
		return nil, err
	}
	value, exists := input.Document.Frontmatter["check-for-updates"]
	if !exists {
		return nil, nil
	}
	enabled, ok := value.(bool)
	if !ok {
		return nil, errors.New("version-check requires check-for-updates to be a literal boolean; correct the AW frontmatter")
	}
	if enabled {
		return nil, nil
	}
	return []Diagnostic{{
		Severity: severity, Path: "check-for-updates",
		Message: "AW version integrity checks are disabled; remove check-for-updates or set it to true",
	}}, nil
}

func PrivateToPublicRule() Rule {
	return Rule{
		ID: "private-to-public", Description: "Keep AW private-to-public flow protections enabled",
		ThreatRuleID: "CTR-031",
		Severity:     Warning, Targets: AWAST, Check: checkPrivateToPublic,
	}
}

func checkPrivateToPublic(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	if input.Document == nil {
		return nil, nil
	}
	severity, err := sourcePolicySeverity(input, "private-to-public")
	if err != nil {
		return nil, err
	}
	tools, err := policyMapping(input.Document.Frontmatter["tools"], "tools")
	if err != nil {
		return nil, err
	}
	if _, shorthand := tools["github"].(bool); shorthand {
		return nil, nil
	}
	github, err := policyMapping(tools["github"], "tools.github")
	if err != nil {
		return nil, err
	}
	value := github["private-to-public-flows"]
	if value == nil {
		return nil, nil
	}
	if err := privateToPublicValue(ctx, value); err != nil {
		return nil, err
	}
	return []Diagnostic{{
		Severity: severity, Path: "tools.github.private-to-public-flows",
		Message: "AW private-to-public flow protections are disabled for all or selected servers; remove private-to-public-flows to retain the guard",
	}}, nil
}

func privateToPublicValue(ctx context.Context, value any) error {
	const invalid = "private-to-public requires tools.github.private-to-public-flows to be allow or a list of non-empty server IDs"
	switch value := value.(type) {
	case string:
		if value == "allow" {
			return nil
		}
	case []string:
		for _, item := range value {
			if err := ctx.Err(); err != nil {
				return err
			}
			if strings.TrimSpace(item) == "" {
				return errors.New(invalid)
			}
		}
		return nil
	case []any:
		for _, item := range value {
			if err := ctx.Err(); err != nil {
				return err
			}
			if text, ok := item.(string); !ok || strings.TrimSpace(text) == "" {
				return errors.New(invalid)
			}
		}
		return nil
	}
	return errors.New(invalid)
}

// ConditionalImportRule inspects only authored imports. It neither loads
// imported files nor treats runtime prompt conditionals as compile-time imports.
func ConditionalImportRule() Rule {
	return Rule{
		ID: "conditional-import", Description: "Keep AW configuration imports unconditional",
		ThreatRuleID: "CTR-020",
		Severity:     Error, Targets: AWAST, Check: checkConditionalImports,
	}
}

func checkConditionalImports(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	if input.Document == nil {
		return nil, nil
	}
	entries, importsPath, _, present, err := authoredImportEntries(input.Document.Frontmatter)
	if err != nil {
		return nil, fmt.Errorf("conditional-import requires %s to be a list; correct the AW frontmatter", importsPath)
	}
	if !present {
		return nil, nil
	}
	var diagnostics []Diagnostic
	var failures []error
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return diagnostics, errors.Join(append(failures, err)...)
		}
		path := fmt.Sprintf("%s[%d]", importsPath, index)
		if text, ok := entry.(string); ok && strings.TrimSpace(text) != "" {
			continue
		}
		fields, err := policyMapping(entry, path)
		if err != nil || fields == nil {
			failures = append(failures, fmt.Errorf("conditional-import requires a non-empty path or import mapping at %s", path))
			continue
		}
		if err := importPolicyShape(fields, path); err != nil {
			failures = append(failures, err)
		}
		if _, conditional := fields["if"]; conditional {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    path + ".if",
				Message: "Compile-time AW imports cannot be conditional; remove if and use a conditional runtime prompt import when needed",
			})
		}
	}
	return diagnostics, errors.Join(failures...)
}

func importPolicyShape(fields map[string]any, path string) error {
	value, exists := fields["path"]
	if !exists {
		value = fields["uses"]
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return fmt.Errorf("conditional-import requires a non-empty literal path or uses field at %s", path)
	}
	for _, key := range []string{"inputs", "with"} {
		if value, exists := fields[key]; exists {
			mapping, err := policyMapping(value, path+"."+key)
			if err != nil || mapping == nil {
				return fmt.Errorf("conditional-import requires an input mapping at %s.%s", path, key)
			}
		}
	}
	return nil
}

func policyMapping(value any, path string) (map[string]any, error) {
	switch value := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return value, nil
	case map[string]string:
		fields := make(map[string]any, len(value))
		for key, item := range value {
			fields[key] = item
		}
		return fields, nil
	default:
		return nil, fmt.Errorf("AST policy lint requires a mapping at %s; correct the configuration before linting", path)
	}
}

func authoredPolicyMapping(ctx context.Context, input Input, keys ...string) (map[string]any, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	if input.Document == nil {
		return nil, nil
	}
	fields := input.Document.Frontmatter
	var path strings.Builder
	for _, key := range keys {
		if path.Len() != 0 {
			path.WriteByte('.')
		}
		path.WriteString(key)
		var err error
		fields, err = policyMapping(fields[key], path.String())
		if err != nil {
			return nil, err
		}
	}
	return fields, nil
}

func policyStrings(ctx context.Context, value any, path string) ([]string, error) {
	switch entries := value.(type) {
	case []string:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return entries, nil
	case []any:
		var result []string
		var failures []error
		for index, entry := range entries {
			if err := ctx.Err(); err != nil {
				return result, errors.Join(append(failures, err)...)
			}
			if text, ok := entry.(string); ok {
				result = append(result, text)
			} else {
				failures = append(failures, fmt.Errorf("AST policy lint requires a string at %s[%d]", path, index))
			}
		}
		return result, errors.Join(failures...)
	default:
		return nil, fmt.Errorf("AST policy lint requires a string list at %s", path)
	}
}
