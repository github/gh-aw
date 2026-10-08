package workflow

import (
	"fmt"
	"maps"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var runtimeStepGeneratorLog = logger.New("workflow:runtime_step_generator")

// GenerateRuntimeSetupSteps creates GitHub Actions steps for runtime setup.
// data is the WorkflowData for the workflow being compiled; it is forwarded to
// generateSetupStep so that the gh-aw setup-cli step can use the lock-file-aware
// pin resolver (getActionPinWithData) rather than the static embedded-pins fallback.
// Pass nil only when WorkflowData is unavailable or when tests specifically
// target non-gh-aw runtime behavior.
func GenerateRuntimeSetupSteps(requirements []RuntimeRequirement, data *WorkflowData) []GitHubActionStep {
	runtimeStepGeneratorLog.Printf("Generating runtime setup steps: requirement_count=%d", len(requirements))
	runtimeSetupLog.Printf("Generating runtime setup steps for %d requirements", len(requirements))
	var steps []GitHubActionStep

	for _, req := range requirements {
		steps = append(steps, generateSetupStep(&req, data))

		// Add environment variable capture steps after setup actions for AWF chroot mode.
		// Most env vars are inherited via AWF_HOST_PATH, but some runtimes need explicit capture.
		switch req.Runtime.ID {
		case "go":
			// GitHub Actions uses "trimmed" Go binaries that require GOROOT to be explicitly set.
			// Unlike other runtimes where PATH is sufficient, Go's trimmed binaries need GOROOT
			// for /proc/self/exe resolution. actions/setup-go does NOT export GOROOT to the
			// environment, so we must capture it explicitly.
			runtimeStepGeneratorLog.Print("Adding GOROOT capture step for chroot mode compatibility")
			steps = append(steps, generateEnvCaptureStep("GOROOT", "go env GOROOT"))
		case "elixir":
			// erlef/setup-beam installs OTP to ${RUNNER_TEMP}/.setup-beam/otp/ via core.addPath(),
			// not to RUNNER_TOOL_CACHE. GetNpmBinPathSetup() only scans RUNNER_TOOL_CACHE, so
			// the erl binary is not found in the AWF sandbox without this explicit capture.
			// Capture ERLANG_HOME (the OTP root) so GetNpmBinPathSetup() can add ERLANG_HOME/bin
			// to PATH inside the AWF container, making erl accessible to mix and elixir.
			runtimeStepGeneratorLog.Print("Adding ERLANG_HOME capture step for AWF chroot mode compatibility")
			steps = append(steps, generateEnvCaptureStep("ERLANG_HOME", `erl -noshell -eval 'io:format("~s", [code:root_dir()]), halt().'`))
		}
		// Note: Java and .NET don't need capture steps because:
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

// mergeRuntimeWithFields combines pre-formatted runtime defaults with user
// overrides, formatting req.ExtraFields for YAML as they are merged.
// User-provided fields take precedence over runtime defaults.
func mergeRuntimeWithFields(req *RuntimeRequirement) map[string]string {
	allExtraFields := make(map[string]string)

	maps.Copy(allExtraFields, req.Runtime.ExtraWithFields)

	for k, v := range req.ExtraFields {
		allExtraFields[k] = formatYAMLValue(v)
	}

	if req.Runtime.ID == "uv" {
		allExtraFields["enable-cache"] = "false"
	}

	return allExtraFields
}

// appendSortedWithFieldEntries appends pre-formatted with: entries in stable
// sorted key order so generated workflow output is deterministic.
func appendSortedWithFieldEntries(step GitHubActionStep, withFields map[string]string) GitHubActionStep {
	for _, key := range sliceutil.SortedKeys(withFields) {
		step = append(step, fmt.Sprintf("          %s: %s", key, withFields[key]))
		workflowLog.Printf("  Added extra with-field: %s = %s", key, withFields[key])
	}

	return step
}

// generateSetupStep creates a setup step for a given runtime requirement
func generateSetupStep(req *RuntimeRequirement, data *WorkflowData) GitHubActionStep {
	runtime := req.Runtime
	version := req.Version
	runtimeStepGeneratorLog.Printf("Generating setup step for runtime: %s, version=%s, if=%s", runtime.ID, version, req.IfCondition)
	runtimeSetupLog.Printf("Generating setup step for runtime: %s, version=%s, if=%s", runtime.ID, version, req.IfCondition)

	if runtime.ID == "gh-aw" {
		if version == "" {
			version = getDefaultGhAWRuntimeVersion()
		}

		step, err := generateGhAwSetupStep(ghAwSetupStepConfig{
			actionMode:           actionModeForRuntimeSetup(IsRelease()),
			ifCondition:          req.IfCondition,
			cliVersion:           version,
			actionRepo:           runtime.ActionRepo,
			fallbackActionRefTag: version,
			workflowData:         data,
			withFields:           mergeRuntimeWithFields(req),
		})
		if err != nil {
			runtimeStepGeneratorLog.Printf("Failed to resolve pinned setup-cli action reference for %s@%s: %v", runtime.ActionRepo, version, err)
		}
		return step
	}

	// Use default version if none specified.
	if version == "" {
		version = runtime.DefaultVersion
	}

	// Use SHA-pinned action reference for security if available
	actionRef := getActionPin(runtime.ActionRepo)

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
		"      - name: Setup " + runtime.Name,
	}

	// Add zizmor suppression for actions from creators not GitHub-verified on the Marketplace.
	// SHA-pinning mitigates mutable-ref risk, but publisher-identity risk remains; suppress
	// the informational finding since no verified-creator alternative exists.
	if runtime.UnverifiedCreator {
		step = append(step, "        # zizmor: ignore[github_action_from_unverified_creator_used]")
	}

	step = append(step, "        uses: "+actionRef)

	// Add if condition if specified
	if req.IfCondition != "" {
		step = append(step, "        if: "+req.IfCondition)
	}

	// Special handling for Go when go-mod-file is explicitly specified
	if runtime.ID == "go" && req.GoModFile != "" {
		withFields := mergeRuntimeWithFields(req)
		delete(withFields, "go-version-file")
		step = append(step, "        with:")
		step = append(step, "          go-version-file: "+req.GoModFile)
		return appendSortedWithFieldEntries(step, withFields)
	}

	withFields := mergeRuntimeWithFields(req)

	// Add version field if we have a version
	if version != "" {
		step = append(step, "        with:")
		step = append(step, fmt.Sprintf("          %s: '%s'", runtime.VersionField, version))
	} else if runtime.ID == "uv" {
		// For uv without version, no with block needed (unless there are extra fields)
		if len(withFields) == 0 {
			return step
		}
		step = append(step, "        with:")
	}

	return appendSortedWithFieldEntries(step, withFields)
}

func actionModeForRuntimeSetup(isRelease bool) ActionMode {
	if isRelease {
		return ActionModeRelease
	}
	return ActionModeDev
}
