package workflow

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var runtimeDeduplicationLog = logger.New("workflow:runtime_deduplication")

// DeduplicateRuntimeSetupStepsFromCustomSteps removes runtime setup action steps from custom steps
// to avoid duplication when runtime steps are added before custom steps.
// This function parses the YAML custom steps, removes any steps that use runtime setup actions,
// and returns the deduplicated YAML.
//
// It preserves user-customized setup actions (e.g., with specific versions) and filters the corresponding
// runtime from the requirements so we don't generate a duplicate runtime setup step.
func DeduplicateRuntimeSetupStepsFromCustomSteps(customSteps string, runtimeRequirements []RuntimeRequirement) (string, []RuntimeRequirement, error) {
	if customSteps == "" || len(runtimeRequirements) == 0 {
		return customSteps, runtimeRequirements, nil
	}

	runtimeDeduplicationLog.Printf("Deduplicating runtime setup steps from custom steps (%d runtimes)", len(runtimeRequirements))

	// Extract version comments from uses lines before unmarshaling
	// This is necessary because YAML treats "# comment" as a comment, not part of the value
	// Format: "uses: action@sha # v1.0.0" -> after unmarshal, only "action@sha" remains
	versionComments := runtimeSetupVersionComments(customSteps)

	// Parse custom steps YAML
	var stepsWrapper map[string]any
	if err := yaml.Unmarshal([]byte(customSteps), &stepsWrapper); err != nil {
		return customSteps, runtimeRequirements, fmt.Errorf("failed to parse custom workflow steps from frontmatter. Custom steps must be valid GitHub Actions step syntax. Example:\nsteps:\n  - name: Setup\n    run: echo 'hello'\n  - name: Build\n    run: make build\nError: %w", err)
	}
	stepsVal, hasSteps := stepsWrapper["steps"]
	if !hasSteps {
		return customSteps, runtimeRequirements, nil
	}
	steps, ok := stepsVal.([]any)
	if !ok {
		return customSteps, runtimeRequirements, nil
	}
	actionRepoToReq := runtimeSetupActionRequirements(runtimeRequirements)
	filteredSteps, filteredRuntimeIDs, removedCount, preservedCount := filterRuntimeSetupSteps(steps, actionRepoToReq)
	if removedCount == 0 && preservedCount == 0 {
		runtimeDeduplicationLog.Print("  No duplicate runtime setup steps found")
		return customSteps, runtimeRequirements, nil
	}
	runtimeDeduplicationLog.Printf("  Removed %d duplicate runtime setup steps, preserved %d user-customized steps", removedCount, preservedCount)
	return marshalDeduplicatedRuntimeSteps(customSteps, runtimeRequirements, stepsWrapper, filteredSteps, filteredRuntimeIDs, versionComments)
}

func runtimeSetupVersionComments(customSteps string) map[string]string {
	versionComments := make(map[string]string)
	lines := strings.SplitSeq(customSteps, "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "uses:") && strings.Contains(trimmed, " # ") {
			// Extract the uses value and version comment
			parts := strings.SplitN(trimmed, " # ", 2)
			if len(parts) == 2 {
				usesValue := strings.TrimSpace(strings.TrimPrefix(parts[0], "uses:"))
				versionComment := " # " + parts[1]
				versionComments[usesValue] = versionComment
			}
		}
	}

	return versionComments
}

func runtimeSetupActionRequirements(runtimeRequirements []RuntimeRequirement) map[string]*RuntimeRequirement {
	// Build map of action repos to runtime requirements
	actionRepoToReq := make(map[string]*RuntimeRequirement)
	for i := range runtimeRequirements {
		if runtimeRequirements[i].Runtime.ActionRepo != "" {
			actionRepoToReq[runtimeRequirements[i].Runtime.ActionRepo] = &runtimeRequirements[i]
			runtimeDeduplicationLog.Printf("  Will check steps using action: %s", runtimeRequirements[i].Runtime.ActionRepo)
		}
	}
	return actionRepoToReq
}

func filterRuntimeSetupSteps(steps []any, actionRepoToReq map[string]*RuntimeRequirement) ([]any, map[string]struct{}, int, int) {
	// Track which runtimes to filter from requirements (user has custom setup)
	filteredRuntimeIDs := make(map[string]struct {
	})

	// Filter out steps that use runtime setup actions
	// BUT: Preserve steps that have user-specified customizations
	var filteredSteps []any
	removedCount := 0
	preservedCount := 0
	for _, stepAny := range steps {
		step, ok := stepAny.(map[string]any)
		if !ok {
			filteredSteps = append(filteredSteps, stepAny)
			continue
		}

		// Check if this step uses a runtime setup action
		usesVal, hasUses := step["uses"]
		if !hasUses {
			filteredSteps = append(filteredSteps, stepAny)
			continue
		}

		usesStr, ok := usesVal.(string)
		if !ok {
			filteredSteps = append(filteredSteps, stepAny)
			continue
		}

		shouldRemove, shouldPreserve := classifyRuntimeSetupStep(step, usesStr, actionRepoToReq, filteredRuntimeIDs)
		if shouldRemove {
			removedCount++
		}
		if shouldPreserve {
			preservedCount++
		}

		if shouldPreserve || !shouldRemove {
			filteredSteps = append(filteredSteps, stepAny)
		}
	}
	return filteredSteps, filteredRuntimeIDs, removedCount, preservedCount
}

