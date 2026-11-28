package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/codemod"
)

var modelAutoCodemodLog = logger.New("cli:codemod_model_auto")

// getModelAutoToRouterCodemod rewrites deprecated main-agent `model: auto` and
// `model: copilot/auto` to `model: router` wherever the compiler would route
// them (sharing the compiler's full two-stage eligibility and validation check).
// When rewriting `copilot/auto` on Claude or Codex without `engine.model-provider`,
// it also adds `engine.model-provider: github` so the inference provider does not
// change. Workflows where routing does not apply are left unchanged with guidance.
func getModelAutoToRouterCodemod() Codemod {
	return Codemod{
		ID:           "model-auto-to-router",
		Name:         "Migrate deprecated model: auto to model: router",
		Description:  "Rewrites eligible main-agent 'model: auto' and 'model: copilot/auto' values to 'model: router' (adding 'engine.model-provider: github' for Claude/Codex when needed), and leaves ineligible workflows unchanged with guidance.",
		IntroducedIn: "1.0.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			return applyModelAutoToRouter(content, frontmatter, "")
		},
		ApplyWithContext: func(content string, frontmatter map[string]any, filePath string) (string, bool, error) {
			return applyModelAutoToRouter(content, frontmatter, filePath)
		},
	}
}

func applyModelAutoToRouter(content string, frontmatter map[string]any, filePath string) (string, bool, error) {
	modelPath, authoredModel, ok := workflow.AuthoredAutoModelPath(frontmatter)
	if !ok {
		return content, false, nil
	}

	outcome, err := workflow.InspectAutoModelRouting(content, filePath)
	if err != nil {
		modelAutoCodemodLog.Printf("Skipping model: %s rewrite for %s: inspection failed: %v", authoredModel, filePath, err)
		return content, false, nil
	}
	if outcome == nil {
		return content, false, nil
	}

	if outcome.AlreadyConfiguredRouting {
		warnModelAutoUnchanged(filePath, fmt.Sprintf(
			"left model: %s unchanged because model routing is already configured and overrides it. Remove model: %s (or migrate engine.model-routing to model: router while preserving your configured mode and shortlist). See https://github.github.com/gh-aw/reference/model-routing/",
			outcome.AuthoredModel, outcome.AuthoredModel))
		return content, false, nil
	}
	if !outcome.Routed {
		warnModelAutoUnchanged(filePath, fmt.Sprintf(
			"left model: %s unchanged because %s. To migrate, remove the blocker and switch to model: router, or pin a concrete model. See https://github.github.com/gh-aw/reference/model-routing/",
			outcome.AuthoredModel, outcome.Reason))
		return content, false, nil
	}
	if outcome.AddedGitHubProvider && !workflow.CanPinEngineGitHubProviderLocally(frontmatter) {
		warnModelAutoUnchanged(filePath, fmt.Sprintf(
			"left model: %s unchanged because engine %q is inherited from an import and adding engine.model-provider: github locally would conflict with or invalidate the engine config. Set engine.model-provider: github alongside the imported engine (or define engine locally) and switch to model: router. See https://github.github.com/gh-aw/reference/model-routing/",
			outcome.AuthoredModel, outcome.EngineID))
		return content, false, nil
	}

	newContent, applied, err := codemod.Apply(content, func(f *codemod.Frontmatter) error {
		if err := f.Set(modelPath, "router"); err != nil {
			return err
		}
		if outcome.AddedGitHubProvider {
			return setEngineGitHubModelProvider(f)
		}
		return nil
	})
	if err != nil || !applied {
		return content, false, err
	}

	// Verify the rewritten workflow still parses cleanly before accepting the edit.
	if _, err := workflow.InspectAutoModelRouting(newContent, filePath); err != nil {
		modelAutoCodemodLog.Printf("Skipping model: %s rewrite for %s: rewritten workflow failed validation: %v", authoredModel, filePath, err)
		warnModelAutoUnchanged(filePath, fmt.Sprintf(
			"left model: %s unchanged because rewriting it automatically produced an invalid workflow (%v). Migrate to model: router manually. See https://github.github.com/gh-aw/reference/model-routing/",
			outcome.AuthoredModel, err))
		return content, false, nil
	}

	modelAutoCodemodLog.Printf("Rewrote %s=%q to router (addedGitHubProvider=%v)", strings.Join(modelPath, "."), authoredModel, outcome.AddedGitHubProvider)
	return newContent, true, nil
}

func warnModelAutoUnchanged(filePath, message string) {
	prefix := ""
	if filePath != "" {
		prefix = filepath.Base(filePath) + ": "
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(prefix+message))
}

// setEngineGitHubModelProvider ensures `engine.model-provider: github` is set,
// expanding string-form `engine: claude` / `engine: codex` to map form when needed.
func setEngineGitHubModelProvider(f *codemod.Frontmatter) error {
	engineVal, found := f.Lookup("engine")
	if !found {
		return errors.New("cannot add engine.model-provider without a local engine declaration")
	}
	if engineStr, ok := engineVal.(string); ok {
		return f.Set([]string{"engine"}, map[string]any{
			"id":             engineStr,
			"model-provider": "github",
		})
	}
	return f.Set([]string{"engine", "model-provider"}, "github")
}
