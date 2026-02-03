package workflow

import (
	"fmt"
	"os"
	"regexp"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var actionSHACheckerLog = logger.New("workflow:action_sha_checker")

// ActionUsage represents an action used in a workflow with its SHA
type ActionUsage struct {
	Repo    string // e.g., "actions/checkout"
	SHA     string // The SHA currently used
	Version string // The version tag if available (e.g., "v5")
}

// ActionUpdateCheck represents the result of checking if an action needs updating
type ActionUpdateCheck struct {
	Action      ActionUsage
	NeedsUpdate bool
	LatestSHA   string
	Message     string
}

// ExtractActionsFromLockFile parses a lock.yml file and extracts all action usages
func ExtractActionsFromLockFile(lockFilePath string) ([]ActionUsage, error) {
	actionSHACheckerLog.Printf("Extracting actions from lock file: %s", lockFilePath)

	content, err := os.ReadFile(lockFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read lock file: %w", err)
	}

	// Parse YAML to extract actions from "uses" fields
	var workflowData map[string]any
	if err := yaml.Unmarshal(content, &workflowData); err != nil {
		return nil, fmt.Errorf("failed to parse lock file YAML: %w", err)
	}

	// Regular expression to match uses: owner/repo@sha with optional version comment
	// This matches: owner/repo@40-char-hex-sha # version
	// Captures: (1) repo, (2) sha, (3) version (optional)
	usesPattern := regexp.MustCompile(`([a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(?:/[a-zA-Z0-9_.-]+)*)@([0-9a-f]{40})(?:\s*#\s*([^\s]+))?`)

	actions := make(map[string]ActionUsage) // Use map to deduplicate

	// Convert to string and extract all uses fields
	contentStr := string(content)
	matches := usesPattern.FindAllStringSubmatch(contentStr, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			repo := match[1]
			sha := match[2]

			// Skip if we've already seen this action
			if _, exists := actions[repo+"@"+sha]; exists {
				continue
			}

			// Extract version from comment if present (match[3])
			version := ""
			if len(match) >= 4 && match[3] != "" {
				version = match[3]
				actionSHACheckerLog.Printf("Found action: %s@%s (version: %s)", repo, sha, version)
			} else {
				// Fallback: try to determine the version tag from action_pins.json
				if pin, found := GetActionPinByRepo(repo); found {
					version = pin.Version
					actionSHACheckerLog.Printf("Found action: %s@%s (version from pins: %s)", repo, sha, version)
				} else {
					actionSHACheckerLog.Printf("Found action: %s@%s (no version)", repo, sha)
				}
			}

			actions[repo+"@"+sha] = ActionUsage{
				Repo:    repo,
				SHA:     sha,
				Version: version,
			}
		}
	}

	// Convert map to slice
	result := make([]ActionUsage, 0, len(actions))
	for _, action := range actions {
		result = append(result, action)
	}

	actionSHACheckerLog.Printf("Extracted %d unique actions", len(result))
	return result, nil
}

// CheckActionSHAUpdates checks if actions need updating by comparing with latest SHAs
func CheckActionSHAUpdates(actions []ActionUsage, resolver *ActionResolver) []ActionUpdateCheck {
	actionSHACheckerLog.Printf("Checking %d actions for updates", len(actions))

	results := make([]ActionUpdateCheck, 0, len(actions))

	for _, action := range actions {
		check := ActionUpdateCheck{
			Action:      action,
			NeedsUpdate: false,
		}

		// Skip if we don't have a version to check against
		if action.Version == "" {
			actionSHACheckerLog.Printf("Skipping %s: no version tag available", action.Repo)
			continue
		}

		// Resolve the latest SHA for this version
		latestSHA, err := resolver.ResolveSHA(action.Repo, action.Version)
		if err != nil {
			actionSHACheckerLog.Printf("Failed to resolve %s@%s: %v", action.Repo, action.Version, err)
			check.Message = fmt.Sprintf("Unable to check for updates: %v", err)
			results = append(results, check)
			continue
		}

		check.LatestSHA = latestSHA

		// Compare SHAs
		if action.SHA != latestSHA {
			check.NeedsUpdate = true
			check.Message = fmt.Sprintf("Action %s@%s is using SHA %s but latest is %s",
				action.Repo, action.Version, action.SHA[:7], latestSHA[:7])
			actionSHACheckerLog.Printf("UPDATE NEEDED: %s", check.Message)
		} else {
			actionSHACheckerLog.Printf("Action %s@%s is up to date", action.Repo, action.Version)
		}

		results = append(results, check)
	}

	return results
}

// ValidateActionSHAsInLockFile validates action SHAs in a lock file and emits warnings
func ValidateActionSHAsInLockFile(lockFilePath string, cache *ActionCache, verbose bool) error {
	actionSHACheckerLog.Printf("Validating action SHAs in: %s", lockFilePath)

	// Extract actions from lock file
	actions, err := ExtractActionsFromLockFile(lockFilePath)
	if err != nil {
		return fmt.Errorf("failed to extract actions: %w", err)
	}

	if len(actions) == 0 {
		actionSHACheckerLog.Print("No pinned actions found in lock file")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No pinned actions to validate"))
		}
		return nil
	}

	// Create resolver for checking latest SHAs
	resolver := NewActionResolver(cache)

	// Check for updates
	checks := CheckActionSHAUpdates(actions, resolver)

	// Count and report updates
	updateCount := 0
	for _, check := range checks {
		if check.NeedsUpdate {
			updateCount++
			// Emit warning (FormatWarningMessage adds the warning emoji)
			warningMsg := fmt.Sprintf("%s@%s has a newer SHA available: %s → %s",
				check.Action.Repo,
				check.Action.Version,
				check.Action.SHA[:7],
				check.LatestSHA[:7])
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))

			// Show full SHA in verbose mode
			if verbose {
				fmt.Fprintf(os.Stderr, "    Current: %s\n", check.Action.SHA)
				fmt.Fprintf(os.Stderr, "    Latest:  %s\n", check.LatestSHA)
			}
		}
	}

	if updateCount > 0 {
		actionSHACheckerLog.Printf("Found %d actions that need updating", updateCount)
		// Save the cache with updated SHAs so the next compilation will use them
		if err := cache.Save(); err != nil {
			actionSHACheckerLog.Printf("Warning: failed to save action cache: %v", err)
		} else {
			actionSHACheckerLog.Print("Saved updated action cache")
		}
		// Provide suggestion to fix the issue
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To apply updated action SHAs, recompile with: gh aw compile"))
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d action(s) with available updates", updateCount)))
		}
	} else {
		actionSHACheckerLog.Print("All actions are up to date")
	}

	return nil
}
