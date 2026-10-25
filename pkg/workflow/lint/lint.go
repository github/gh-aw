// Package lint provides an extensible, in-process linter for Actions/AW ASTs.
// Rules inspect read-only ASTs and report structured, value-free diagnostics.
package lint

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime/trace"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
)

type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

type ASTTargets uint8

const (
	ActionsAST ASTTargets = 1 << iota
	AWAST
)

func (targets ASTTargets) String() string {
	switch targets {
	case AWAST:
		return "aw"
	case ActionsAST | AWAST:
		return "actions,aw"
	default:
		return "actions"
	}
}

type Input struct {
	Workflow *actions.Workflow
	// SourcePath identifies the declaring AW source, including imported fragments
	// and companion Markdown when Workflow is a compiled Actions AST.
	SourcePath string
	// Document optionally supplies authored AW fields, including the prompt.
	// Either AST may be supplied independently. Workflow is never a projection
	// of AW frontmatter; when both are supplied it is the compiled graph.
	Document   *aw.Document
	JavaScript *taint.JavaScriptOptions
	// AllowActionRefs permits well-formed mutable references as warnings.
	// Malformed references remain errors regardless of this compiler allowance.
	AllowActionRefs bool
	// SafeOutputJobs includes imported integration jobs resolved by the compiler.
	SafeOutputJobs []string
}

type Diagnostic struct {
	RuleID       string            `json:"rule_id"`
	ThreatRuleID string            `json:"threat_rule_id,omitempty"`
	Severity     Severity          `json:"severity"`
	Path         string            `json:"path"`
	Line         int               `json:"script_line,omitempty"`
	Message      string            `json:"message"`
	Related      []RelatedLocation `json:"related,omitempty"`
}

type RelatedLocation struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
}

// Rule is independent of the taint engine: future structural and semantic
// checkers can share the registry, selection, diagnostics, and CLI integration.
// Check must not mutate Input or include sensitive values in diagnostics.
type Rule struct {
	ID string
	// ThreatRuleID optionally identifies the corresponding compiler threat
	// specification rule; a mapping does not imply full rule conformance.
	ThreatRuleID string
	Description  string
	Severity     Severity
	// Zero preserves the Actions-only contract of existing extension rules.
	Targets ASTTargets
	Check   func(context.Context, Input) ([]Diagnostic, error)
}

type Runner struct {
	rules []Rule
}

var ruleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
var threatRuleIDPattern = regexp.MustCompile(`^CTR-[0-9]{3}$`)

func New(rules ...Rule) (*Runner, error) {
	seen := make(map[string]struct{})
	for _, rule := range rules {
		if !ruleIDPattern.MatchString(rule.ID) || rule.Description == "" || rule.Check == nil {
			return nil, errors.New("lint rule requires a kebab-case id, description, and check")
		}
		if rule.Severity != Error && rule.Severity != Warning {
			return nil, fmt.Errorf("lint rule %q requires error or warning severity", rule.ID)
		}
		if rule.ThreatRuleID != "" && !threatRuleIDPattern.MatchString(rule.ThreatRuleID) {
			return nil, fmt.Errorf("lint rule %q requires a compiler threat ID in CTR-NNN form", rule.ID)
		}
		if rule.Targets & ^(ActionsAST|AWAST) != 0 {
			return nil, errors.New("lint rule targets must be Actions or AW ASTs")
		}
		if _, exists := seen[rule.ID]; exists {
			return nil, fmt.Errorf("duplicate lint rule %q", rule.ID)
		}
		seen[rule.ID] = struct{}{}
	}
	return &Runner{rules: slices.Clone(rules)}, nil
}

func (r *Runner) Rules() []Rule {
	return slices.Clone(r.rules)
}