func classifyRuntimeSetupStep(step map[string]any, usesStr string, actionRepoToReq map[string]*RuntimeRequirement, filteredRuntimeIDs map[string]struct{}) (bool, bool) {
	for actionRepo, req := range actionRepoToReq {
		if strings.Contains(usesStr, actionRepo) {
			withVal, hasWith := step["with"]
			if hasWith {
				withMap, isMap := withVal.(map[string]any)
				if isMap && len(withMap) > 0 {
					if runtimeSetupHasCustomization(withMap, req) {
						filteredRuntimeIDs[req.Runtime.ID] = struct{}{}
						runtimeDeduplicationLog.Printf("  Preserving user-customized runtime setup step: %s", usesStr)
						return false, true
					}
					captureRuntimeSetupExtraFields(withMap, req)
				}
			}
			runtimeDeduplicationLog.Printf("  Removing duplicate runtime setup step: %s", usesStr)
			return true, false
		}
	}
	return false, false
}

func marshalDeduplicatedRuntimeSteps(customSteps string, runtimeRequirements []RuntimeRequirement, stepsWrapper map[string]any, filteredSteps []any, filteredRuntimeIDs map[string]struct{}, versionComments map[string]string) (string, []RuntimeRequirement, error) {
	// Filter runtime requirements to exclude those with user-customized setup actions
	var filteredRequirements []RuntimeRequirement
	for _, req := range runtimeRequirements {
		if !setutil.Contains(filteredRuntimeIDs, req.Runtime.ID) {
			filteredRequirements = append(filteredRequirements, req)
		} else {
			runtimeDeduplicationLog.Printf("  Excluding runtime %s from generated setup steps (user has custom setup)", req.Runtime.ID)
		}
	}

	// Convert back to YAML
	stepsWrapper["steps"] = filteredSteps

	// Restore version comments to steps that have them
	// This must be done before marshaling
	for i, step := range filteredSteps {
		if stepMap, ok := step.(map[string]any); ok {
			if usesVal, hasUses := stepMap["uses"]; hasUses {
				if usesStr, ok := usesVal.(string); ok {
					if versionComment, hasComment := versionComments[usesStr]; hasComment {
						// Add the version comment back
						stepMap["uses"] = usesStr + versionComment
						filteredSteps[i] = stepMap
					}
				}
			}
		}
	}

	deduplicatedYAML, err := yaml.Marshal(stepsWrapper)
	if err != nil {
		return customSteps, runtimeRequirements, fmt.Errorf("failed to marshal deduplicated workflow steps to YAML. Step deduplication removes duplicate runtime setup actions (like actions/setup-node) from custom steps to avoid conflicts when automatic runtime detection adds them. This optimization ensures runtime setup steps appear before custom steps. Error: %w", err)
	}

	// Remove quotes from uses values with version comments
	// The YAML marshaller quotes strings containing # (for inline version comments)
	// but GitHub Actions expects unquoted uses values
	deduplicatedStr := unquoteUsesWithComments(string(deduplicatedYAML))

	return deduplicatedStr, filteredRequirements, nil
}

func runtimeSetupHasCustomization(withMap map[string]any, req *RuntimeRequirement) bool {
	hasCustomization := false
	if req.Runtime.ID == "go" {
		for key := range withMap {
			if key != "go-version-file" && key != "cache" {
				hasCustomization = true
				break
			}
		}
		if !hasCustomization {
			if goVersionFile, ok := withMap["go-version-file"]; ok {
				if goVersionFileStr, isStr := goVersionFile.(string); isStr {
					if goVersionFileStr != "go.mod" {
						hasCustomization = true
					}
				}
			}
		}
	} else if req.Runtime.VersionField != "" {
		if userVersion, hasVersion := withMap[req.Runtime.VersionField]; hasVersion {
			userVersionStr := fmt.Sprintf("%v", userVersion)
			if req.Runtime.DefaultVersion != "" && userVersionStr != req.Runtime.DefaultVersion {
				hasCustomization = true
			} else if req.Version != "" && userVersionStr != req.Version {
				hasCustomization = true
			} else if req.Runtime.DefaultVersion == "" && req.Version == "" {
				hasCustomization = true
			}
		}
		if req.Runtime.ID == "node" {
			if nodeVersionFile, ok := withMap["node-version-file"].(string); ok && strings.TrimSpace(nodeVersionFile) != "" {
				hasCustomization = true
			}
		}
	}
	return hasCustomization
}

func captureRuntimeSetupExtraFields(withMap map[string]any, req *RuntimeRequirement) {
	if req.ExtraFields == nil {
		req.ExtraFields = make(map[string]any)
	}
	for key, value := range withMap {
		if req.Runtime.VersionField != "" && key == req.Runtime.VersionField {
			continue
		}
		if req.Runtime.ID == "go" && (key == "go-version-file" || key == "cache") {
			continue
		}
		if req.Runtime.ID == "uv" && key == "enable-cache" {
			continue
		}
		req.ExtraFields[key] = value
		runtimeDeduplicationLog.Printf("  Capturing extra field from setup step: %s = %v", key, value)
	}
}
