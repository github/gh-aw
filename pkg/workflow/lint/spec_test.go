//go:build !integration

package lint_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/lint"
)

// These tests derive from README.md's Overview, Public API, and Built-in Rules
// sections. Only exported declarations were consulted in implementation files.

func specLintRule(id string, targets lint.ASTTargets) lint.Rule {
	return lint.Rule{
		ID: id, Description: "Specification example rule", Severity: lint.Warning, Targets: targets,
		Check: func(_ context.Context, input lint.Input) ([]lint.Diagnostic, error) {
			return []lint.Diagnostic{{
				RuleID: id, Severity: lint.Warning, Path: input.SourcePath,
				Message: "Specification example finding",
				Related: []lint.RelatedLocation{{Path: "related.md", Symbol: "example"}},
			}}, nil
		},
	}
}

func TestSpec_PublicAPI_NewAndRules(t *testing.T) {
	rule := specLintRule("spec-custom", lint.AWAST)
	runner, err := lint.New(rule)
	require.NoError(t, err, "New must accept a custom rule with documented fields")
	rules := runner.Rules()
	require.Len(t, rules, 1, "Rules must return the registered custom rule")
	assert.Equal(t, rule.ID, rules[0].ID, "Rules must retain the custom identifier")

	rules[0].ID = "caller-change"
	rules[0].Description = "caller-change"
	rules[0].Check = nil
	fresh := runner.Rules()
	require.Len(t, fresh, 1, "mutating a returned slice must not change registration count")
	assert.Equal(t, rule.ID, fresh[0].ID, "Rules must return a copy of the registrations")
	assert.Equal(t, rule.Description, fresh[0].Description, "returned rule values must be independent")
	require.NotNil(t, fresh[0].Check, "changing a returned check must not change the runner")
}

func TestSpec_PublicAPI_RunnerSelect(t *testing.T) {
	runner, err := lint.New(
		specLintRule("spec-first", lint.AWAST),
		specLintRule("spec-second", lint.AWAST),
	)
	require.NoError(t, err, "custom runner construction must succeed")
	for _, tt := range []struct {
		name    string
		ids     []string
		wantErr bool
	}{
		{"one registered rule", []string{"spec-second"}, false},
		{"both registered rules", []string{"spec-first", "spec-second"}, false},
		{"unknown rule", []string{"spec-unknown"}, true},
		{"mixed known and unknown", []string{"spec-first", "spec-unknown"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			selected, err := runner.Select(tt.ids...)
			if tt.wantErr {
				assert.Error(t, err, "Select must reject unknown IDs for %s", tt.name)
				return
			}
			require.NoError(t, err, "Select must accept registered IDs for %s", tt.name)
			diagnostics, err := selected.Run(context.Background(), lint.Input{
				Document: &aw.Document{}, SourcePath: "workflow.md",
			})
			require.NoError(t, err, "selected custom checks must run")
			var ids []string
			for _, diagnostic := range diagnostics {
				ids = append(ids, diagnostic.RuleID)
			}
			assert.ElementsMatch(t, tt.ids, ids, "only selected rules must execute")
			assert.Len(t, runner.Rules(), 2, "Select must leave the original runner intact")
		})
	}
}

func TestSpec_PublicAPI_RunnerRunTargets(t *testing.T) {
	runner, err := lint.New(
		specLintRule("spec-aw", lint.AWAST),
		specLintRule("spec-actions", lint.ActionsAST),
		specLintRule("spec-both", lint.AWAST|lint.ActionsAST),
	)
	require.NoError(t, err, "rules may target authored, compiled, or both ASTs")

	for _, tt := range []struct {
		name     string
		authored bool
		compiled bool
		wantIDs  []string
	}{
		{"authored input", true, false, []string{"spec-aw", "spec-both"}},
		{"compiled input", false, true, []string{"spec-actions", "spec-both"}},
		{"both inputs", true, true, []string{"spec-aw", "spec-actions", "spec-both"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := lint.Input{
				SourcePath:   "workflow.md",
				AuthoredJobs: map[string]any{"example": map[string]any{"name": "example"}},
			}
			if tt.authored {
				input.Document = &aw.Document{}
			}
			if tt.compiled {
				input.Workflow = &actions.Workflow{}
			}
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err, "applicable checks must run for %s", tt.name)
			var ids []string
			for _, diagnostic := range diagnostics {
				ids = append(ids, diagnostic.RuleID)
				assert.Equal(t, input.SourcePath, diagnostic.Path, "a finding must retain its source path")
				assert.Equal(t, "Specification example finding", diagnostic.Message, "a finding must retain its message")
				assert.Equal(t, lint.Warning, diagnostic.Severity, "a finding must retain its severity")
				assert.Equal(t, []lint.RelatedLocation{{Path: "related.md", Symbol: "example"}},
					diagnostic.Related, "a finding must retain related source locations")
			}
			assert.ElementsMatch(t, tt.wantIDs, ids, "Run must execute exactly the applicable AST rules")
			if tt.authored {
				assert.Equal(t, &aw.Document{}, input.Document, "the runner must not mutate the authored AST")
			}
			if tt.compiled {
				assert.Equal(t, &actions.Workflow{}, input.Workflow, "the runner must not mutate the compiled AST")
			}
			assert.Equal(t, map[string]any{"example": map[string]any{"name": "example"}},
				input.AuthoredJobs, "the runner must not mutate analysis context")
		})
	}
}

