// This file normalizes a statically authored main-agent `model: auto` or
// `model: copilot/auto` into `model: router` wherever the workflow is eligible
// for implicit routing on AWF v0.29.21+ (router fallback enabled).
//
// Workflows where routing does not apply or stage-2 validation rolls back keep
// their existing `auto` behavior cleanly without failing compilation. Both
// outcomes emit a deprecation warning during compilation, and `gh aw fix`
// shares the exact same two-stage decision via InspectAutoModelRouting.

package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var modelRoutingAutoLog = logger.New("workflow:model_routing_auto")

// AutoModelRoutingOutcome records how the compiler handled an authored
// main-agent `model: auto` or `model: copilot/auto` value.
type AutoModelRoutingOutcome struct {
	// AuthoredModel is "auto" or "copilot/auto".
	AuthoredModel string
	// EngineID is the resolved engine ID (e.g. "copilot", "claude", "codex", "pi").
	EngineID string
	// FromImport is true when the effective `auto` / `copilot/auto` model came
	// from an imported file rather than the main workflow's own frontmatter.
	FromImport bool
	// Routed is true when the workflow passed both eligibility stages and was
	// normalized to model: router.
	Routed bool
	// AddedGitHubProvider is true when copilot/auto on claude or codex inferred
	// engine.model-provider: github so routing stays on GitHub Copilot.
	AddedGitHubProvider bool
	// Reason explains why routing did not apply when Routed is false.
	Reason string
}

// AuthoredAutoModelPath returns the YAML mapping path and value when frontmatter
// statically authors `auto` or `copilot/auto` as its effective main-agent model
// (following engine.pi.model > engine.model > top-level model precedence).
func AuthoredAutoModelPath(frontmatter map[string]any) ([]string, string, bool) {
	if engineObj, ok := frontmatter["engine"].(map[string]any); ok {
		if piObj, ok := engineObj["pi"].(map[string]any); ok {
			if raw, exists := piObj["model"]; exists {
				if model, ok := raw.(string); ok && strings.TrimSpace(model) != "" {
					if m, isAuto := isAuthoredAutoModelValue(model); isAuto {
						return []string{"engine", "pi", "model"}, m, true
					}
					return nil, "", false
				}
			}
		}
		if raw, exists := engineObj["model"]; exists {
			if model, ok := raw.(string); ok && strings.TrimSpace(model) != "" {
				if m, isAuto := isAuthoredAutoModelValue(model); isAuto {
					return []string{"engine", "model"}, m, true
				}
				return nil, "", false
			}
		}
	}
	if raw, exists := frontmatter["model"]; exists {
		if model, ok := raw.(string); ok {
			if m, isAuto := isAuthoredAutoModelValue(model); isAuto {
				return []string{"model"}, m, true
			}
		}
	}
	return nil, "", false
}

// isAuthoredAutoModelValue reports whether model is an exact bare `auto` or
// `copilot/auto` value (no query parameters such as `?effort=high`, no
// GitHub Actions expressions).
func isAuthoredAutoModelValue(model string) (string, bool) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "auto" || trimmed == "copilot/auto" {
		return trimmed, true
	}
	return "", false
}

// workflowDefinesAutoAlias reports whether the main workflow or any of its
// imports declares its own `auto` alias under `models:`.
func workflowDefinesAutoAlias(frontmatter map[string]any, importsResult *parser.ImportsResult) bool {
	if _, ok := extractMainModelAliases(frontmatter)["auto"]; ok {
		return true
	}
	if importsResult != nil {
		for _, imported := range importsResult.MergedModels {
			if _, ok := imported["auto"]; ok {
				return true
			}
		}
	}
	return false
}

// applyAutoModelRouting normalizes a statically authored main-agent `model: auto`
// or `model: copilot/auto` to `model: router` using the same two-stage
// eligibility and validation-rollback contract as implicit default routing.
func (c *Compiler) applyAutoModelRouting(ctx *workflowBuildContext) {
	if ctx == nil || ctx.workflowData == nil || ctx.engineSetup == nil {
		return
	}
	data := ctx.workflowData
	authored, ok := isAuthoredAutoModelValue(data.Model)
	if !ok {
		return
	}
	if workflowDefinesAutoAlias(ctx.frontmatter.Frontmatter, ctx.engineSetup.importsResult) {
		modelRoutingAutoLog.Printf("Preserving custom models.auto alias in %s", filepath.Base(ctx.cleanPath))
		return
	}
	_, _, authoredLocally := AuthoredAutoModelPath(ctx.frontmatter.Frontmatter)
	fromImport := !authoredLocally
	if data.EngineConfig == nil {
		data.AutoModelRouting = keepAutoModelOutcome(authored, "", fromImport, "no engine configuration")
		return
	}
	data.AutoModelRouting = c.resolveAutoModelRouting(data, authored, fromImport, filepath.Base(ctx.cleanPath))
}

