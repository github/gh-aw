package lint

import (
	"bytes"
	"context"
	"errors"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/checkoutpolicy"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
	"mvdan.cc/sh/v3/syntax"
)

func ArtifactExecutionRule() Rule {
	return Rule{
		ID: "artifact-execution", Description: "Review cross-run artifact workspace extraction and direct execution on workflow_run",
		Severity: Warning, Targets: ActionsAST | AWAST, Check: checkArtifactExecution,
	}
}

type artifactDownload struct {
	directory string
	path      string
}

const (
	artifactWorkspaceRoot = "/__gh_aw__/workspace"
	artifactTempRoot      = "/__gh_aw__/temp"
)

func checkArtifactExecution(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	if _, found, err := authoredTrigger(ctx, executionPolicyOn(input), "workflow_run"); err != nil || !found {
		return nil, err
	}
	blocks, err := executionPolicyBlocks(ctx, input)
	failures := []error{err}
	var diagnostics []Diagnostic
	for _, block := range blocks {
		var downloads []artifactDownload
		for _, step := range block.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			download, found, err := crossRunArtifact(ctx, step)
			failures = append(failures, err)
			if found {
				downloads = append(downloads, download)
				if artifactContains(download.directory, artifactWorkspaceRoot) || download.directory == "" {
					message := "Cross-run artifact extraction may overwrite workspace code or configuration; extract into a dedicated literal directory and consume validated data rather than executable content"
					if download.directory == "" {
						message = "Cross-run artifact extraction path is dynamic or unsupported; use a dedicated literal directory and review downstream execution separately"
					}
					diagnostics = append(diagnostics, Diagnostic{Path: download.path, Message: message})
				}
			}
			if len(downloads) == 0 {
				continue
			}
			foundDiagnostics, err := artifactStepExecution(ctx, input, block, step, downloads)
			diagnostics = append(diagnostics, foundDiagnostics...)
			failures = append(failures, err)
		}
	}
	return diagnostics, errors.Join(failures...)
}

func crossRunArtifact(ctx context.Context, step aw.ExecutionStep) (artifactDownload, bool, error) {
	name := actionName(step.Fields["uses"])
	if name != "actions/download-artifact" && name != "dawidd6/action-download-artifact" {
		return artifactDownload{}, false, nil
	}
	with, err := policyMapping(step.Fields["with"], step.Path+".with")
	if err != nil {
		return artifactDownload{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return artifactDownload{}, false, err
	}
	runKey := "run-id"
	if name == "dawidd6/action-download-artifact" {
		runKey = "run_id"
	}
	run, explicit := with[runKey]
	if name == "actions/download-artifact" {
		token, supplied := with["github-token"]
		if !supplied || artifactEmptyToken(token) {
			return artifactDownload{}, false, nil
		}
		if !explicit || run == nil || run == "" {
			return artifactDownload{}, false, nil
		}
	}
	if text, ok := run.(string); ok && checkoutpolicy.MatchesExpression(text, "github.run_id") {
		return artifactDownload{}, false, nil
	}
	directory := artifactWorkspaceRoot
	if raw, explicit := with["path"]; explicit {
		if text, ok := raw.(string); ok {
			directory = artifactLiteralPath(text)
		} else {
			directory = ""
		}
	}
	return artifactDownload{directory: directory, path: step.Path + ".with.path"}, true, nil
}

func artifactEmptyToken(value any) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	if !ok {
		return false
	}
	if strings.TrimSpace(text) == "" {
		return true
	}
	expression, ok := actionslanguageservice.WrappedExpression(text)
	if !ok {
		return false
	}
	for expression.Kind == "group" {
		expression = expression.Child(0)
	}
	if expression.Kind != "literal" {
		return false
	}
	if text, ok := expression.Token.Value.(string); expression.Token.Kind == "string" && ok {
		return strings.TrimSpace(text) == ""
	}
	return expression.Token.Kind == "null"
}

func artifactLiteralPath(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "${{") {
		if end := strings.Index(text, "}}"); end >= 0 {
			root, suffix := artifactExpressionRoot(text[:end+2]), text[end+2:]
			if root != "" && (suffix == "" || strings.HasPrefix(suffix, "/")) {
				text = root + suffix
			}
		}
	}
	if strings.Contains(text, "${{") || strings.HasPrefix(text, "~") {
		return ""
	}
	return artifactResolvePath(artifactWorkspaceRoot, text)
}

func artifactExpressionRoot(text string) string {
	for expression, root := range map[string]string{
		"github.workspace": artifactWorkspaceRoot,
		"runner.temp":      artifactTempRoot,
	} {
		if checkoutpolicy.MatchesExpression(text, expression) {
			return root
		}
	}
	return ""
}