// Select rejects unknown IDs rather than silently skipping a requested check.
// An empty selection runs every registered rule.
func (r *Runner) Select(ids ...string) (*Runner, error) {
	if len(ids) == 0 {
		return New(r.rules...)
	}
	var selected []Rule
	seen := make(map[string]struct{})
	for _, id := range ids {
		found := false
		for _, rule := range r.rules {
			if rule.ID != id {
				continue
			}
			found = true
			if _, exists := seen[id]; !exists {
				selected = append(selected, rule)
				seen[id] = struct{}{}
			}
			break
		}
		if !found {
			return nil, fmt.Errorf("unknown workflow lint rule %q", id)
		}
	}
	return New(selected...)
}

// Run returns diagnostics even when another rule fails. Tooling failures are
// errors, not successful empty reports. Rule IDs/severities are registry-owned.
func (r *Runner) Run(ctx context.Context, input Input) ([]Diagnostic, error) {
	if input.Workflow == nil && input.Document == nil {
		return nil, errors.New("workflow lint requires an Actions or AW AST")
	}
	var diagnostics []Diagnostic
	var failures []error
	region := trace.StartRegion(ctx, "workflow:lint")
	defer region.End()
	checked := 0
	if performanceLog.Enabled() {
		started := time.Now()
		defer func() {
			performanceLog.Printf("run_complete duration_ns=%d rules=%d diagnostics=%d failed=%t",
				time.Since(started).Nanoseconds(), checked, len(diagnostics), len(failures) != 0 || ctx.Err() != nil)
		}()
	}
	for _, rule := range r.rules {
		if err := ctx.Err(); err != nil {
			return diagnostics, errors.Join(append(failures, err)...)
		}
		targets := rule.Targets
		if targets == 0 {
			targets = ActionsAST
		}
		if (input.Workflow == nil || targets&ActionsAST == 0) && (input.Document == nil || targets&AWAST == 0) {
			continue
		}
		checked++
		findings, err := checkRulePerformance(ctx, rule, input)
		if err != nil {
			failures = append(failures, fmt.Errorf("lint rule %s: %w", rule.ID, err))
		}
		for _, diagnostic := range findings {
			diagnostic.RuleID = rule.ID
			diagnostic.ThreatRuleID = rule.ThreatRuleID
			if diagnostic.Severity == "" {
				diagnostic.Severity = rule.Severity
			}
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	diagnostics = normalizeDiagnosticsPerformance(ctx, diagnostics)
	return diagnostics, errors.Join(failures...)
}

func sortDiagnostics(diagnostics []Diagnostic) {
	slices.SortFunc(diagnostics, func(a, b Diagnostic) int {
		for _, pair := range [][2]string{{a.Path, b.Path}, {a.RuleID, b.RuleID}} {
			if order := strings.Compare(pair[0], pair[1]); order != 0 {
				return order
			}
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		if order := strings.Compare(string(a.Severity), string(b.Severity)); order != 0 {
			return order
		}
		return strings.Compare(a.Message, b.Message)
	})
}

func Default() *Runner {
	return &Runner{rules: []Rule{
		SecretLeakRule(),
		OTLPSecretLeakRule(),
		ActionPinRule(),
		JobTimeoutRule(),
		NeedsConsistencyRule(),
		StepReferenceRule(),
		TemplateInjectionRule(),
		WorkflowRunScopeRule(),
		PermissionScopeRule(),
		VersionCheckRule(),
		ConditionalImportRule(),
		PrivateToPublicRule(),
		InstallScriptsRule(),
		AllowedLabelsRule(),
		NetworkScopeRule(),
		PushTargetRule(),
		ToolScopeRule(),
		SandboxConfigRule(),
		ImageArgumentRule(),
		CheckoutModeRule(),
		ForkAssociationRule(),
		PullRequestTargetRolesRule(),
		SafeOutputShellExpansionRule(),
		FirewallSSLBumpRule(),
		CustomJobDetectionRule(),
		GraderFileIORule(),
		GraderShellFileWriteRule(),
		PullRequestTargetCheckoutRule(),
		PromptExpressionRule(),
		GatewayDispatchLiteralsRule(),
		ArtipackedRule(),
		CachePoisoningRule(),
		ExcessivePermissionsRule(),
		ObfuscationRule(),
		BashCasePatternRule(),
	}}
}
