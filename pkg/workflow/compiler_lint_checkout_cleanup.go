package workflow

import (
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/goccy/go-yaml"
)

func (c *Compiler) artipackedCheckoutLintPaths(graph *actions.Workflow) (map[string]struct{}, error) {
	retained, retainedErr := c.safeOutputRetainedCheckoutLintPaths(graph)
	cleaned, cleanupErr := c.verifiedCleanupCheckoutLintPaths(graph)
	paths := make(map[string]struct{}, len(retained)+len(cleaned))
	for path := range retained {
		paths[path] = struct{}{}
	}
	for path := range cleaned {
		paths[path] = struct{}{}
	}
	return paths, errors.Join(retainedErr, cleanupErr)
}

// Cleanup is eligible only when compiler provenance proves the entire block and
// the very next step runs both the canonical cleanup and its fail-closed verifier,
// and the checkout config is within their bounded discovery coverage.
func (c *Compiler) verifiedCleanupCheckoutLintPaths(graph *actions.Workflow) (map[string]struct{}, error) {
	if len(c.agentCheckoutLintBlocks) == 0 {
		return nil, nil
	}
	var cleanup []map[string]any
	if err := yaml.Unmarshal([]byte(generateCheckoutCredentialsCleanupStep()), &cleanup); err != nil {
		return nil, fmt.Errorf("parse compiler-owned checkout cleanup for linting: %w", err)
	}
	paths := make(map[string]struct{})
	for _, block := range c.agentCheckoutLintBlocks {
		var expected []map[string]any
		if err := yaml.Unmarshal([]byte(block), &expected); err != nil {
			return nil, fmt.Errorf("parse compiler-owned agent checkout steps for linting: %w", err)
		}
		if len(expected) < 2 {
			continue
		}
		for _, job := range graph.Jobs {
			if job.ID != "agent" {
				continue
			}
			match := -1
			for start := range job.Steps {
				if !matchesPRCheckoutBlock(job.Steps, start, expected) {
					continue
				}
				if match >= 0 {
					match = -1 // Ambiguous authored copies must not inherit provenance.
					break
				}
				match = start
			}
			if match < 0 {
				continue
			}
			for index, step := range expected[:len(expected)-1] {
				uses, isAction := step["uses"].(string)
				with, hasInputs := step["with"].(map[string]any)
				if !isAction || !hasInputs || !strings.HasPrefix(uses, "actions/checkout@") || with["persist-credentials"] != true {
					continue
				}
				if !checkoutWithinCleanupDiscovery(with) {
					continue
				}
				if len(cleanup) == 1 && reflect.DeepEqual(expected[index+1], cleanup[0]) { //nolint:reflectdeepequalusage,uncheckedsliceindex // Compare full fields; the range excludes the last step and cleanup length is checked.
					paths[fmt.Sprintf("jobs.agent.steps[%d].with.persist-credentials", match+index)] = struct{}{}
				}
			}
		}
	}
	return paths, nil
}

func checkoutWithinCleanupDiscovery(with map[string]any) bool {
	value, present := with["path"]
	if !present {
		return true
	}
	checkoutPath, literal := value.(string)
	if !literal || strings.Contains(checkoutPath, "${{") || strings.ContainsAny(checkoutPath, ":\\\r\n") {
		return false
	}
	checkoutPath = strings.TrimSpace(checkoutPath)
	if validateCheckoutPath(checkoutPath) != nil {
		return false
	}
	// Both scripts find regular .git/config files at maxdepth 15 from the
	// workspace. Reserve two components for .git/config; backslashes above
	// remain unproven because their separator semantics depend on the platform.
	cleaned := path.Clean(checkoutPath)
	return cleaned == "." || strings.Count(cleaned, "/")+1 <= 13
}
