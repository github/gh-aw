// This file provides Git repository utilities for workflow compilation.
//
// This file contains helper functions for interacting with Git repositories
// to extract metadata such as tags and version information. These helpers are
// used during workflow compilation to determine release contexts and versioning.
//
// # Organization Rationale
//
// These Git utilities are grouped in a helper file because they:
//   - Provide Git-specific functionality (tags, versions)
//   - Are used by multiple workflow compilation modules
//   - Encapsulate Git command execution and error handling
//   - Have a clear domain focus (Git repository metadata)
//
// This follows the helper file conventions documented in the developer instructions.
// See skills/developer/SKILL.md#helper-file-conventions for details.
//
// # Key Functions
//
// Tag Detection:
//   - GetCurrentGitTag() - Detect current Git tag from environment or repository
//
// # Usage Patterns
//
// These functions are primarily used during workflow compilation to:
//   - Detect release contexts (tags vs. regular commits)
//   - Extract version information for releases
//   - Support conditional workflow behavior based on Git state

package workflow

import (
	"os"
	"os/exec"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var gitHelpersLog = logger.New("workflow:git_helpers")

// findGitRoot attempts to find the git repository root directory.
// Returns empty string if not in a git repository or if git command fails.
// This function is safe to call from any context and won't cause errors if git is not available.
func findGitRoot() string {
	gitHelpersLog.Print("Attempting to find git root directory")
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		gitHelpersLog.Printf("Could not find git root (not a git repo or git not available): %v", err)
		return ""
	}
	gitRoot := strings.TrimSpace(string(output))
	gitHelpersLog.Printf("Found git root: %s", gitRoot)
	return gitRoot
}

// GetCurrentGitTag returns the current git tag if available.
// Returns empty string if not on a tag.
func GetCurrentGitTag() string {
	// Try GITHUB_REF for tags (refs/tags/v1.0.0)
	if ref := os.Getenv("GITHUB_REF"); strings.HasPrefix(ref, "refs/tags/") {
		tag := strings.TrimPrefix(ref, "refs/tags/")
		gitHelpersLog.Printf("Using tag from GITHUB_REF: %s", tag)
		return tag
	}

	// Try git describe --exact-match for local tag
	cmd := exec.Command("git", "describe", "--exact-match", "--tags", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		// Not on a tag, which is fine
		gitHelpersLog.Print("Not on a git tag")
		return ""
	}

	tag := strings.TrimSpace(string(output))
	gitHelpersLog.Printf("Using tag from git describe: %s", tag)
	return tag
}
