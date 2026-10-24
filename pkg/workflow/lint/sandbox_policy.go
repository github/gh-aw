package lint

import (
	"context"
	"errors"
	"fmt"
)

func SandboxConfigRule() Rule {
	return Rule{
		ID: "sandbox-config", Description: "Keep the agent sandbox enabled and avoid internal sandbox overrides",
		ThreatRuleID: "CTR-004",
		Severity:     Warning, Targets: AWAST, Check: checkSandboxConfig,
	}
}

func checkSandboxConfig(ctx context.Context, input Input) ([]Diagnostic, error) {
	source, err := authoredPolicyMapping(ctx, input)
	if err != nil || source == nil {
		return nil, err
	}
	if shorthand, ok := source["sandbox"].(string); ok && (shorthand == "awf" || shorthand == "default") {
		return nil, nil
	}
	config, err := policyMapping(source["sandbox"], "sandbox")
	if err != nil || config == nil {
		return nil, err
	}
	severity, err := sourcePolicySeverity(input, "sandbox-config")
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	var failures []error
	if disabled, ok := config["agent"].(bool); ok && !disabled {
		diagnostics = append(diagnostics, Diagnostic{Severity: severity, Path: "sandbox.agent",
			Message: "Disabling the agent sandbox removes a trust boundary; keep the sandbox enabled"})
	}
	for _, section := range []struct {
		name   string
		fields []string
	}{
		{"agent", []string{"command", "args", "env"}},
		{"mcp", []string{"container", "version", "entrypoint", "args", "entrypoint-args"}},
	} {
		raw := config[section.name]
		switch raw.(type) {
		case nil, string, bool:
			continue
		}
		fields, err := policyMapping(raw, "sandbox."+section.name)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		for _, key := range section.fields {
			path := "sandbox." + section.name + "." + key
			enabled, err := sandboxOverride(ctx, fields[key], key, path)
			failures = append(failures, err)
			if enabled {
				diagnostics = append(diagnostics, Diagnostic{Severity: severity, Path: path,
					Message: "Internal sandbox overrides can change the trusted execution boundary; remove this customization"})
			}
		}
	}
	return diagnostics, errors.Join(failures...)
}

func sandboxOverride(ctx context.Context, raw any, key, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if raw == nil {
		return false, nil
	}
	switch key {
	case "env":
		values, err := policyMapping(raw, path)
		return len(values) > 0, err
	case "args", "entrypoint-args":
		values, err := policyStrings(ctx, raw, path)
		return len(values) > 0, err
	default:
		value, ok := raw.(string)
		if !ok {
			return false, fmt.Errorf("sandbox-config requires a string at %s", path)
		}
		return value != "", nil
	}
}
