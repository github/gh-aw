package workflow

import (
	"fmt"
	"sort"

	"github.com/github/gh-aw/pkg/logger"
)

var runtimeStepGeneratorLog = logger.New("workflow:runtime_step_generator")

// GenerateRuntimeSetupSteps creates GitHub Actions steps for runtime setup
func GenerateRuntimeSetupSteps(requirements []RuntimeRequirement) []GitHubActionStep {
	runtimeStepGeneratorLog.Printf("Generating runtime setup steps: requirement_count=%d", len(requirements))
	runtimeSetupLog.Printf("Generating runtime setup steps for %d requirements", len(requirements))
	var steps []GitHubActionStep

	for _, req := range requirements {
		steps = append(steps, generateSetupStep(&req))

		// Add environment variable capture steps after setup actions for AWF chroot mode.
		// Most env vars are inherited via AWF_HOST_PATH, but Go is special.
		switch req.Runtime.ID {
		case "go":
			// GitHub Actions uses "trimmed" Go binaries that require GOROOT to be explicitly set.
			// Unlike other runtimes where PATH is sufficient, Go's trimmed binaries need GOROOT
			// for /proc/self/exe resolution. actions/setup-go does NOT export GOROOT to the
			// environment, so we must capture it explicitly.
			runtimeStepGeneratorLog.Print("Adding GOROOT capture step for chroot mode compatibility")
			steps = append(steps, generateEnvCaptureStep("GOROOT", "go env GOROOT"))
		}
		// Note: Java and .NET don't need capture steps anymore because:
		// - AWF_HOST_PATH captures the complete host PATH including $JAVA_HOME/bin and $DOTNET_ROOT
		// - AWF's entrypoint.sh exports PATH="${AWF_HOST_PATH}" which preserves all setup-* additions
	}

	runtimeStepGeneratorLog.Printf("Generated %d runtime setup steps", len(steps))
	return steps
}

// generateEnvCaptureStep creates a step to capture an environment variable and export it.
// This is required because some setup actions don't export env vars, but AWF chroot mode
// needs them to be set in the environment to pass them to the container.
func generateEnvCaptureStep(envVar string, captureCmd string) GitHubActionStep {
	return GitHubActionStep{
		fmt.Sprintf("      - name: Capture %s for AWF chroot mode", envVar),
		fmt.Sprintf("        run: echo \"%s=$(%s)\" >> \"$GITHUB_ENV\"", envVar, captureCmd),
	}
}

// GenerateSerenaLanguageServiceSteps creates installation steps for Serena language services
// NOTE: This function is now obsolete since Serena runs in a Docker container.
// Language services are provided inside the container and do not require host installation.
// This function is kept for backward compatibility but returns an empty slice.
func GenerateSerenaLanguageServiceSteps(tools *ToolsConfig) []GitHubActionStep {
	runtimeStepGeneratorLog.Print("Serena language services provided inside container, no installation steps needed")
	runtimeSetupLog.Print("Serena language services are now provided inside the container - no installation steps needed")

	// Return empty slice - no steps needed since Serena runs in a container
	// with all language services pre-installed
	return []GitHubActionStep{}
}

// generateSetupStep creates a setup step for a given runtime requirement
func generateSetupStep(req *RuntimeRequirement) GitHubActionStep {
	runtime := req.Runtime
	version := req.Version
	runtimeStepGeneratorLog.Printf("Generating setup step for runtime: %s, version=%s", runtime.ID, version)
	runtimeSetupLog.Printf("Generating setup step for runtime: %s, version=%s", runtime.ID, version)
	// Use default version if none specified
	if version == "" {
		version = runtime.DefaultVersion
	}

	// Use SHA-pinned action reference for security if available
	actionRef := GetActionPin(runtime.ActionRepo)

	// If no pin exists (custom action repo), use the action repo with its version
	if actionRef == "" {
		if runtime.ActionVersion != "" {
			actionRef = fmt.Sprintf("%s@%s", runtime.ActionRepo, runtime.ActionVersion)
		} else {
			// Fallback to just the repo name (shouldn't happen in practice)
			actionRef = runtime.ActionRepo
		}
	}

	step := GitHubActionStep{
		fmt.Sprintf("      - name: Setup %s", runtime.Name),
		fmt.Sprintf("        uses: %s", actionRef),
	}

	// Special handling for Go when go-mod-file is explicitly specified
	if runtime.ID == "go" && req.GoModFile != "" {
		step = append(step, "        with:")
		step = append(step, fmt.Sprintf("          go-version-file: %s", req.GoModFile))
		step = append(step, "          cache: true")
		// Add any extra fields from user's setup step (sorted for stable output)
		var extraKeys []string
		for key := range req.ExtraFields {
			extraKeys = append(extraKeys, key)
		}
		sort.Strings(extraKeys)
		for _, key := range extraKeys {
			valueStr := formatYAMLValue(req.ExtraFields[key])
			step = append(step, fmt.Sprintf("          %s: %s", key, valueStr))
		}
		return step
	}

	// Add version field if we have a version
	if version != "" {
		step = append(step, "        with:")
		step = append(step, fmt.Sprintf("          %s: '%s'", runtime.VersionField, version))
	} else if runtime.ID == "uv" {
		// For uv without version, no with block needed (unless there are extra fields)
		if len(req.ExtraFields) == 0 {
			return step
		}
		step = append(step, "        with:")
	}

	// Merge extra fields from runtime configuration and user's setup step
	// User fields take precedence over runtime fields
	// Note: runtime.ExtraWithFields are pre-formatted strings, req.ExtraFields need formatting
	allExtraFields := make(map[string]string)

	// Add runtime extra fields (already formatted)
	for k, v := range runtime.ExtraWithFields {
		allExtraFields[k] = v
	}

	// Add user extra fields (need formatting), these override runtime fields
	for k, v := range req.ExtraFields {
		allExtraFields[k] = formatYAMLValue(v)
	}

	// Output merged extra fields in sorted key order for stable output
	var allKeys []string
	for key := range allExtraFields {
		allKeys = append(allKeys, key)
	}
	sort.Strings(allKeys)
	for _, key := range allKeys {
		step = append(step, fmt.Sprintf("          %s: %s", key, allExtraFields[key]))
		log.Printf("  Added extra field to runtime setup: %s = %s", key, allExtraFields[key])
	}

	return step
}

// formatYAMLValue formats a value for YAML output
func formatYAMLValue(value any) string {
	switch v := value.(type) {
	case string:
		// Quote strings if they contain special characters or look like non-string types
		if v == "true" || v == "false" || v == "null" {
			return fmt.Sprintf("'%s'", v)
		}
		// Check if it's a number
		if _, err := fmt.Sscanf(v, "%f", new(float64)); err == nil {
			return fmt.Sprintf("'%s'", v)
		}
		// Return as-is for simple strings, quote for complex ones
		return fmt.Sprintf("'%s'", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", v)
	case int8:
		return fmt.Sprintf("%d", v)
	case int16:
		return fmt.Sprintf("%d", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case uint:
		return fmt.Sprintf("%d", v)
	case uint8:
		return fmt.Sprintf("%d", v)
	case uint16:
		return fmt.Sprintf("%d", v)
	case uint32:
		return fmt.Sprintf("%d", v)
	case uint64:
		return fmt.Sprintf("%d", v)
	case float32:
		return fmt.Sprintf("%v", v)
	case float64:
		return fmt.Sprintf("%v", v)
	default:
		// For other types, convert to string and quote
		return fmt.Sprintf("'%v'", v)
	}
}
