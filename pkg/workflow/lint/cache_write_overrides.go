package lint

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/aw"
)

func cacheWriteOverrides(ctx context.Context, input Input, blocks []aw.ExecutionBlock) ([]Diagnostic, error) {
	var root any
	if input.Workflow != nil {
		root = input.Workflow.CacheMode
	} else {
		root = input.Document.Frontmatter["cache-mode"]
	}
	rootMode, err := cachePolicyMode(root, "cache-mode")
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	var failures []error
	reported := make(map[string]struct{})
	check := func(value any, path string) {
		field := "cache-mode"
		if path != "" {
			field = path + ".cache-mode"
		}
		mode, err := cachePolicyMode(value, field)
		if err != nil {
			failures = append(failures, err)
			return
		}
		if mode == "" {
			mode, field = rootMode, "cache-mode"
		}
		_, exists := reported[field]
		if (mode == "write" || mode == "write-only") && !exists {
			reported[field] = struct{}{}
			diagnostics = append(diagnostics, Diagnostic{
				Path: field,
				Message: "Explicit cache-write access overrides the read-only default for pull_request_target, issue_comment, or workflow_run; " +
					"use cache-mode: read for untrusted execution and review any dedicated cache publisher's input validation and cache consumers",
			})
		}
	}
	if input.Workflow != nil {
		for _, job := range input.Workflow.Jobs {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			check(job.CacheMode, "jobs."+string(job.ID))
		}
	} else {
		for _, block := range blocks {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			if block.Path == "" || strings.HasPrefix(block.Path, "jobs.") ||
				strings.HasPrefix(block.Path, "safe-jobs.") || strings.HasPrefix(block.Path, "safe-outputs.jobs.") {
				check(block.Fields["cache-mode"], block.Path)
			}
		}
	}
	return diagnostics, errors.Join(failures...)
}

func cacheLowTrustTrigger(ctx context.Context, on any) (bool, error) {
	for _, event := range []string{"pull_request_target", "issue_comment", "workflow_run"} {
		if _, found, err := authoredTrigger(ctx, on, event); err != nil || found {
			return found, err
		}
	}
	return false, nil
}

func cachePolicyMode(value any, path string) (string, error) {
	if value == nil {
		return "", nil
	}
	if mode, ok := value.(string); ok {
		switch mode {
		case "", "read", "write", "write-only", "none":
			return mode, nil
		}
	}
	return "", fmt.Errorf("cache-poisoning requires read, write, write-only, or none at %s; correct the cache access mode before linting", path)
}
