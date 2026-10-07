package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// generateCacheSteps generates cache steps for the workflow based on cache configuration
func generateCacheSteps(builder *strings.Builder, data *WorkflowData, verbose bool) {
	if data.Cache == "" {
		return
	}

	cacheLog.Print("Generating cache steps from frontmatter cache configuration")
	builder.WriteString("      # Cache configuration from frontmatter processed below\n")
	caches, err := parseCacheStepConfigs(data.Cache)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: Failed to parse cache configuration: %v\n", err)
		}
		return
	}
	for i, cache := range caches {
		if data.DryRun {
			writeCacheStepWithAction(builder, cache, i, len(caches), "actions/cache/restore")
		} else {
			writeCacheStep(builder, cache, i, len(caches))
		}
	}
}

func parseCacheStepConfigs(cacheYAML string) ([]map[string]any, error) {
	var topLevel map[string]any
	if err := yaml.Unmarshal([]byte(cacheYAML), &topLevel); err != nil {
		return nil, err
	}
	cacheConfig, exists := topLevel["cache"]
	if !exists {
		return nil, errors.New("no cache key found in parsed configuration")
	}
	if cacheArray, isArray := cacheConfig.([]any); isArray {
		cacheLog.Printf("Processing %d cache entries (array format)", len(cacheArray))
		return normalizeCacheStepArray(cacheArray), nil
	}
	if cacheMap, isMap := cacheConfig.(map[string]any); isMap {
		cacheLog.Print("Processing single cache entry (object format)")
		return []map[string]any{cacheMap}, nil
	}
	return nil, nil
}

func normalizeCacheStepArray(cacheArray []any) []map[string]any {
	caches := make([]map[string]any, 0, len(cacheArray))
	for _, cacheItem := range cacheArray {
		if cacheMap, ok := cacheItem.(map[string]any); ok {
			caches = append(caches, cacheMap)
		}
	}
	return caches
}

func writeCacheStep(builder *strings.Builder, cache map[string]any, idx int, total int) {
	writeCacheStepWithAction(builder, cache, idx, total, "actions/cache")
}

func writeCacheStepWithAction(builder *strings.Builder, cache map[string]any, idx int, total int, action string) {
	stepName := resolveCacheStepName(cache, idx, total)
	fmt.Fprintf(builder, "      - name: %s\n", stepName)
	fmt.Fprintf(builder, "        uses: %s\n", getActionPin(action))
	builder.WriteString("        with:\n")
	writeCacheStepValue(builder, "key", cache["key"])
	writeCachePath(builder, cache["path"])
	writeCacheRestoreKeys(builder, cache["restore-keys"])
	writeCacheStepValue(builder, "upload-chunk-size", cache["upload-chunk-size"])
	writeCacheStepValue(builder, "fail-on-cache-miss", cache["fail-on-cache-miss"])
	writeCacheStepValue(builder, "lookup-only", cache["lookup-only"])
}

func resolveCacheStepName(cache map[string]any, idx int, total int) string {
	stepName := "Cache"
	if total > 1 {
		stepName = fmt.Sprintf("Cache %d", idx+1)
	}
	if nameStr, ok := cache["name"].(string); ok && nameStr != "" {
		return nameStr
	}
	if keyStr, ok := cache["key"].(string); ok && keyStr != "" {
		return fmt.Sprintf("Cache (%s)", keyStr)
	}
	return stepName
}

func writeCachePath(builder *strings.Builder, path any) {
	if path == nil {
		return
	}
	if pathArray, isArray := path.([]any); isArray {
		builder.WriteString("          path: |\n")
		for _, p := range pathArray {
			fmt.Fprintf(builder, "            %v\n", p)
		}
		return
	}
	fmt.Fprintf(builder, "          path: %v\n", path)
}

// buildCacheRestoreKeys derives the ordered list of restore-keys for a cache entry.
// The primary key (without the run_id suffix) is always included.
// For "repo" scope, a second key that also strips the workflow ID is appended to allow
// cross-workflow cache sharing.
//
// cacheKey must be the fully-formed primary key (e.g. as returned by
// computeIntegrityCacheKey) and scope is the cache entry's scope field
// ("workflow" or "repo"; empty is treated as "workflow").
func buildCacheRestoreKeys(cacheKey, scope string) []string {
	if scope == "" {
		scope = "workflow"
	}
	const runIDSuffix = "-${{ github.run_id }}"

	var keys []string
	if strings.HasSuffix(cacheKey, runIDSuffix) {
		keys = append(keys, strings.TrimSuffix(cacheKey, "${{ github.run_id }}"))
	} else {
		parts := strings.Split(cacheKey, "-")
		if len(parts) >= 2 {
			keys = append(keys, strings.Join(parts[:len(parts)-1], "-")+"-")
		}
	}

	if scope == "repo" {
		repoKey := strings.TrimSuffix(cacheKey, "${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}")
		if repoKey != cacheKey && repoKey != "" {
			keys = append(keys, repoKey)
		}
	}
	return keys
}

func writeCacheRestoreKeys(builder *strings.Builder, restoreKeys any) {
	if restoreKeys == nil {
		return
	}
	if restoreArray, isArray := restoreKeys.([]any); isArray {
		builder.WriteString("          restore-keys: |\n")
		for _, key := range restoreArray {
			fmt.Fprintf(builder, "            %v\n", key)
		}
		return
	}
	fmt.Fprintf(builder, "          restore-keys: %v\n", restoreKeys)
}

func writeCacheStepValue(builder *strings.Builder, key string, value any) {
	if value != nil {
		fmt.Fprintf(builder, "          %s: %v\n", key, value)
	}
}
