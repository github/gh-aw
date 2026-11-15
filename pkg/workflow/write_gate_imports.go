package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func jobsContainWriteGateField(jobs map[string]any) bool {
	for name, raw := range jobs {
		if normalizeBuiltinJobAlias(name) != string(constants.SafeOutputsJobName) {
			continue
		}
		if config, ok := raw.(map[string]any); ok {
			if _, exists := config["gate-steps"]; exists {
				return true
			}
		}
	}
	return false
}

func normalizeWriteGateImportJobs(main map[string]any, imports string) (map[string]any, string, error) {
	parsed := make([]map[string]any, 0)
	gated := jobsContainWriteGateField(main)
	for _, line := range strings.Split(imports, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var jobs map[string]any
		if err := json.Unmarshal([]byte(line), &jobs); err != nil {
			if gated {
				return nil, "", fmt.Errorf("imported write-gate jobs could not be parsed: %w. Use valid job objects in shared workflows", err)
			}
			continue
		}
		gated = gated || jobsContainWriteGateField(jobs)
		parsed = append(parsed, jobs)
	}
	if !gated {
		return main, imports, nil
	}
	normalized, err := normalizeWriteGateJobAliases(main)
	if err != nil {
		return nil, "", err
	}
	lines := make([]string, 0, len(parsed))
	for _, jobs := range parsed {
		jobs, err := normalizeWriteGateJobAliases(jobs)
		if err != nil {
			return nil, "", err
		}
		bytes, err := json.Marshal(jobs)
		if err != nil {
			return nil, "", fmt.Errorf("imported write-gate jobs could not be encoded: %w", err)
		}
		lines = append(lines, string(bytes))
	}
	return normalized, strings.Join(lines, "\n"), nil
}

func normalizeWriteGateJobAliases(jobs map[string]any) (map[string]any, error) {
	result := maps.Clone(jobs)
	if result == nil {
		result = make(map[string]any)
	}
	canonical := string(constants.SafeOutputsJobName)
	alias := string(constants.SafeOutputsHyphenJobName)
	if raw, exists := result[alias]; exists {
		if _, duplicate := result[canonical]; duplicate {
			return nil, fmt.Errorf("jobs.%s and jobs.%s both configure the write-gate job. Use only jobs.%s", canonical, alias, canonical)
		}
		result[canonical] = raw
		delete(result, alias)
	}
	if raw, exists := result[canonical]; exists {
		config, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("jobs.safe_outputs expects an object; imported write gates cannot be replaced with a scalar. Example: safe_outputs: {}")
		}
		if value, exists := config["gate-steps"]; exists {
			if _, ok := value.([]any); !ok {
				return nil, fmt.Errorf("jobs.safe_outputs.gate-steps expects an additive array; imported gate steps cannot be removed with null or false. Example: gate-steps: []")
			}
		}
	}
	return result, nil
}