func (c *Compiler) resolveAutoModelRouting(data *WorkflowData, authored string, fromImport bool, workflowFile string) *AutoModelRoutingOutcome {
	prevModel := data.Model
	prevProvider := data.EngineConfig.LLMProvider
	addedGitHubProvider := false

	// On engines whose default provider is not GitHub Copilot (claude, codex),
	// `copilot/auto` without an explicit `engine.model-provider` selects GitHub
	// Copilot inference. Preserve that provider for routing, without overriding
	// an explicit non-GitHub provider.
	if authored == "copilot/auto" && prevProvider == "" && modelRoutingProvider(data.EngineConfig.ID, data) != LLMProviderGitHub {
		data.EngineConfig.LLMProvider = LLMProviderGitHub
		addedGitHubProvider = true
	}

	// Stage 1: structural eligibility and AWF fallback version check while
	// EngineConfig.ModelRouting is still nil.
	if reason := autoModelRoutingStage1Ineligibility(data, authored); reason != "" {
		data.EngineConfig.LLMProvider = prevProvider
		modelRoutingAutoLog.Printf("Model %q kept existing auto behavior in %s: %s", authored, workflowFile, reason)
		return keepAutoModelOutcome(authored, data.EngineConfig.ID, fromImport, reason)
	}

	// Stage 2: tentatively enable routing as `model: router` and validate.
	data.Model = ""
	data.EngineConfig.ModelRouting = &CopilotModelRoutingConfig{
		Goal:      routerModelGoal,
		Mode:      defaultModelRoutingMode,
		requested: "router",
	}
	if reason := defaultModelRoutingValidationFailure(c, data); reason != "" {
		data.Model = prevModel
		data.EngineConfig.ModelRouting = nil
		data.EngineConfig.LLMProvider = prevProvider
		modelRoutingAutoLog.Printf("Model %q rolled back to existing auto behavior in %s: %s", authored, workflowFile, reason)
		return keepAutoModelOutcome(authored, data.EngineConfig.ID, fromImport, reason)
	}

	modelRoutingAutoLog.Printf("Normalized model %q to model: router in %s for engine %s (addedGitHubProvider=%v, fromImport=%v)",
		authored, workflowFile, data.EngineConfig.ID, addedGitHubProvider, fromImport)
	return &AutoModelRoutingOutcome{
		AuthoredModel:       authored,
		EngineID:            data.EngineConfig.ID,
		FromImport:          fromImport,
		Routed:              true,
		AddedGitHubProvider: addedGitHubProvider,
	}
}

func keepAutoModelOutcome(authored, engineID string, fromImport bool, reason string) *AutoModelRoutingOutcome {
	return &AutoModelRoutingOutcome{
		AuthoredModel: authored,
		EngineID:      engineID,
		FromImport:    fromImport,
		Routed:        false,
		Reason:        reason,
	}
}

func autoModelRoutingStage1Ineligibility(data *WorkflowData, authored string) string {
	if reason := modelRoutingRuntimeIneligibility(data); reason != "" {
		return reason
	}
	if !apiProxySupportsRoutingFallback(data) {
		return fmt.Sprintf("AWF version does not support router fallback (requires %s or newer)", constants.AWFModelRoutingFallbackMinVersion)
	}
	return autoModelThreatDetectionProviderChange(data, authored)
}

// autoModelThreatDetectionProviderChange reports when clearing an authored
// `copilot/auto` model would switch an explicit Claude or Codex threat-detection
// engine from GitHub Copilot to its native provider.
func autoModelThreatDetectionProviderChange(data *WorkflowData, authored string) string {
	if authored != "copilot/auto" || !IsDetectionJobEnabled(data.SafeOutputs) {
		return ""
	}
	td := data.SafeOutputs.ThreatDetection
	if td == nil || td.Model != "" || td.EngineConfig == nil || td.EngineConfig.LLMProvider != "" {
		return ""
	}
	tdEngineID := strings.ToLower(td.EngineConfig.ID)
	if tdEngineID != string(constants.ClaudeEngine) && tdEngineID != string(constants.CodexEngine) {
		return ""
	}
	return fmt.Sprintf("safe-outputs.threat-detection.engine %q relies on the copilot/ prefix to select GitHub Copilot; set safe-outputs.threat-detection.engine.model-provider: github (or a detection model) first", tdEngineID)
}

