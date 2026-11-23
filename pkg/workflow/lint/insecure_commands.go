package lint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
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
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	blocks, err := executionPolicyBlocks(ctx, input)
	failures := []error{err}
	var diagnostics []Diagnostic
	checkEnv := func(env any, path string, windows bool) {
		fields, ok := env.(map[string]any)
		if !ok {
			return
		}
		for _, key := range slices.Sorted(maps.Keys(fields)) {
			if !allowUnsecureCommandsKey(key, windows) {
				continue
			}
			valuePath := path + "." + key
			state, err := allowUnsecureCommandsState(ctx, fields[key], valuePath)
			failures = append(failures, err)
			if diagnostic, found := insecureCommandsEnvDiagnostic(state, valuePath); found {
				diagnostics = append(diagnostics, diagnostic)
			}
		}
	}

	blockWindows := make([]bool, len(blocks))
	anyWindows := false
	for index, block := range blocks {
		blockWindows[index] = windowsRunner(insecureCommandsBlockRunner(input, block))
		anyWindows = anyWindows || blockWindows[index]
	}

	if workflow := input.Workflow; workflow != nil {
		checkEnv(renderActionsEnv(workflow.Env, "", workflow, anyWindows, &failures), "env", anyWindows)
		for _, job := range workflow.Jobs {
			windows := windowsRunner(job.RunsOn)
			checkEnv(renderActionsEnv(job.Env, job.ID, workflow, windows, &failures), "jobs."+string(job.ID)+".env", windows)
		}
	} else if input.Document != nil {
		rootWindows := false
		for index, block := range blocks {
			windows := blockWindows[index]
			if block.Path == "" {
				rootWindows, windows = windows, anyWindows
			}
			checkEnv(block.Fields["env"], block.FieldPath("env"), windows)
		}
		if engine, ok := input.Document.Frontmatter["engine"].(map[string]any); ok {
			checkEnv(engine["env"], "engine.env", rootWindows)
		}
		if sandbox, ok := input.Document.Frontmatter["sandbox"].(map[string]any); ok {
			if agent, ok := sandbox["agent"].(map[string]any); ok {
				checkEnv(agent["env"], "sandbox.agent.env", rootWindows)
			}
		}
	}

	for index, block := range blocks {
		for _, step := range block.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			checkEnv(step.Fields["env"], step.Path+".env", blockWindows[index])
			script, ok := step.Fields["run"].(string)
			if !ok {
				continue
			}
			shell, known := policyRunShell(input, block, step)
			if !known {
				// Unresolved runners keep the historical default-shell analysis.
				shell, known = effectiveRunShell(input, block, step)
			}
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

// Windows environment names are case-insensitive; other runners match exactly.
func allowUnsecureCommandsKey(key string, windows bool) bool {
	if windows {
		return strings.EqualFold(key, allowUnsecureCommandsEnv)
	}
	return key == allowUnsecureCommandsEnv
}

func windowsRunner(runner any) bool {
	shell, known := policyRunnerShell(runner)
	return known && shell == "pwsh"
}

func insecureCommandsBlockRunner(input Input, block aw.ExecutionBlock) any {
	if input.Workflow != nil {
		for _, job := range input.Workflow.Jobs {
			if block.Path == "jobs."+string(job.ID) {
				return job.RunsOn
			}
		}
		return nil
	}
	runner := block.Fields["runs-on"]
	if runner == nil && block.Path == "safe-outputs" && input.Document != nil {
		runner = input.Document.Frontmatter["runs-on-slim"]
	}
	return runner
}

func renderActionsEnv(env map[string]actions.Expression, consumer actions.JobID, workflow *actions.Workflow, windows bool, failures *[]error) any {
	var rendered map[string]any
	for _, key := range slices.Sorted(maps.Keys(env)) {
		if !allowUnsecureCommandsKey(key, windows) {
			continue
		}
		value, err := actions.RenderExpression(env[key], consumer, workflow, true)
		if err != nil {
			*failures = append(*failures, fmt.Errorf("cannot inspect %s: invalid Actions env expression; correct the workflow before linting", allowUnsecureCommandsEnv))
			continue
		}
		if rendered == nil {
			rendered = map[string]any{}
		}
		rendered[key] = value
	}
	return rendered
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
	fields := strings.Fields(shell)
	if len(fields) == 0 {
		return true
	}
	name := fields[0][strings.LastIndexByte(fields[0], '/')+1:]
	return name == "bash" || name == "sh"
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
		switch node := node.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			// Captured output is data until something else prints it.
			return false
		case *syntax.Stmt:
			return !stdoutRedirectedAwayFromLog(node.Redirs)
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		name := call.Args[0].Lit()
		if name != "echo" && name != "printf" {
			return true
		}
		if name == "printf" && strings.HasPrefix(call.Args[1].Lit(), "-v") {
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

// stdoutRedirectedAwayFromLog reports whether the final stdout redirection
// sends output somewhere other than the runner's stdout or stderr streams.
func stdoutRedirectedAwayFromLog(redirs []*syntax.Redirect) bool {
	away := false
	for _, redir := range redirs {
		fd := "1"
		if redir.N != nil {
			fd = redir.N.Value
		}
		switch redir.Op {
		case syntax.RdrAll, syntax.AppAll:
		case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.DplOut:
			if fd != "1" {
				continue
			}
		default:
			continue
		}
		target := redir.Word.Lit()
		if redir.Op == syntax.DplOut {
			away = target != "1" && target != "2"
			continue
		}
		away = target != "/dev/stdout" && target != "/dev/stderr"
	}
	return away
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