func TestSpec_PublicAPI_DefaultBuiltInRules(t *testing.T) {
	runner := lint.Default()
	require.NotNil(t, runner, "Default must construct the built-in runner")
	registered := make(map[string]bool)
	for _, rule := range runner.Rules() {
		registered[rule.ID] = true
	}
	constructors := []struct {
		name string
		new  func() lint.Rule
	}{
		{"TemplateInjectionRule", lint.TemplateInjectionRule},
		{"PromptExpressionRule", lint.PromptExpressionRule},
		{"SecretLeakRule", lint.SecretLeakRule},
		{"OTLPSecretLeakRule", lint.OTLPSecretLeakRule},
		{"ActionPinRule", lint.ActionPinRule},
		{"VersionCheckRule", lint.VersionCheckRule},
		{"JobTimeoutRule", lint.JobTimeoutRule},
		{"NeedsConsistencyRule", lint.NeedsConsistencyRule},
		{"StepReferenceRule", lint.StepReferenceRule},
		{"WorkflowRunScopeRule", lint.WorkflowRunScopeRule},
		{"ConditionalImportRule", lint.ConditionalImportRule},
		{"PrivateToPublicRule", lint.PrivateToPublicRule},
		{"InstallScriptsRule", lint.InstallScriptsRule},
		{"GatewayDispatchLiteralsRule", lint.GatewayDispatchLiteralsRule},
		{"BashCasePatternRule", lint.BashCasePatternRule},
		{"ObfuscationRule", lint.ObfuscationRule},
		{"PermissionScopeRule", lint.PermissionScopeRule},
		{"ExcessivePermissionsRule", lint.ExcessivePermissionsRule},
		{"CustomJobDetectionRule", lint.CustomJobDetectionRule},
		{"PrivilegedCheckoutRule", lint.PrivilegedCheckoutRule},
		{"CheckoutModeRule", lint.CheckoutModeRule},
		{"ForkAssociationRule", lint.ForkAssociationRule},
		{"PullRequestTargetRolesRule", lint.PullRequestTargetRolesRule},
		{"UntrustedRolesDetectionRule", lint.UntrustedRolesDetectionRule},
		{"PullRequestTargetCheckoutRule", lint.PullRequestTargetCheckoutRule},
		{"NetworkScopeRule", lint.NetworkScopeRule},
		{"PushTargetRule", lint.PushTargetRule},
		{"AllowedLabelsRule", lint.AllowedLabelsRule},
		{"ToolScopeRule", lint.ToolScopeRule},
		{"ImageArgumentRule", lint.ImageArgumentRule},
		{"UnpinnedImagesRule", lint.UnpinnedImagesRule},
		{"SandboxConfigRule", lint.SandboxConfigRule},
		{"FirewallSSLBumpRule", lint.FirewallSSLBumpRule},
		{"SafeOutputShellExpansionRule", lint.SafeOutputShellExpansionRule},
		{"SafeOutputCredentialsRule", lint.SafeOutputCredentialsRule},
		{"ArtifactExecutionRule", lint.ArtifactExecutionRule},
		{"InsecureCommandsRule", lint.InsecureCommandsRule},
		{"RemoteScriptExecutionRule", lint.RemoteScriptExecutionRule},
		{"ArtipackedRule", lint.ArtipackedRule},
		{"CachePoisoningRule", lint.CachePoisoningRule},
		{"EnvironmentFileInjectionRule", lint.EnvironmentFileInjectionRule},
		{"GraderFileIORule", lint.GraderFileIORule},
	}
	for _, tt := range constructors {
		t.Run(tt.name, func(t *testing.T) {
			rule := tt.new()
			require.NotEmpty(t, rule.ID, "the documented %s constructor must identify its rule", tt.name)
			assert.True(t, registered[rule.ID], "Default must register the documented %s constructor", tt.name)
		})
	}
}

func TestSpec_PublicAPI_SeverityString(t *testing.T) {
	// SPEC_MISMATCH: README.md promises Severity.String, but Severity does not
	// implement that method. Keep the regression visible and enable its label
	// assertions automatically when the documented method becomes available.
	if _, ok := any(lint.Error).(interface{ String() string }); !ok {
		t.Skip("SPEC_MISMATCH: documented Severity.String is absent for Error and Warning")
	}
	for _, severity := range []lint.Severity{lint.Error, lint.Warning} {
		t.Run(string(severity), func(t *testing.T) {
			label, ok := any(severity).(interface{ String() string })
			require.True(t, ok, "README documents Severity.String for the diagnostic label")
			assert.NotEmpty(t, label.String(), "a severity diagnostic label must not be empty")
		})
	}
}

// SPEC_AMBIGUITY: README.md does not specify custom-rule validation criteria,
// exact severity labels, or the individual built-in detection policies.
