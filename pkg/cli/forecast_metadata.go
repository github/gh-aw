package cli

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// loadWorkflowMeta reads the workflow's Markdown file and extracts frontmatter metadata.
// Errors are non-fatal; a partial result is returned on failure.
func loadWorkflowMeta(workflowName string, verbose bool) workflowMeta {
	meta := workflowMeta{}

	// Try to find the Markdown source file.
	mdFile := findMarkdownFileForWorkflow(workflowName)
	if mdFile == "" {
		forecastRunLog.Printf("Markdown file not found for workflow %q", workflowName)
		return meta
	}

	content, err := os.ReadFile(mdFile)
	if err != nil {
		forecastRunLog.Printf("Failed to read Markdown file %q: %v", mdFile, err)
		return meta
	}

	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || result.Frontmatter == nil {
		forecastRunLog.Printf("Failed to parse frontmatter for %q: %v", workflowName, err)
		return meta
	}

	cfg, err := workflow.ParseFrontmatterConfig(result.Frontmatter)
	if err != nil || cfg == nil {
		forecastRunLog.Printf("Failed to build FrontmatterConfig for %q: %v", workflowName, err)
		return meta
	}

	// Collect active trigger names.
	meta.activeTriggers = extractTriggerNames(cfg)

	// Concurrency limit: read the `cancel-in-progress` or derive from the concurrency map.
	meta.concurrencyLimit = extractConcurrencyLimit(cfg)

	// Collect experiment variant names (counts come from run history later).
	meta.variants = extractExperimentVariantStubs(cfg)
	meta.engines = extractEngineNames(cfg)

	return meta
}

func extractEngineNames(cfg *workflow.FrontmatterConfig) []string {
	seen := make(map[string]struct{})
	var names []string
	var collect func(any)
	collect = func(value any) {
		switch typed := value.(type) {
		case string:
			name := strings.TrimSpace(typed)
			if name == "" {
				return
			}
			if _, exists := seen[name]; exists {
				return
			}
			seen[name] = struct{}{}
			names = append(names, name)
		case []any:
			for _, entry := range typed {
				collect(entry)
			}
		case map[string]any:
			if id, ok := typed["id"]; ok {
				collect(id)
			}
			if engine, ok := typed["engine"]; ok {
				collect(engine)
			}
			if fallback, ok := typed["fallback"]; ok {
				collect(fallback)
			}
			if fallbacks, ok := typed["fallbacks"]; ok {
				collect(fallbacks)
			}
			if engines, ok := typed["engines"]; ok {
				collect(engines)
			}
		}
	}
	collect(cfg.Engine)
	sort.Strings(names)
	return names
}

// findMarkdownFileForWorkflow tries to locate the .md source file for a workflow.
func findMarkdownFileForWorkflow(workflowName string) string {
	// workflowName might be a display name like "CI Doctor" or a lock file like "ci-doctor.lock.yml".
	// Try to reverse-engineer the md file path.
	candidates := []string{
		fmt.Sprintf(".github/workflows/%s.md", workflowName),
	}
	// Strip known suffixes.
	for _, sfx := range []string{".lock.yml", ".yml", ".yaml"} {
		if base, ok := strings.CutSuffix(workflowName, sfx); ok {
			// Also strip ".lock" from lock files.
			base, _ = strings.CutSuffix(base, ".lock")
			candidates = append(candidates, fmt.Sprintf(".github/workflows/%s.md", base))
		}
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// extractTriggerNames returns the list of active trigger event names from a workflow config.
func extractTriggerNames(cfg *workflow.FrontmatterConfig) []string {
	if cfg.On == nil {
		return nil
	}
	names := sliceutil.SortedKeys(cfg.On)
	return names
}

// extractConcurrencyLimit returns the workflow-level concurrency limit.
// Returns 0 when unlimited (no concurrency config) and 1 when concurrency is configured
// (either via cancel-in-progress or a concurrency group, since GitHub Actions queues at
// most one pending run when a concurrency group is set).
func extractConcurrencyLimit(cfg *workflow.FrontmatterConfig) int {
	if cfg.Concurrency == nil {
		return 0
	}
	// When concurrency is configured with cancel-in-progress: true, effective concurrency = 1.
	if typeutil.ParseBool(cfg.Concurrency, "cancel-in-progress") {
		return 1
	}
	// When there's a concurrency group without cancel-in-progress, runs queue up; treat as 1
	// active at a time by convention (GitHub Actions queues at most one pending run).
	if _, hasGroup := cfg.Concurrency["group"]; hasGroup {
		return 1
	}
	return 0
}

// extractExperimentVariantStubs extracts experiment variant metadata from frontmatter.
// Run counts are not yet known at this stage; they are populated from run history later.
func extractExperimentVariantStubs(cfg *workflow.FrontmatterConfig) []ForecastVariantResult {
	if len(cfg.ExperimentConfigs) == 0 {
		return nil
	}
	stubs := make([]ForecastVariantResult, 0)
	for expName, expCfg := range cfg.ExperimentConfigs {
		if expCfg == nil {
			continue
		}
		for _, variant := range expCfg.Variants {
			stubs = append(stubs, ForecastVariantResult{
				ExperimentName: expName,
				Variant:        variant,
			})
		}
	}
	slices.SortFunc(stubs, func(a, b ForecastVariantResult) int {
		if a.ExperimentName != b.ExperimentName {
			if a.ExperimentName < b.ExperimentName {
				return -1
			}
			return 1
		}
		switch {
		case a.Variant < b.Variant:
			return -1
		case a.Variant > b.Variant:
			return 1
		default:
			return 0
		}
	})
	return stubs
}

// computeVariantFractions populates run counts and fractions on the variant stubs
// by examining the DisplayTitle of sampled runs (gh-aw encodes the variant there).
// When no stubs are present (workflow has no experiments), returns nil.
func computeVariantFractions(stubs []ForecastVariantResult, runs []WorkflowRun) []ForecastVariantResult {
	if len(stubs) == 0 {
		return nil
	}

	total := len(runs)
	if total == 0 {
		return stubs
	}

	// Count how many run titles contain each variant name.
	for i, stub := range stubs {
		count := 0
		for _, r := range runs {
			if strings.Contains(r.DisplayTitle, stub.Variant) {
				count++
			}
		}
		stubs[i].RunCount = count
		stubs[i].Fraction = float64(count) / float64(total)
	}
	return stubs
}

// extractWorkflowIDFromName returns the short workflow ID from a display/lock name.
func extractWorkflowIDFromName(name string) string {
	for _, sfx := range []string{".lock.yml", ".yml", ".yaml"} {
		if base, ok := strings.CutSuffix(name, sfx); ok {
			base, _ = strings.CutSuffix(base, ".lock")
			name = base
		}
	}
	return name
}
