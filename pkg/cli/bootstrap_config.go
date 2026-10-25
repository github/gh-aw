package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
)

// printBootstrapConfigTODO prints a TODO checklist of manual steps required by the
// config entries in the package manifest. Called by the non-interactive
// "add" command after workflows have been installed.
func printBootstrapConfigTODO(w io.Writer, profile *resolvedBootstrapProfile) {
	if profile == nil || profile.Profile == nil || len(profile.Profile.Config) == 0 {
		return
	}

	bootstrapLog.Printf("Printing bootstrap config TODO: package=%s, actions=%d", profile.PackageID, len(profile.Profile.Config))
	if _, err := fmt.Fprintln(w, ""); err != nil {
		bootstrapLog.Printf("Failed to write bootstrap checklist separator: %v", err)
		return
	}
	if _, err := fmt.Fprintln(w, console.FormatInfoMessage("Post-installation steps from "+profile.PackageID+":")); err != nil {
		bootstrapLog.Printf("Failed to write bootstrap checklist heading: %v", err)
		return
	}

	for _, action := range profile.Profile.Config {
		switch action.Type {
		case "require-owner-type":
			if _, err := fmt.Fprintf(w, "  ☐ Verify repository owner type: %s\n", action.Value); err != nil {
				bootstrapLog.Printf("Failed to write bootstrap owner type step: %v", err)
				return
			}
		case "repo-variable":
			line := "  ☐ Set repository variable: " + action.Name
			if action.Prompt != "" {
				line += " — " + action.Prompt
			}
			if action.Optional {
				line += " (optional)"
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				bootstrapLog.Printf("Failed to write bootstrap repository variable step: %v", err)
				return
			}
		case "repo-secret":
			line := "  ☐ Set repository secret: " + action.Name
			if action.Prompt != "" {
				line += " — " + action.Prompt
			}
			if action.Optional {
				line += " (optional)"
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				bootstrapLog.Printf("Failed to write bootstrap repository secret step: %v", err)
				return
			}
		case "repo-label":
			if _, err := fmt.Fprintf(w, "  ☐ Create or update repository label: %s (%s)\n", action.Name, action.Color); err != nil {
				bootstrapLog.Printf("Failed to write bootstrap label step: %v", err)
				return
			}
		case "github-app":
			appLabel := action.AppName
			if appLabel == "" {
				appLabel = "GitHub App"
			}
			if _, err := fmt.Fprintf(w, "  ☐ Configure %s (variable: %s, secret: %s)\n",
				appLabel, action.AppIDVariable, action.PrivateKeySecret); err != nil {
				bootstrapLog.Printf("Failed to write bootstrap GitHub App step: %v", err)
				return
			}
		case "copilot-auth":
			secret := action.Secret
			if secret == "" {
				secret = "COPILOT_GITHUB_TOKEN"
			}
			if _, err := fmt.Fprintf(w, "  ☐ Set Copilot PAT secret: %s\n", secret); err != nil {
				bootstrapLog.Printf("Failed to write bootstrap Copilot authentication step: %v", err)
				return
			}
		case "commit-and-push":
			if _, err := fmt.Fprintf(w, "  ☐ Commit and push local changes — %s\n", action.Message); err != nil {
				bootstrapLog.Printf("Failed to write bootstrap commit step: %v", err)
				return
			}
		case "handoff":
			if _, err := fmt.Fprintln(w, console.FormatInfoMessage(action.Message)); err != nil {
				bootstrapLog.Printf("Failed to write bootstrap handoff step: %v", err)
				return
			}
		}
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		bootstrapLog.Printf("Failed to write bootstrap checklist separator: %v", err)
	}
}

// executeBootstrapConfigForAdd runs the bootstrap config actions interactively.
// Used by add-wizard after the workflow PR has been created and merged.
func executeBootstrapConfigForAdd(ctx context.Context, repo string, sources []string, profile *resolvedBootstrapProfile, useCopilotRequests bool, verbose bool, disableGitHubAppPermissionInference bool) error {
	if profile == nil || profile.Profile == nil || len(profile.Profile.Config) == 0 {
		return nil
	}

	if repo == "" {
		return errors.New("--repo OWNER/REPO is required to apply bootstrap config steps interactively")
	}

	bootstrapLog.Printf("Applying bootstrap config for add: repo=%s, package=%s, actions=%d, useCopilotRequests=%t", repo, profile.PackageID, len(profile.Profile.Config), useCopilotRequests)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Applying setup steps from "+profile.PackageID+"..."))
	repoDir, err := gitutil.FindGitRoot()
	if err != nil {
		bootstrapLog.Printf("Could not determine git root for add bootstrap config: %v", err)
	}

	return executeBootstrapProfile(ctx, bootstrapProfileRunConfig{
		Repo:                                repo,
		RepoDir:                             repoDir,
		Sources:                             sources,
		Profile:                             profile,
		UseCopilotRequests:                  useCopilotRequests,
		Verbose:                             verbose,
		DisableGitHubAppPermissionInference: disableGitHubAppPermissionInference,
	})
}
