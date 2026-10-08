package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/goccy/go-yaml"
)

// validateRuntimeSetupCaches checks user setup steps that bypass generated runtime defaults.
func (c *Compiler) validateRuntimeSetupCaches(data *WorkflowData) error {
	customSteps, _, err := DeduplicateRuntimeSetupStepsFromCustomSteps(data.CustomSteps, detectRuntimeRequirementsCached(data))
	if err != nil {
		return err
	}

	var offendingSteps []string
	sections := []string{data.PreSteps, customSteps, data.PreAgentSteps, data.PostSteps}
	sections = append(sections, agentJobSetupStepSections(data)...)
	for _, section := range sections {
		if !strings.Contains(strings.ToLower(section), "astral-sh/setup-uv") {
			continue
		}
		var wrapper map[string][]map[string]any
		if err := yaml.Unmarshal([]byte(section), &wrapper); err != nil {
			return fmt.Errorf("failed to parse runtime setup steps: %w", err)
		}
		for _, steps := range wrapper {
			for _, step := range steps {
				if uvSetupCacheEnabled(step) {
					offendingSteps = append(offendingSteps, stepDisplayName(step))
				}
			}
		}
	}
	if len(offendingSteps) == 0 {
		return nil
	}

	msg := fmt.Sprintf("astral-sh/setup-uv step(s) without 'enable-cache: false' detected in the agent job: %s. "+
		"Actions caches can save agent-written files and expose other repository jobs to cache poisoning. "+
		"Add 'enable-cache: false' to the 'with:' block of each setup-uv step.", strings.Join(offendingSteps, ", "))
	if c.effectiveStrictMode(data.RawFrontmatter) {
		return fmt.Errorf("strict mode: %s", msg)
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(msg))
	c.IncrementWarningCount()
	return nil
}

func uvSetupCacheEnabled(step map[string]any) bool {
	uses, ok := step["uses"].(string)
	if !ok {
		return false
	}

	uses, _, _ = strings.Cut(uses, " #")
	repo, _, _ := strings.Cut(strings.TrimSpace(uses), "@")
	if !strings.EqualFold(repo, "astral-sh/setup-uv") {
		return false
	}
	if with, ok := step["with"].(map[string]any); ok {
		if value, ok := with["enable-cache"].(bool); ok {
			return value
		}
		if value, ok := with["enable-cache"].(string); ok {
			return value != "false"
		}
	}
	return true
}

func agentJobSetupStepSections(data *WorkflowData) []string {
	if data == nil || data.Jobs == nil {
		return nil
	}
	agentConfig, ok := data.Jobs["agent"].(map[string]any)
	if !ok {
		return nil
	}

	var sections []string
	for _, field := range []string{"setup-steps", "pre-steps"} {
		steps, exists := agentConfig[field]
		if !exists {
			continue
		}
		section, err := yaml.Marshal(map[string]any{field: steps})
		if err == nil {
			sections = append(sections, string(section))
		}
	}
	return sections
}
