package lint

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
)

// TemplateInjectionRule checks the direct executable-interpolation trigger of
// CTR-006, not whole-rule conformance or arbitrary shell-code data flow.
func TemplateInjectionRule() Rule {
	return Rule{
		ID:           "template-injection",
		ThreatRuleID: "CTR-006",
		Description:  "Detect attacker-controlled Actions expressions interpolated into run or github-script code",
		Severity:     Error,
		Targets:      ActionsAST | AWAST,
		Check:        checkTemplateInjection,
	}
}

func checkTemplateInjection(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.Workflow == nil {
		if input.Document != nil {
			return checkAuthoredTemplateInjection(ctx, input)
		}
		return nil, errors.New("template-injection requires an Actions AST")
	}
	resolved, err := actions.Resolve(input.Workflow)
	if err != nil {
		return nil, err
	}
	checker := templateInjectionChecker{rule: taint.Rule{
		ID: "template-injection", IsSource: templateInjectionSource,
		IsFilter: templateInjectionFilter,
	}}
	jobs := slices.Clone(resolved.Jobs)
	slices.SortFunc(jobs, func(a, b *actions.Job) int { return strings.Compare(string(a.ID), string(b.ID)) })
	for _, job := range jobs {
		bindings, err := checker.environment(ctx, resolved.Env, "env", job.ID, resolved, nil)
		if err != nil {
			return checker.diagnostics, err
		}
		path := "jobs." + string(job.ID)
		bindings, err = checker.environment(ctx, job.Env, path+".env", job.ID, resolved, bindings)
		if err != nil {
			return checker.diagnostics, err
		}
		for i, step := range job.Steps {
			stepPath := fmt.Sprintf("%s.steps[%d]", path, i)
			if err := checker.step(ctx, step, stepPath, job.ID, resolved, bindings); err != nil {
				return checker.diagnostics, err
			}
		}
	}
	sortDiagnostics(checker.diagnostics)
	return checker.diagnostics, nil
}

type templateInjectionChecker struct {
	rule        taint.Rule
	diagnostics []Diagnostic
}

func (c *templateInjectionChecker) step(ctx context.Context, step actions.Step, path string, job actions.JobID, workflow *actions.Workflow, bindings map[string][]taint.Source) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var script string
	var env map[string][]taint.Source
	var err error
	javascript := false
	switch value := step.(type) {
	case actions.RunStep:
		script = value.Run
		env, err = c.environment(ctx, value.Env, path+".env", job, workflow, bindings)
	case actions.RawStep:
		return c.rawStep(ctx, value.Fields, path, bindings)
	case actions.ActionStep:
		if actionName(value.Uses) != "actions/github-script" {
			return nil
		}
		expression, exists := value.With["script"]
		if !exists {
			return nil
		}
		javascript = true
		script, err = actions.RenderExpression(expression, job, workflow, true)
		if err != nil {
			return fmt.Errorf("cannot render executable script at %s.with.script; correct the action input before linting", path)
		}
		env, err = c.environment(ctx, value.Env, path+".env", job, workflow, bindings)
	default:
		return fmt.Errorf("unsupported step at %s", path)
	}
	if err != nil {
		return err
	}
	scriptPath := path + ".run"
	if javascript {
		scriptPath = path + ".with.script"
	}
	return c.checkScript(ctx, script, scriptPath, javascript, env)
}

func (c *templateInjectionChecker) rawStep(ctx context.Context, fields map[string]any, path string, bindings map[string][]taint.Source) error {
	raw, exists := fields["run"]
	scriptPath, javascript, kind := path+".run", false, "run script"
	if !exists {
		if actionName(fields["uses"]) != "actions/github-script" {
			return nil
		}
		with, err := policyMapping(fields["with"], path+".with")
		if err != nil {
			return err
		}
		raw, exists = with["script"]
		if !exists {
			return nil
		}
		scriptPath, javascript, kind = path+".with.script", true, "github-script code"
	}
	script, ok := raw.(string)
	if !ok {
		return fmt.Errorf("%s must be a string at %s", kind, scriptPath)
	}
	env, err := c.rawEnvironment(ctx, fields["env"], path+".env", bindings)
	if err != nil {
		return err
	}
	return c.checkScript(ctx, script, scriptPath, javascript, env)
}

func (c *templateInjectionChecker) checkScript(ctx context.Context, script, scriptPath string, javascript bool, env map[string][]taint.Source) error {
	// Interpolation happens before every shell/interpreter starts, even
	// within apparent quotes, comments, or quoted heredoc bodies.
	expressions, err := c.text(ctx, script, scriptPath, env)
	for _, expression := range expressions {
		if len(expression.Sources) == 0 {
			continue
		}
		related := make([]RelatedLocation, 0, len(expression.Sources))
		for _, source := range expression.Sources {
			related = append(related, RelatedLocation{Path: source.Path})
		}
		remediation := "bind it through env and use a quoted variable expansion instead"
		if javascript {
			remediation = "bind it through env and read process.env instead"
		}
		c.diagnostics = append(c.diagnostics, Diagnostic{
			Path: scriptPath, Line: expression.Line, Related: slices.Compact(related),
			Message: "Attacker-controlled Actions value is interpolated into executable script text; " +
				remediation,
		})
	}
	return err
}

