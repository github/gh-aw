package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var actionPinsLog = logger.New("workflow:action_pins")

//go:embed data/action_pins.json
var actionPinsJSON []byte

// formatActionReference formats an action reference with repo, SHA, and version.
// Example: "actions/checkout@abc123 # v4.1.0"
func formatActionReference(repo, sha, version string) string {
	return fmt.Sprintf("%s@%s # %s", repo, sha, version)
}

// formatActionCacheKey generates a cache key for action resolution.
// Example: "actions/checkout@v4"
func formatActionCacheKey(repo, version string) string {
	return fmt.Sprintf("%s@%s", repo, version)
}

// ActionPin represents a pinned GitHub Action with its commit SHA
type ActionPin struct {
	Repo    string `json:"repo"`    // e.g., "actions/checkout"
	Version string `json:"version"` // e.g., "v5" - the golden/default version
	SHA     string `json:"sha"`     // Full commit SHA for the pinned version
}

// ActionPinsData represents the structure of the embedded JSON file
// This matches the schema used by ActionCache for consistency
type ActionPinsData struct {
	Entries map[string]ActionPin `json:"entries"` // key: "repo@version"
}

var (
	// cachedActionPins holds the parsed and sorted action pins
	cachedActionPins []ActionPin
	// actionPinsOnce ensures the action pins are loaded only once
	actionPinsOnce sync.Once
)

// getActionPins returns the action pins from the embedded JSON
// Returns a sorted slice of action pins (by version descending, then by repo name)
// The data is parsed once on first call and cached for subsequent calls
func getActionPins() []ActionPin {
	actionPinsOnce.Do(func() {
		actionPinsLog.Print("Unmarshaling action pins from embedded JSON (first call, will be cached)")

		var data ActionPinsData
		if err := json.Unmarshal(actionPinsJSON, &data); err != nil {
			actionPinsLog.Printf("Failed to unmarshal action pins JSON: %v", err)
			panic(fmt.Sprintf("failed to load action pins: %v", err))
		}

		// Detect and log key/version mismatches
		mismatchCount := 0
		for key, pin := range data.Entries {
			// Extract version from key (format: "repo@version")
			if idx := strings.LastIndex(key, "@"); idx != -1 {
				keyVersion := key[idx+1:]
				if keyVersion != pin.Version {
					mismatchCount++
					// Safely truncate SHA for logging
					shortSHA := pin.SHA
					if len(pin.SHA) > 8 {
						shortSHA = pin.SHA[:8]
					}
					actionPinsLog.Printf("WARNING: Key/version mismatch in action_pins.json: key=%s has version=%s but pin.Version=%s (sha=%s)",
						key, keyVersion, pin.Version, shortSHA)
				}
			}
		}
		if mismatchCount > 0 {
			actionPinsLog.Printf("Found %d key/version mismatches in action_pins.json", mismatchCount)
		}

		// Convert map values to slice - immutable initialization
		pins := make([]ActionPin, 0, len(data.Entries))
		for _, pin := range data.Entries {
			pins = append(pins, pin)
		}

		// Sort by version (descending) then by repo name (ascending)
		// Use standard library sort for better performance O(n log n) vs O(n²)
		sort.Slice(pins, func(i, j int) bool {
			// Compare versions first (descending order - higher version first)
			if pins[i].Version != pins[j].Version {
				return pins[i].Version > pins[j].Version
			}
			// Same version, sort by repo name (ascending order)
			return pins[i].Repo < pins[j].Repo
		})

		actionPinsLog.Printf("Successfully unmarshaled and sorted %d action pins from JSON", len(pins))
		cachedActionPins = pins
	})

	return cachedActionPins
}

// sortPinsByVersion sorts action pins by version in descending order (highest first).
// This function returns a new sorted slice without modifying the input.
// This is an immutable operation for better safety and clarity.
func sortPinsByVersion(pins []ActionPin) []ActionPin {
	// Create a copy to avoid mutating the input
	result := make([]ActionPin, len(pins))
	copy(result, pins)

	sort.Slice(result, func(i, j int) bool {
		// Strip 'v' prefix for comparison
		v1 := strings.TrimPrefix(result[i].Version, "v")
		v2 := strings.TrimPrefix(result[j].Version, "v")
		// Return true if v1 > v2 to get descending order
		return compareVersions(v1, v2) > 0
	})

	return result
}

