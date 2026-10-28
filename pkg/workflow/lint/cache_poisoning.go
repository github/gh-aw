package lint

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CachePoisoningRule checks release/tag restores and explicit low-trust writes.
// It does not infer trust from job names or prove cached-file provenance.
func CachePoisoningRule() Rule {
	return Rule{
		ID: "cache-poisoning", Description: "Review shared cache restores and low-trust cache-write overrides",
		Severity: Warning, Targets: ActionsAST | AWAST, Check: checkCachePoisoning,
	}
}

func checkCachePoisoning(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	var on any
	if input.Workflow != nil {
		on = input.Workflow.On
	} else {
		on = input.Document.Frontmatter["on"]
	}
	release, err := cacheReleaseTrigger(ctx, on)
	if err != nil {
		return nil, err
	}
	lowTrust, err := cacheLowTrustTrigger(ctx, on)
	if err != nil || (!release && !lowTrust) {
		return nil, err
	}
	blocks, err := executionPolicyBlocks(ctx, input)
	failures := []error{err}
	var diagnostics []Diagnostic
	if lowTrust {
		diagnostics, err = cacheWriteOverrides(ctx, input, blocks)
		failures = append(failures, err)
	}
	if !release {
		return diagnostics, errors.Join(failures...)
	}
	for _, block := range blocks {
		for _, step := range block.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			restores, err := actionRestoresCache(ctx, step.Fields, step.Path)
			if err != nil {
				failures = append(failures, err)
			} else if restores {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    step.Path + ".uses",
					Message: "Release or tag workflow may restore a shared dependency cache; disable caching for release builds or establish a separately trusted cache boundary",
				})
			}
		}
	}
	return diagnostics, errors.Join(failures...)
}

func cacheReleaseTrigger(ctx context.Context, on any) (bool, error) {
	if _, found, err := authoredTrigger(ctx, on, "release"); err != nil || found {
		return found, err
	}
	push, found, err := authoredTrigger(ctx, on, "push")
	if err != nil || !found || push == nil {
		return false, err
	}
	fields, err := policyMapping(push, "on.push")
	if err != nil {
		return false, err
	}
	tags, exists := fields["tags"]
	if !exists {
		return false, nil
	}
	patterns, valid, err := workflowRunStrings(ctx, tags, false)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, errors.New("cache-poisoning requires a non-empty string list at on.push.tags; correct the trigger before linting")
	}
	for _, pattern := range patterns {
		if !strings.HasPrefix(pattern, "!") {
			return true, nil
		}
	}
	return false, nil
}

func actionRestoresCache(ctx context.Context, fields map[string]any, path string) (bool, error) {
	name := actionName(fields["uses"])
	switch name {
	case "actions/cache", "actions/cache/restore", "actions/setup-go", "actions/setup-node":
	default:
		return false, nil
	}
	with, err := policyMapping(fields["with"], path+".with")
	if err != nil {
		return false, err
	}
	flag, defaultEnabled, inverted := "cache", true, false
	switch name {
	case "actions/cache", "actions/cache/restore":
		flag, inverted = "lookup-only", true
	case "actions/setup-go":
		defaultEnabled = !actionMajorBefore(fields["uses"], 4)
	case "actions/setup-node":
		if value, explicit := with["cache"]; explicit && value != nil {
			text, ok := value.(string)
			if !ok {
				return false, fmt.Errorf("cache-poisoning requires a string at %s.with.cache; correct the action inputs before linting", path)
			}
			if text != "" {
				return true, nil
			}
		}
		flag, defaultEnabled = "package-manager-cache", !actionMajorBefore(fields["uses"], 5)
	}
	value, explicit := with[flag]
	if !explicit {
		return defaultEnabled, nil
	}
	enabled, known, err := actionInputBoolean(ctx, value, path+".with."+flag)
	if err != nil {
		return false, err
	}
	if !known {
		return true, nil
	}
	if inverted {
		enabled = !enabled
	}
	return enabled, nil
}

// SHA-pinned setup actions have unknown major versions here. Their default
// cache behavior is treated as potentially enabled, without a network lookup.
func actionMajorBefore(value any, boundary int) bool {
	uses, ok := value.(string)
	if !ok {
		return false
	}
	_, ref, _ := strings.Cut(uses, "@")
	version, tagged := strings.CutPrefix(ref, "v")
	if !tagged {
		return false
	}
	major, _, _ := strings.Cut(version, ".")
	number, err := strconv.Atoi(major)
	return err == nil && number > 0 && number < boundary
}