func (c *templateInjectionChecker) text(ctx context.Context, text, path string, bindings map[string][]taint.Source) ([]taint.TextExpression, error) {
	expressions, err := taint.AnalyzeText(ctx, text, path, c.rule, bindings)
	var parseError *taint.ExpressionError
	if errors.As(err, &parseError) {
		c.diagnostics = append(c.diagnostics, Diagnostic{
			Severity: Warning, Path: path, Line: parseError.Line,
			Message: "Incomplete template-injection analysis: invalid Actions expression; " +
				"fix the expression and rerun the check",
		})
		return expressions, nil
	}
	return expressions, err
}

func (c *templateInjectionChecker) environment(ctx context.Context, env map[string]actions.Expression, path string, job actions.JobID, workflow *actions.Workflow, parent map[string][]taint.Source) (map[string][]taint.Source, error) {
	values := make(map[string]any, len(env))
	for _, key := range slices.Sorted(maps.Keys(env)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text, err := actions.RenderExpression(env[key], job, workflow, true)
		if err != nil {
			return nil, fmt.Errorf("cannot render Actions environment binding at %s.%s", path, key)
		}
		values[key] = text
	}
	return c.rawEnvironment(ctx, values, path, parent)
}

func (c *templateInjectionChecker) rawEnvironment(ctx context.Context, raw any, path string, parent map[string][]taint.Source) (map[string][]taint.Source, error) {
	bindings := maps.Clone(parent)
	if bindings == nil {
		bindings = make(map[string][]taint.Source)
	}
	if raw == nil {
		return bindings, nil
	}
	env, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("environment must be a mapping at %s", path)
	}
	for _, key := range slices.Sorted(maps.Keys(env)) {
		text, ok := env[key].(string)
		if !ok {
			switch env[key].(type) {
			case nil, bool, int, int8, int16, int32, int64,
				uint, uint8, uint16, uint32, uint64, float32, float64:
				text = ""
			default:
				return nil, fmt.Errorf("environment binding must be a scalar at %s.%s", path, key)
			}
		}
		expressions, err := c.text(ctx, text, path+"."+key, parent)
		if err != nil {
			return nil, err
		}
		var sources []taint.Source
		for _, expression := range expressions {
			sources = append(sources, expression.Sources...)
		}
		bindings["env."+strings.ToLower(key)] = sources
	}
	return bindings, nil
}

func templateInjectionFilter(node actionlint.ExprNode) bool {
	switch n := node.(type) {
	case *actionlint.CompareOpNode, *actionlint.NotOpNode:
		return true
	case *actionlint.FuncCallNode:
		switch strings.ToLower(n.Callee) {
		case "contains", "startswith", "endswith":
			return true
		}
	}
	return false
}

func templateInjectionSource(reference string) bool {
	// Match known string-bearing fields, their aggregate parents, and computed
	// indexes. Numeric IDs and arbitrary compiler step/job outputs are not sources.
	sources := []string{
		"inputs", "github.actor", "github.triggering_actor", "github.head_ref", "github.ref", "github.ref_name",
		"github.event.inputs", "github.event.client_payload",
		"github.event.issue.title", "github.event.issue.body", "github.event.issue.user.login",
		"github.event.issue.labels.*.name", "github.event.issue.labels.*.description",
		"github.event.pull_request.title", "github.event.pull_request.body",
		"github.event.pull_request.user.login", "github.event.pull_request.head.ref",
		"github.event.pull_request.head.label", "github.event.pull_request.head.repo.default_branch",
		"github.event.pull_request.labels.*.name", "github.event.pull_request.labels.*.description",
		"github.event.comment.body", "github.event.comment.user.login",
		"github.event.review.body", "github.event.review.user.login",
		"github.event.review_comment.body", "github.event.review_comment.user.login",
		"github.event.discussion.title", "github.event.discussion.body", "github.event.discussion.user.login",
		"github.event.sender.login", "github.event.label.name", "github.event.label.description", "github.event.pages.*.page_name",
		"github.event.milestone.title", "github.event.milestone.description",
		"github.event.head_commit.message", "github.event.head_commit.author.name",
		"github.event.head_commit.author.email", "github.event.head_commit.committer.name",
		"github.event.head_commit.committer.email",
		"github.event.commits.*.message", "github.event.commits.*.author.name",
		"github.event.commits.*.author.email", "github.event.commits.*.committer.name",
		"github.event.commits.*.committer.email",
		"github.event.release.name", "github.event.release.body", "github.event.release.tag_name",
		"github.event.workflow_run.head_branch", "github.event.workflow_run.head_commit.message",
	}
	parts := strings.Split(strings.ToLower(reference), ".")
	for _, source := range sources {
		fields := strings.Split(source, ".")
		length := min(len(parts), len(fields))
		if slices.EqualFunc(parts[:length], fields[:length], func(part, field string) bool {
			return part == field || part == "*" || field == "*"
		}) {
			return true
		}
	}
	return false
}