func artifactStepExecution(ctx context.Context, input Input, block aw.ExecutionBlock, step aw.ExecutionStep, downloads []artifactDownload) ([]Diagnostic, error) {
	related := func(file string) []RelatedLocation {
		var locations []RelatedLocation
		for _, download := range downloads {
			if artifactContains(download.directory, file) {
				locations = append(locations, RelatedLocation{Path: download.path})
			}
		}
		return locations
	}
	diagnostic := func(locations []RelatedLocation, path string, line int) []Diagnostic {
		if len(locations) == 0 {
			return nil
		}
		return []Diagnostic{{Path: path, Line: line, Related: locations,
			Message: "A later step may execute cross-run artifact content with workflow_run privileges; treat artifacts as untrusted data or establish independent provenance before executing them. Upstream success, branch filters and repository identity alone do not authenticate content",
		}}
	}
	if uses, ok := step.Fields["uses"].(string); ok && strings.HasPrefix(uses, "./") {
		return diagnostic(related(artifactLiteralPath(uses)), step.Path+".uses", 0), nil
	}
	directory := artifactWorkingDirectory(input, block, step)
	return policyShellCalls(ctx, input, block, step, func(call *syntax.CallExpr, script string) ([]Diagnostic, error) {
		var locations []RelatedLocation
		for _, file := range artifactCommandFiles(call, directory, script) {
			locations = append(locations, related(file)...)
		}
		return diagnostic(locations, step.Path+".run", int(call.Pos().Line())), nil
	})
}

func artifactCommandFiles(call *syntax.CallExpr, directory, script string) []string {
	if len(call.Args) == 0 || directory == "" {
		return nil
	}
	argument := func(index int) string {
		if index >= 0 && index < len(call.Args) {
			return artifactShellWord(call.Args[index], script)
		}
		return ""
	}
	command := argument(0)
	hasOperand := func(start int) bool {
		for index := start; index < len(call.Args); index++ {
			value := argument(index)
			if value == "" || !strings.HasPrefix(value, "-") {
				return true
			}
		}
		return false
	}
	switch command {
	case "bash", "sh", "python", "python3", "node", "source", ".":
		if file := artifactInterpreterOperand(command, len(call.Args), argument); file != "" {
			return []string{artifactResolvePath(directory, file)}
		}
	case "npm":
		if len(call.Args) > 1 {
			switch argument(1) {
			case "run", "run-script":
				if hasOperand(2) {
					return []string{directory}
				}
			case "ci", "install", "i", "test", "start", "exec", "rebuild":
				return []string{directory}
			}
		}
	case "npx", "make":
		if len(call.Args) == 1 || hasOperand(1) {
			return []string{directory}
		}
	default:
		if strings.Contains(command, "/") {
			return []string{artifactResolvePath(directory, command)}
		}
	}
	return nil
}

// Select only the script operand, not option payloads or arguments passed to it.
func artifactInterpreterOperand(command string, count int, argument func(int) string) string {
	for index := 1; index < count; index++ {
		value := argument(index)
		if value == "" {
			return ""
		}
		if value == "--" {
			value = argument(index + 1)
			if value == "-" && (command == "python" || command == "python3" || command == "node") {
				return ""
			}
			return value
		}
		if command == "source" || command == "." {
			return value
		}
		if value == "-" {
			if command == "bash" || command == "sh" {
				return argument(index + 1)
			}
			return ""
		}
		shellOption := strings.HasPrefix(value, "+") && (command == "bash" || command == "sh")
		if !strings.HasPrefix(value, "-") && !shellOption {
			return value
		}
		takesValue, noScript := artifactInterpreterOption(command, value)
		if noScript {
			return ""
		}
		if takesValue {
			index++
		}
	}
	return ""
}

func artifactInterpreterOption(command, value string) (takesValue, noScript bool) {
	option, _, attached := strings.Cut(value, "=")
	switch command {
	case "bash", "sh":
		switch option {
		case "--help", "--version":
			return false, true
		case "--rcfile", "--init-file":
			return !attached, false
		}
		stopFlags := "cs"
		if strings.HasPrefix(value, "+") {
			stopFlags = ""
		}
		return artifactInterpreterShortOption(value, "oO", stopFlags)
	case "python", "python3":
		switch option {
		case "--help", "--help-env", "--help-xoptions", "--help-all", "--version":
			return false, true
		case "--check-hash-based-pycs":
			return !attached, false
		}
		return artifactInterpreterShortOption(value, "WX", "cmhV")
	case "node":
		switch option {
		case "-e", "--eval", "-p", "--print", "-pe", "-ep", "--run", "-h", "--help", "-v", "--version":
			return false, true
		case "-r", "--require", "--import", "--loader", "--experimental-loader",
			"-C", "--conditions", "--log-file", "--input-type", "--inspect-port",
			"--title", "--redirect-warnings", "--env-file", "--env-file-if-exists":
			return !attached, false
		}
		return false, strings.HasPrefix(value, "-e") || strings.HasPrefix(value, "-p")
	}
	return false, false
}