// GetActionPin returns the pinned action reference for a given action repository
// When multiple versions exist for the same repo, it returns the latest version by semver
// If no pin is found, it returns an empty string
// The returned reference includes a comment with the version tag (e.g., "repo@sha # v1")
func GetActionPin(actionRepo string) string {
	actionPins := getActionPins()

	// Find all pins matching the repo - using functional filter
	matchingPins := sliceutil.Filter(actionPins, func(pin ActionPin) bool {
		return pin.Repo == actionRepo
	})

	if len(matchingPins) == 0 {
		// If no pin exists, return empty string to signal that this action is not pinned
		return ""
	}

	// Sort matching pins by version (descending - latest first) - immutable operation
	sortedPins := sortPinsByVersion(matchingPins)

	// Return the latest version (first after sorting)
	latestPin := sortedPins[0]
	return formatActionReference(actionRepo, latestPin.SHA, latestPin.Version)
}

// GetActionPinWithData returns the pinned action reference for a given action@version
// It tries dynamic resolution first, then falls back to hardcoded pins
// If strictMode is true and resolution fails, it returns an error
// The returned reference includes a comment with the version tag (e.g., "repo@sha # v1")
func GetActionPinWithData(actionRepo, version string, data *WorkflowData) (string, error) {
	actionPinsLog.Printf("Resolving action pin: repo=%s, version=%s, strict_mode=%t", actionRepo, version, data.StrictMode)

	// Check if version is already a full 40-character SHA
	isAlreadySHA := isValidFullSHA(version)

	// First try dynamic resolution if resolver is available (but not for SHAs, as they can't be resolved)
	if data.ActionResolver != nil && !isAlreadySHA {
		actionPinsLog.Printf("Attempting dynamic resolution for %s@%s", actionRepo, version)
		sha, err := data.ActionResolver.ResolveSHA(actionRepo, version)
		if err == nil && sha != "" {
			actionPinsLog.Printf("Dynamic resolution succeeded: %s@%s → %s", actionRepo, version, sha)

			// Successfully resolved - cache will be saved at end of compilation
			actionPinsLog.Printf("Successfully resolved action pin (cache marked dirty, will save at end)")
			result := formatActionReference(actionRepo, sha, version)
			actionPinsLog.Printf("Returning pinned reference: %s", result)
			return result, nil
		}
		actionPinsLog.Printf("Dynamic resolution failed for %s@%s: %v", actionRepo, version, err)
	} else {
		if isAlreadySHA {
			actionPinsLog.Printf("Version is already a SHA, skipping dynamic resolution")
		} else {
			actionPinsLog.Printf("No action resolver available, skipping dynamic resolution")
		}
	}

	// Dynamic resolution failed, try hardcoded pins
	actionPinsLog.Printf("Falling back to hardcoded pins for %s@%s", actionRepo, version)
	actionPins := getActionPins()

	// Find all pins matching the repo - using functional filter
	matchingPins := sliceutil.Filter(actionPins, func(pin ActionPin) bool {
		return pin.Repo == actionRepo
	})

	if len(matchingPins) == 0 {
		// No pins found for this repo, will handle below
		actionPinsLog.Printf("No hardcoded pins found for %s", actionRepo)
	} else {
		actionPinsLog.Printf("Found %d hardcoded pin(s) for %s", len(matchingPins), actionRepo)

		// Sort matching pins by version (descending - highest first) - immutable operation
		matchingPins = sortPinsByVersion(matchingPins)

		// First, try to find an exact version match (for version tags)
		for _, pin := range matchingPins {
			if pin.Version == version {
				actionPinsLog.Printf("Exact version match: requested=%s, found=%s", version, pin.Version)
				return formatActionReference(actionRepo, pin.SHA, pin.Version), nil
			}
		}

		// If version is a SHA, check if it matches any hardcoded pin's SHA
		if isAlreadySHA {
			for _, pin := range matchingPins {
				if pin.SHA == version {
					actionPinsLog.Printf("Exact SHA match: requested=%s, found version=%s", version, pin.Version)
					return formatActionReference(actionRepo, pin.SHA, pin.Version), nil
				}
			}
			// SHA provided but doesn't match any hardcoded pin - return it as-is without warnings
			actionPinsLog.Printf("SHA %s not found in hardcoded pins, returning as-is", version)
			return formatActionReference(actionRepo, version, version), nil
		}

		// No exact match found
		// In non-strict mode, find the highest semver-compatible version
		// Semver compatibility means respecting major version boundaries
		// (e.g., v5 -> highest v5.x.x, not v6.x.x)
		if !data.StrictMode && len(matchingPins) > 0 {
			// Filter for semver-compatible pins (matching major version) - using functional filter
			compatiblePins := sliceutil.Filter(matchingPins, func(pin ActionPin) bool {
				return isSemverCompatible(pin.Version, version)
			})

			// If we found compatible pins, use the highest one (first after sorting)
			// Otherwise fall back to the highest overall pin
			var selectedPin ActionPin
			if len(compatiblePins) > 0 {
				selectedPin = compatiblePins[0]
				actionPinsLog.Printf("No exact match for version %s, using highest semver-compatible version: %s", version, selectedPin.Version)
			} else {
				selectedPin = matchingPins[0]
				actionPinsLog.Printf("No exact match for version %s, no semver-compatible versions found, using highest available: %s", version, selectedPin.Version)
			}

			// Only emit warning if the version is not a SHA (SHAs shouldn't generate warnings)
			if !isAlreadySHA {
				// Initialize the warning cache if needed
				if data.ActionPinWarnings == nil {
					data.ActionPinWarnings = make(map[string]bool)
				}

				// Create a cache key for this action@version combination
				cacheKey := formatActionCacheKey(actionRepo, version)

				// Only emit warning if we haven't already warned about this action
				if !data.ActionPinWarnings[cacheKey] {
					warningMsg := fmt.Sprintf("Unable to resolve %s@%s dynamically, using hardcoded pin for %s@%s",
						actionRepo, version, actionRepo, selectedPin.Version)
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
					data.ActionPinWarnings[cacheKey] = true
				}
			}
			actionPinsLog.Printf("Using version in non-strict mode: %s@%s (requested) → %s@%s (used)",
				actionRepo, version, actionRepo, selectedPin.Version)
			// Use the requested version in the comment, not the pin's version
			// This preserves the user's intent (e.g., v8 instead of v8.0.0)
			return formatActionReference(actionRepo, selectedPin.SHA, version), nil
		}
	}

	// No pin available
	if data.StrictMode {
		errMsg := fmt.Sprintf("Unable to pin action %s@%s", actionRepo, version)
		if data.ActionResolver != nil {
			errMsg = fmt.Sprintf("Unable to pin action %s@%s: resolution failed", actionRepo, version)
		}
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return "", fmt.Errorf("%s", errMsg)
	}

	// In non-strict mode, emit warning and return empty string (unless it's already a SHA)
	if isAlreadySHA {
		// If version is already a SHA and we couldn't find it in pins, return it as-is without warnings
		actionPinsLog.Printf("SHA %s not found in hardcoded pins, returning as-is", version)
		return formatActionReference(actionRepo, version, version), nil
	}

	// Initialize the warning cache if needed
	if data.ActionPinWarnings == nil {
		data.ActionPinWarnings = make(map[string]bool)
	}

	// Create a cache key for this action@version combination
	cacheKey := formatActionCacheKey(actionRepo, version)

	// Only emit warning if we haven't already warned about this action
	if !data.ActionPinWarnings[cacheKey] {
		warningMsg := fmt.Sprintf("Unable to pin action %s@%s", actionRepo, version)
		if data.ActionResolver != nil {
			warningMsg = fmt.Sprintf("Unable to pin action %s@%s: resolution failed", actionRepo, version)
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
		data.ActionPinWarnings[cacheKey] = true
	}
	return "", nil
}

// ApplyActionPinToTypedStep applies SHA pinning to a WorkflowStep if it uses an action.
// Returns a modified copy of the step with pinned references.
// If the step doesn't use an action or the action is not pinned, returns the original step.
func ApplyActionPinToTypedStep(step *WorkflowStep, data *WorkflowData) *WorkflowStep {
	// Check if step uses an action
	if step == nil || !step.IsUsesStep() {
		return step
	}

	actionPinsLog.Printf("Applying action pin to step: uses=%s", step.Uses)

	// Extract action repo and version from uses field
	actionRepo := extractActionRepo(step.Uses)
	if actionRepo == "" {
		return step
	}

	version := extractActionVersion(step.Uses)
	if version == "" {
		// No version specified, can't pin
		return step
	}

	// Try to get pinned SHA
	pinnedRef, err := GetActionPinWithData(actionRepo, version, data)
	if err != nil {
		// In strict mode, this would have already been handled by GetActionPinWithData
		// In normal mode, we just return the original step
		return step
	}

	if pinnedRef == "" {
		// No pin available for this action, return original step
		return step
	}

	actionPinsLog.Printf("Pinning action: %s@%s -> %s", actionRepo, version, pinnedRef)

	// Create a copy of the step with the pinned reference
	result := step.Clone()
	result.Uses = pinnedRef

	return result
}

// extractActionRepo extracts the action repository from a uses string
// For example:
//   - "actions/checkout@v5" -> "actions/checkout"
//   - "actions/setup-node@v6" -> "actions/setup-node"
//   - "github/codeql-action/upload-sarif@v3" -> "github/codeql-action/upload-sarif"
//   - "actions/checkout" -> "actions/checkout"
func extractActionRepo(uses string) string {
	// Split on @ to separate repo from version/ref
	idx := strings.Index(uses, "@")
	if idx == -1 {
		// No @ found, return the whole string
		return uses
	}
	// Return everything before the @
	return uses[:idx]
}

// extractActionVersion extracts the version from a uses string
// For example:
//   - "actions/checkout@v5" -> "v4"
//   - "actions/setup-node@v6" -> "v5"
//   - "actions/checkout" -> ""
func extractActionVersion(uses string) string {
	// Split on @ to separate repo from version/ref
	idx := strings.Index(uses, "@")
	if idx == -1 {
		// No @ found, no version
		return ""
	}
	// Return everything after the @
	return uses[idx+1:]
}

// ApplyActionPinsToTypedSteps applies SHA pinning to a slice of typed WorkflowStep pointers
// Returns a new slice with pinned references - this is the type-safe version
func ApplyActionPinsToTypedSteps(steps []*WorkflowStep, data *WorkflowData) []*WorkflowStep {
	actionPinsLog.Printf("Applying action pins to %d typed steps", len(steps))
	if steps == nil {
		return nil
	}

	result := make([]*WorkflowStep, 0, len(steps))
	for i, step := range steps {
		if step == nil {
			actionPinsLog.Printf("Skipping nil step at index %d", i)
			result = append(result, nil)
			continue
		}

		pinnedStep := ApplyActionPinToTypedStep(step, data)
		result = append(result, pinnedStep)
	}

	actionPinsLog.Printf("Successfully applied pins to %d typed steps", len(result))
	return result
}

// GetActionPinByRepo returns the ActionPin for a given repository, if it exists
// When multiple versions exist for the same repo, it returns the latest version by semver
func GetActionPinByRepo(repo string) (ActionPin, bool) {
	actionPins := getActionPins()

	// Find all pins matching the repo - using functional filter
	matchingPins := sliceutil.Filter(actionPins, func(pin ActionPin) bool {
		return pin.Repo == repo
	})

	if len(matchingPins) == 0 {
		return ActionPin{}, false
	}

	// Sort matching pins by version (descending - latest first) - immutable operation
	sortedPins := sortPinsByVersion(matchingPins)

	// Return the latest version (first after sorting)
	return sortedPins[0], true
}
