package lint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
	"mvdan.cc/sh/v3/syntax"
)

const allowUnsecureCommandsEnv = "ACTIONS_ALLOW_UNSECURE_COMMANDS"

// Deprecated stdout workflow commands and the environment files replacing them.
var deprecatedWorkflowCommands = []struct{ name, file string }{
	{"set-env", "GITHUB_ENV"},
	{"add-path", "GITHUB_PATH"},
	{"set-output", "GITHUB_OUTPUT"},
	{"save-state", "GITHUB_STATE"},
}

func InsecureCommandsRule() Rule {
	return Rule{
		ID: "insecure-commands", Description: "Detect re-enabled or emitted deprecated set-env and add-path workflow commands",
		Severity: Error, Targets: ActionsAST | AWAST, Check: checkInsecureCommands,
	}
}

type envFlagState int

const (
	envFlagOff envFlagState = iota
	envFlagOn
	envFlagDynamic
)

func checkInsecureCommands(ctx context.Context, input Input) ([]Diagnostic, error) {
	blocks, err := executionPolicyBlocks(ctx, input)
	failures := []error{err}
	var diagnostics []Diagnostic
	checkEnv := func(env any, path string) {
		fields, ok := env.(map[string]any)
		if !ok {
			return
		}
		value, ok := fields[allowUnsecureCommandsEnv]
		if !ok {
			return
		}
		valuePath := path + "." + allowUnsecureCommandsEnv
		state, err := allowUnsecureCommandsState(ctx, value, valuePath)
		failures = append(failures, err)
		if diagnostic, found := insecureCommandsEnvDiagnostic(state, valuePath); found {
			diagnostics = append(diagnostics, diagnostic)
		}
	}

	if workflow := input.Workflow; workflow != nil {
		checkEnv(renderActionsEnv(workflow.Env, "", workflow, &failures), "env")
		for _, job := range workflow.Jobs {
			checkEnv(renderActionsEnv(job.Env, job.ID, workflow, &failures), "jobs."+string(job.ID)+".env")
		}
	} else if input.Document != nil {
		for _, block := range blocks {
			checkEnv(block.Fields["env"], block.FieldPath("env"))
		}
		if engine, ok := input.Document.Frontmatter["engine"].(map[string]any); ok {
			checkEnv(engine["env"], "engine.env")
		}
	}

	for _, block := range blocks {
		for _, step := range block.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			checkEnv(step.Fields["env"], step.Path+".env")
			script, ok := step.Fields["run"].(string)
			if !ok {
				continue
			}
			shell, known := effectiveRunShell(input, block, step)
			if !known || !posixRunShell(shell) {
				continue
			}
			found, err := deprecatedWorkflowCommandEmitters(ctx, script, step.Path+".run")
			diagnostics = append(diagnostics, found...)
			failures = append(failures, err)
		}
	}
	return diagnostics, errors.Join(failures...)
}

func renderActionsEnv(env map[string]actions.Expression, consumer actions.JobID, workflow *actions.Workflow, failures *[]error) any {
	expr, ok := env[allowUnsecureCommandsEnv]
	if !ok {
		return nil
	}
	value, err := actions.RenderExpression(expr, consumer, workflow, true)
	if err != nil {
		*failures = append(*failures, fmt.Errorf("cannot inspect %s: invalid Actions env expression; correct the workflow before linting", allowUnsecureCommandsEnv))
		return nil
	}
	return map[string]any{allowUnsecureCommandsEnv: value}
}

func insecureCommandsEnvDiagnostic(state envFlagState, path string) (Diagnostic, bool) {
	switch state {
	case envFlagOn:
		return Diagnostic{Path: path,
			Message: allowUnsecureCommandsEnv + " re-enables the deprecated set-env and add-path workflow commands, letting any process that prints to the log inject environment variables or PATH entries (CVE-2020-15228); remove it and write to GITHUB_ENV or GITHUB_PATH instead"}, true
	case envFlagDynamic:
		return Diagnostic{Severity: Warning, Path: path,
			Message: allowUnsecureCommandsEnv + " is set from an expression and may re-enable the deprecated set-env and add-path workflow commands (CVE-2020-15228); remove it and write to GITHUB_ENV or GITHUB_PATH instead"}, true
	}
	return Diagnostic{}, false
}

// The runner enables insecure commands only when bool.TryParse accepts the
// value as true, so numbers and other strings are inert.
func allowUnsecureCommandsState(ctx context.Context, value any, path string) (envFlagState, error) {
	switch value := value.(type) {
	case bool:
		if value {
			return envFlagOn, nil
		}
		return envFlagOff, nil
	case string:
		text := strings.TrimSpace(value)
		if !strings.Contains(text, "${{") {
			if strings.EqualFold(text, "true") {
				return envFlagOn, nil
			}
			return envFlagOff, nil
		}
		state := envFlagDynamic
		err := taint.VisitInterpolations(ctx, text, path, func(start, end, _ int, node actionlint.ExprNode) error {
			if start != 0 || end != len(text) {
				return ctx.Err()
			}
			switch node := node.(type) {
			case *actionlint.BoolNode:
				state = envFlagOff
				if node.Value {
					state = envFlagOn
				}
			case *actionlint.StringNode:
				state = envFlagOff
				if strings.EqualFold(strings.TrimSpace(node.Value), "true") {
					state = envFlagOn
				}
			case *actionlint.NullNode, *actionlint.IntNode, *actionlint.FloatNode:
				state = envFlagOff
			}
			return ctx.Err()
		})
		return state, err
	}
	return envFlagOff, nil
}

func posixRunShell(shell string) bool {
	name, _, _ := strings.Cut(shell, " ")
	return name == "" || name == "bash" || name == "sh"
}

func deprecatedWorkflowCommandEmitters(ctx context.Context, script, path string) ([]Diagnostic, error) {
	// Expressions are runner-provided data, not shell grammar. Keep byte offsets
	// and line numbers intact without evaluating or reproducing their values.
	rendered := []byte(script)
	err := taint.VisitInterpolations(ctx, script, path, func(start, end, _ int, _ actionlint.ExprNode) error {
		maskBashRange(rendered[start:end], 'x')
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	tree, err := syntax.NewParser().Parse(bytes.NewReader(rendered), "")
	if err != nil {
		// Shell syntax errors and other interpreters belong to their own checks.
		return nil, ctx.Err()
	}
	var diagnostics []Diagnostic
	syntax.Walk(tree, func(node syntax.Node) bool {
		if ctx.Err() != nil {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		if name := call.Args[0].Lit(); name != "echo" && name != "printf" {
			return true
		}
		for _, arg := range call.Args[1:] {
			prefix := literalWordPrefix(arg)
			for _, command := range deprecatedWorkflowCommands {
				rest, found := strings.CutPrefix(prefix, "::"+command.name)
				if !found || (rest != "" && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "::")) {
					continue
				}
				diagnostics = append(diagnostics, Diagnostic{
					Severity: Warning, Path: path, Line: int(arg.Pos().Line()),
					Message: fmt.Sprintf("Run script emits the deprecated ::%s workflow command, which the runner disables or will remove; append the record to $%s instead", command.name, command.file),
				})
				return true
			}
		}
		return true
	})
	return diagnostics, ctx.Err()
}

// literalWordPrefix returns the statically known leading text of a shell word.
func literalWordPrefix(word *syntax.Word) string {
	var prefix strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			prefix.WriteString(part.Value)
		case *syntax.SglQuoted:
			prefix.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return prefix.String()
				}
				prefix.WriteString(lit.Value)
			}
		default:
			return prefix.String()
		}
	}
	return prefix.String()
}