// reportAutoModelRouting emits the deprecation warning for an authored
// main-agent `model: auto` or `model: copilot/auto` after routing eligibility
// and rollback have settled. It runs only from the compilation path so
// parse-only callers stay quiet, and remains a warning even in strict mode.
func (c *Compiler) reportAutoModelRouting(data *WorkflowData, markdownPath string) {
	if data == nil || data.AutoModelRouting == nil {
		return
	}
	message := FormatAutoModelRoutingWarning(data.AutoModelRouting)
	c.diagnostics = append(c.diagnostics, CompilerDiagnostic{
		Path:    markdownPath,
		Level:   "warning",
		Message: message,
	})
	c.IncrementWarningCount()
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		formatCompilerMessage(markdownPath, "warning", message)))
}

// FormatAutoModelRoutingWarning formats the compiler deprecation warning for an
// authored main-agent `model: auto` or `model: copilot/auto` value.
func FormatAutoModelRoutingWarning(outcome *AutoModelRoutingOutcome) string {
	if outcome == nil {
		return ""
	}
	if outcome.FromImport {
		if outcome.Routed {
			if outcome.AddedGitHubProvider {
				return fmt.Sprintf("imported model: %s is deprecated and now routes as model: router on GitHub Copilot. "+
					"Switch to model: router and set engine.model-provider: github on the engine declaration. "+
					"See https://github.github.com/gh-aw/reference/model-routing/", outcome.AuthoredModel)
			}
			return fmt.Sprintf("imported model: %s is deprecated and now routes as model: router. "+
				"Update the imported workflow to use model: router. "+
				"See https://github.github.com/gh-aw/reference/model-routing/", outcome.AuthoredModel)
		}
		return fmt.Sprintf("imported model: %s is deprecated; kept existing auto behavior because %s. "+
			"Update the imported workflow to remove the blocker and switch to model: router, or pin a concrete model. "+
			"See https://github.github.com/gh-aw/reference/model-routing/", outcome.AuthoredModel, outcome.Reason)
	}
	if outcome.Routed {
		if outcome.AddedGitHubProvider {
			return fmt.Sprintf("model: %s is deprecated and now routes as model: router on GitHub Copilot. "+
				"Run 'gh aw fix --write' to rewrite it to model: router with engine.model-provider: github. "+
				"See https://github.github.com/gh-aw/reference/model-routing/", outcome.AuthoredModel)
		}
		return fmt.Sprintf("model: %s is deprecated and now routes as model: router. "+
			"Run 'gh aw fix --write' to rewrite it to model: router. "+
			"See https://github.github.com/gh-aw/reference/model-routing/", outcome.AuthoredModel)
	}
	return fmt.Sprintf("model: %s is deprecated; kept existing auto behavior because %s. "+
		"To migrate, remove the blocker and switch to model: router, or pin a concrete model. "+
		"See https://github.github.com/gh-aw/reference/model-routing/", outcome.AuthoredModel, outcome.Reason)
}

// InspectAutoModelRouting runs the compiler's full parse and two-stage
// model: auto eligibility pipeline on content (resolving imports relative to
// filePath when non-empty) so `gh aw fix` shares the exact same decision as the
// compiler. Shared/redirect-only workflows and workflows without an eligible
// deprecation target return (nil, nil).
func InspectAutoModelRouting(content, filePath string) (*AutoModelRoutingOutcome, error) {
	virtualPath := filePath
	if virtualPath == "" {
		virtualPath = "workflow.md"
	}
	compiler := NewCompiler()
	compiler.SetQuiet(true)
	compiler.SetNoEmit(true)
	compiler.SetSkipValidation(true)

	data, err := compiler.ParseWorkflowString(content, virtualPath)
	if err != nil {
		var sharedErr *SharedWorkflowError
		var redirectErr *RedirectOnlyWorkflowError
		if errors.As(err, &sharedErr) || errors.As(err, &redirectErr) {
			return nil, nil
		}
		return nil, err
	}
	return data.AutoModelRouting, nil
}
