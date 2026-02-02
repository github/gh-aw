package cli

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	"github.com/cli/go-gh/v2"
	"github.com/github/gh-aw/pkg/logger"
)

var execLog = logger.New("cli:exec")

// ghExecOrFallback executes a gh CLI command if GH_TOKEN is available,
// otherwise falls back to an alternative command.
// The gh CLI arguments are inferred from the fallback command arguments.
// Returns the stdout, stderr, and error from whichever command was executed.
func ghExecOrFallback(fallbackCmd string, fallbackArgs []string, fallbackEnv []string) (string, string, error) {
	ghToken := os.Getenv("GH_TOKEN")

	if ghToken != "" {
		// Use gh CLI when GH_TOKEN is available
		// Infer gh args from fallback args
		ghArgs := inferGhArgs(fallbackCmd, fallbackArgs)
		execLog.Printf("Using gh CLI: gh %s", strings.Join(ghArgs, " "))
		stdout, stderr, err := gh.Exec(ghArgs...)
		return stdout.String(), stderr.String(), err
	}

	// Fall back to alternative command when GH_TOKEN is not available
	execLog.Printf("Using fallback command: %s %s", fallbackCmd, strings.Join(fallbackArgs, " "))
	cmd := exec.Command(fallbackCmd, fallbackArgs...)

	// Add custom environment variables if provided
	if len(fallbackEnv) > 0 {
		cmd.Env = append(os.Environ(), fallbackEnv...)
	}

	// Capture stdout and stderr separately like gh.Exec
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// inferGhArgs infers gh CLI arguments from fallback command arguments
func inferGhArgs(fallbackCmd string, fallbackArgs []string) []string {
	if fallbackCmd != "git" || len(fallbackArgs) == 0 {
		// For non-git commands, use gh exec
		return append([]string{"exec", "--", fallbackCmd}, fallbackArgs...)
	}

	// Handle git commands
	gitCmd := fallbackArgs[0]

	switch gitCmd {
	case "clone":
		// git clone [options] <repo> <dir>
		// -> gh repo clone <repo> <dir> [options]
		return buildGhCloneArgs(fallbackArgs[1:])
	default:
		// For other git commands, use gh exec
		return append([]string{"exec", "--", "git"}, fallbackArgs...)
	}
}

// buildGhCloneArgs builds gh repo clone arguments from git clone arguments
func buildGhCloneArgs(gitArgs []string) []string {
	ghArgs := []string{"repo", "clone"}

	var repoURL string
	var targetDir string
	var otherArgs []string

	// Options that take a value
	optsWithValue := map[string]bool{
		"--branch":            true,
		"--depth":             true,
		"--origin":            true,
		"--template":          true,
		"--config":            true,
		"--server-option":     true,
		"--upload-pack":       true,
		"--reference":         true,
		"--reference-if-able": true,
		"--separate-git-dir":  true,
	}

	// Parse git clone arguments
	for i := 0; i < len(gitArgs); i++ {
		arg := gitArgs[i]
		if strings.HasPrefix(arg, "https://") || strings.HasPrefix(arg, "git@") {
			repoURL = arg
		} else if strings.HasPrefix(arg, "-") {
			// It's an option
			otherArgs = append(otherArgs, arg)
			// Check if this option takes a value
			if optsWithValue[arg] && i+1 < len(gitArgs) {
				i++ // Move to next arg
				otherArgs = append(otherArgs, gitArgs[i])
			}
		} else if repoURL != "" && targetDir == "" {
			// This is the target directory
			targetDir = arg
		}
	}

	// Extract repo slug from URL (remove https://github.com/ or enterprise domain)
	repoSlug := extractRepoSlug(repoURL)

	// Build gh args: gh repo clone <slug> <dir> -- [git options]
	ghArgs = append(ghArgs, repoSlug)
	if targetDir != "" {
		ghArgs = append(ghArgs, targetDir)
	}

	if len(otherArgs) > 0 {
		ghArgs = append(ghArgs, "--")
		ghArgs = append(ghArgs, otherArgs...)
	}

	return ghArgs
}

// extractRepoSlug extracts the owner/repo slug from a GitHub URL
func extractRepoSlug(repoURL string) string {
	githubHost := getGitHubHost()

	// Remove the GitHub host from the URL
	slug := strings.TrimPrefix(repoURL, githubHost+"/")

	// Remove .git suffix if present
	slug = strings.TrimSuffix(slug, ".git")

	return slug
}
