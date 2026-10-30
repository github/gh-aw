package lint

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/cachepolicy"
)

func cacheWriteOverrides(ctx context.Context, input Input, blocks []aw.ExecutionBlock) ([]Diagnostic, error) {
	var root any
	var present bool
	if input.Workflow != nil {
		root = input.Workflow.CacheMode
		present = input.Workflow.CacheModeSet || input.Workflow.CacheMode != ""
	} else {
		root, present = input.Document.Frontmatter["cache-mode"]
	}
	rootMode, err := cachePolicyMode(root, "cache-mode", present)
	var diagnostics []Diagnostic
	failures := []error{err}
	reported := make(map[string]struct{})
	check := func(value any, present bool, path string) {
		field := "cache-mode"
		if path != "" {
			field = path + ".cache-mode"
		}
		mode, err := cachePolicyMode(value, field, present)
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
				Message: "Explicit cache-write access can override the read-only default outside GitHub's write-capable trigger allowlist; " +
					"use cache-mode: read for untrusted execution and review reusable callers, publisher input validation, and cache consumers",
			})
		}
	}
	if input.Workflow != nil {
		for _, job := range input.Workflow.Jobs {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			check(job.CacheMode, job.CacheModeSet || job.CacheMode != "", "jobs."+string(job.ID))
		}
	} else {
		for _, block := range blocks {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			if block.Path == "" || strings.HasPrefix(block.Path, "jobs.") ||
				strings.HasPrefix(block.Path, "safe-jobs.") || strings.HasPrefix(block.Path, "safe-outputs.jobs.") {
				if block.Path != "" {
					value, present := block.Fields["cache-mode"]
					check(value, present, block.Path)
				} else {
					check(nil, false, "")
				}
			}
		}
	}
	return diagnostics, errors.Join(failures...)
}

func cacheLowTrustTrigger(ctx context.Context, on any) (bool, error) {
	for _, event := range cachepolicy.ReadOnlyEvents() {
		if _, found, err := authoredTrigger(ctx, on, event); err != nil || found {
			return found, err
		}
	}
	return false, nil
}

func cachePolicyMode(value any, path string, present bool) (string, error) {
	if !present {
		return "", nil
	}
	if mode, ok := value.(string); ok {
		switch mode {
		case "read", "write", "write-only", "none":
			return mode, nil
		}
	}
	return "", fmt.Errorf("cache-poisoning requires read, write, write-only, or none at %s; correct the cache access mode before linting", path)
}