func artifactInterpreterShortOption(value, valueFlags, stopFlags string) (takesValue, noScript bool) {
	if strings.HasPrefix(value, "--") {
		return false, false
	}
	flags := strings.TrimPrefix(strings.TrimPrefix(value, "-"), "+")
	for offset, flag := range flags {
		if strings.ContainsRune(stopFlags, flag) {
			return false, true
		}
		if strings.ContainsRune(valueFlags, flag) {
			return offset == len(flags)-1, false
		}
	}
	return false, false
}

func artifactWorkingDirectory(input Input, block aw.ExecutionBlock, step aw.ExecutionStep) string {
	if value, explicit := step.Fields["working-directory"]; explicit {
		if text, ok := value.(string); ok {
			return artifactLiteralPath(text)
		}
		return ""
	}
	var defaults []map[string]any
	if input.Workflow != nil {
		for _, job := range input.Workflow.Jobs {
			if block.Path == "jobs."+string(job.ID) {
				defaults = append(defaults, job.Defaults)
				break
			}
		}
		defaults = append(defaults, input.Workflow.Defaults)
	} else {
		for _, fields := range []map[string]any{block.Fields, input.Document.Frontmatter} {
			if value, ok := fields["defaults"].(map[string]any); ok {
				defaults = append(defaults, value)
			}
		}
	}
	for _, fields := range defaults {
		run, ok := fields["run"].(map[string]any)
		if !ok {
			continue
		}
		if value, explicit := run["working-directory"]; explicit {
			if text, ok := value.(string); ok {
				return artifactLiteralPath(text)
			}
			return ""
		}
	}
	return artifactWorkspaceRoot
}

func artifactResolvePath(directory, file string) string {
	if path.IsAbs(file) {
		return path.Clean(file)
	}
	return path.Join(directory, file)
}

func artifactContains(directory, file string) bool {
	if directory == "" || file == "" {
		return false
	}
	return file == directory || strings.HasPrefix(file, strings.TrimSuffix(directory, "/")+"/")
}

// Reparse original operands so interpolation masks never become literal paths.
func artifactShellWord(word *syntax.Word, script string) string {
	start, end := int(word.Pos().Offset()), int(word.End().Offset())
	if start < 0 || end > len(script) || start >= end {
		return ""
	}
	raw := script[start:end]
	var rendered strings.Builder
	last := 0
	unknown := false
	err := taint.VisitInterpolations(context.Background(), raw, "", func(start, end, _ int, _ actionlint.ExprNode) error {
		rendered.WriteString(raw[last:start])
		if root := artifactExpressionRoot(raw[start:end]); root != "" {
			rendered.WriteString(root)
		} else {
			unknown = true
		}
		last = end
		return nil
	})
	if err != nil || unknown {
		return ""
	}
	rendered.WriteString(raw[last:])
	tree, err := syntax.NewParser().Parse(bytes.NewBufferString(": "+rendered.String()), "")
	if err != nil || len(tree.Stmts) > 1 {
		return ""
	}
	if len(tree.Stmts) > 0 {
		call, ok := tree.Stmts[0].Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) > 2 {
			return ""
		}
		operand := 1
		if operand >= 0 && operand < len(call.Args) {
			var result strings.Builder
			if artifactShellParts(&result, call.Args[operand].Parts, false) {
				return result.String()
			}
		}
	}
	return ""
}

func artifactShellParts(result *strings.Builder, values []syntax.WordPart, quoted bool) bool {
	for _, value := range values {
		switch value := value.(type) {
		case *syntax.Lit:
			if !artifactShellLiteral(result, value.Value, quoted) {
				return false
			}
		case *syntax.SglQuoted:
			if value.Dollar {
				return false
			}
			result.WriteString(value.Value)
		case *syntax.DblQuoted:
			if !artifactShellParts(result, value.Parts, true) {
				return false
			}
		case *syntax.ParamExp:
			if value.Param == nil ||
				(!value.Short && (value.Excl || value.Length || value.Width || value.Index != nil ||
					value.Slice != nil || value.Repl != nil || value.Exp != nil)) {
				return false
			}
			switch value.Param.Value {
			case "GITHUB_WORKSPACE":
				result.WriteString(artifactWorkspaceRoot)
			case "RUNNER_TEMP":
				result.WriteString(artifactTempRoot)
			default:
				return false
			}
		default:
			return false
		}
	}
	return true
}

func artifactShellLiteral(result *strings.Builder, text string, quoted bool) bool {
	skip := false
	for index, ch := range []byte(text) {
		if skip {
			skip = false
			continue
		}
		nextIndex := index + 1
		if ch == '\\' && nextIndex >= 0 && nextIndex < len(text) {
			next := text[nextIndex]
			if !quoted || strings.ContainsRune("$`\"\\\n", rune(next)) {
				skip = true
				if next != '\n' {
					result.WriteByte(next)
				}
				continue
			}
		}
		if !quoted && strings.ContainsRune("*?[~", rune(ch)) {
			return false
		}
		result.WriteByte(ch)
	}
	return true
}
