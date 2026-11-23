package lint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
	"mvdan.cc/sh/v3/syntax"
)

// RemoteScriptExecutionRule reports run steps that execute content fetched
// from the network without a verification step, such as curl ... | bash.
func RemoteScriptExecutionRule() Rule {
	return Rule{
		ID: "remote-script-execution", Description: "Avoid executing downloaded scripts without integrity verification",
		Severity: Warning, Targets: ActionsAST | AWAST, Check: checkRemoteScriptExecution,
	}
}

const remoteScriptRemediation = "without integrity verification; download a pinned, versioned artifact to a file and verify its checksum or signature before executing it, or use a pinned action"

func checkRemoteScriptExecution(ctx context.Context, input Input) ([]Diagnostic, error) {
	blocks, err := executionPolicyBlocks(ctx, input)
	failures := []error{err}
	var diagnostics []Diagnostic
	for _, block := range blocks {
		for _, step := range block.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			script, ok := step.Fields["run"].(string)
			if !ok {
				continue
			}
			shell, known := policyRunShell(input, block, step)
			if !known || !remotePosixShell(shell) {
				continue
			}
			found, err := remoteScriptExecutions(ctx, script, step.Path+".run")
			diagnostics = append(diagnostics, found...)
			failures = append(failures, err)
		}
	}
	return diagnostics, errors.Join(failures...)
}

// remotePosixShell reports whether a run shell such as bash, sh -e {0} or
// /bin/bash -e {0} executes the script with a POSIX shell.
func remotePosixShell(shell string) bool {
	fields := strings.Fields(shell)
	if len(fields) == 0 {
		return false
	}
	name := path.Base(fields[0])
	return name == "bash" || name == "sh"
}

func remoteScriptExecutions(ctx context.Context, script, diagnosticPath string) ([]Diagnostic, error) {
	rendered := []byte(script)
	err := taint.VisitInterpolations(ctx, script, diagnosticPath, func(start, end, _ int, _ actionlint.ExprNode) error {
		maskBashRange(rendered[start:end], 'x')
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	tree, err := syntax.NewParser().Parse(bytes.NewReader(rendered), "")
	if err != nil {
		// Shell syntax errors belong to other checks.
		return nil, ctx.Err()
	}
	var diagnostics []Diagnostic
	report := func(node syntax.Node, format string, args ...any) {
		args = append(args, node.Pos().Col())
		diagnostics = append(diagnostics, Diagnostic{
			Path: diagnosticPath, Line: int(node.Pos().Line()),
			Message: fmt.Sprintf(format+" at column %d "+remoteScriptRemediation, args...),
		})
	}
	nestedPipes := make(map[*syntax.BinaryCmd]bool)
	syntax.Walk(tree, func(node syntax.Node) bool {
		if ctx.Err() != nil {
			return false
		}
		switch node := node.(type) {
		case *syntax.BinaryCmd:
			if !remotePipe(node) || nestedPipes[node] {
				return true
			}
			stages := remotePipelineStages(node, nestedPipes)
			for index, stage := range stages {
				if index == 0 {
					continue
				}
				call, ok := stage.Cmd.(*syntax.CallExpr)
				if !ok {
					continue
				}
				interpreter, ok := remoteInterpreterCall(call)
				if !ok || !interpreter.stdin {
					continue
				}
				// Walk upstream while each receiving stage still reads the pipe
				// and each sending stage still writes to it.
				for previous := index - 1; previous >= 0 && !remoteRedirectsStdin(stages[previous+1]); previous-- {
					if previous < index-1 && remoteRedirectsStdout(stages[previous+1]) {
						break
					}
					if downloader := remoteDownload(stages[previous]); downloader != "" {
						report(stage, "Content downloaded with %s is piped into %s", downloader, interpreter.name)
						break
					}
				}
			}
		case *syntax.Stmt:
			call, ok := node.Cmd.(*syntax.CallExpr)
			if !ok {
				return true
			}
			interpreter, ok := remoteInterpreterCall(call)
			if !ok {
				return true
			}
			for _, word := range interpreter.code {
				if downloader := remoteDownload(word); downloader != "" {
					report(node, "Content downloaded with %s is executed by %s through substitution", downloader, interpreter.name)
					return true
				}
			}
			if interpreter.stdin {
				for _, redirect := range node.Redirs {
					if redirect.N != nil && redirect.N.Value != "0" {
						continue
					}
					var source syntax.Node
					switch redirect.Op {
					case syntax.RdrIn, syntax.WordHdoc:
						source = redirect.Word
					case syntax.Hdoc, syntax.DashHdoc:
						source = redirect.Hdoc
					}
					if source == nil {
						continue
					}
					if downloader := remoteDownload(source); downloader != "" {
						report(node, "Content downloaded with %s is redirected into %s", downloader, interpreter.name)
						return true
					}
				}
			}
		}
		return true
	})
	return diagnostics, ctx.Err()
}

func remoteRedirectsStdin(stmt *syntax.Stmt) bool {
	for _, redirect := range stmt.Redirs {
		if redirect.N != nil && redirect.N.Value != "0" {
			continue
		}
		switch redirect.Op {
		case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn, syntax.WordHdoc, syntax.Hdoc, syntax.DashHdoc:
			return true
		}
	}
	return false
}

func remoteRedirectsStdout(stmt *syntax.Stmt) bool {
	for _, redirect := range stmt.Redirs {
		if redirect.N != nil && redirect.N.Value != "1" {
			continue
		}
		switch redirect.Op {
		case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.RdrAll, syntax.AppAll:
			return true
		case syntax.DplOut:
			if policyShellLiteral(redirect.Word) != "1" {
				return true
			}
		}
	}
	return false
}

func remotePipe(node *syntax.BinaryCmd) bool {
	return node.Op == syntax.Pipe || node.Op == syntax.PipeAll
}

func remotePipelineStages(node *syntax.BinaryCmd, nested map[*syntax.BinaryCmd]bool) []*syntax.Stmt {
	var stages []*syntax.Stmt
	var flatten func(*syntax.Stmt)
	flatten = func(stmt *syntax.Stmt) {
		if binary, ok := stmt.Cmd.(*syntax.BinaryCmd); ok && remotePipe(binary) && !stmt.Negated && len(stmt.Redirs) == 0 {
			nested[binary] = true
			flatten(binary.X)
			flatten(binary.Y)
			return
		}
		stages = append(stages, stmt)
	}
	flatten(node.X)
	flatten(node.Y)
	return stages
}

type remoteInterpreter struct {
	name  string
	stdin bool
	// code holds words whose values are executed as program text, including
	// script operands that may be process substitutions.
	code []*syntax.Word
}

type remoteInterpreterSyntax struct {
	// codeFlags take the program text as their value.
	codeFlags string
	// longCodeFlags are long-form equivalents of codeFlags.
	longCodeFlags []string
	// valueFlags consume a following argument that is not program text.
	valueFlags     string
	longValueFlags []string
	// stopFlags select a non-stdin program source such as a module.
	stopFlags string
	// stdinFlags force reading the program from standard input.
	stdinFlags string
	// codeFromPositional means codeFlags select the next positional operand
	// rather than consuming a value immediately, as for sh -c.
	codeFromPositional bool
}

var remoteShellSyntax = remoteInterpreterSyntax{
	codeFlags: "c", valueFlags: "oO", longValueFlags: []string{"--rcfile", "--init-file"},
	stdinFlags: "s", codeFromPositional: true,
}

var remoteInterpreters = map[string]remoteInterpreterSyntax{
	"bash": remoteShellSyntax, "sh": remoteShellSyntax, "dash": remoteShellSyntax,
	"zsh": remoteShellSyntax, "ksh": remoteShellSyntax,
	"python": {codeFlags: "c", valueFlags: "WX", stopFlags: "m"},
	"node":   {codeFlags: "ep", longCodeFlags: []string{"--eval", "--print"}, valueFlags: "r", longValueFlags: []string{"--require", "--import"}},
	"perl":   {codeFlags: "eE", valueFlags: "IM"},
	"ruby":   {codeFlags: "eE", valueFlags: "IrC"},
}

func remoteInterpreterName(name string) (string, bool) {
	name = path.Base(name)
	if strings.HasPrefix(name, "python") && strings.TrimRight(strings.TrimPrefix(name, "python"), "0123456789.") == "" {
		return "python", true
	}
	_, ok := remoteInterpreters[name]
	return name, ok
}

func remoteInterpreterCall(call *syntax.CallExpr) (remoteInterpreter, bool) {
	args := remoteUnwrapCommand(call.Args)
	if len(args) == 0 {
		return remoteInterpreter{}, false
	}
	name := policyShellLiteral(args[0])
	switch name {
	case "eval":
		return remoteInterpreter{name: name, code: args[1:]}, true
	case "source", ".":
		return remoteInterpreter{name: name, code: args[1:min(2, len(args))]}, true
	}
	key, ok := remoteInterpreterName(name)
	if !ok {
		return remoteInterpreter{}, false
	}
	grammar := remoteInterpreters[key]
	result := remoteInterpreter{name: path.Base(name)}
	codeFlag, stdinFlag, options := false, false, true
	for index := 1; index < len(args); index++ {
		arg := policyShellLiteral(args[index])
		if options && arg == "--" {
			options = false
			continue
		}
		if options && slices.Contains(grammar.longCodeFlags, arg) {
			if index+1 < len(args) {
				result.code = append(result.code, args[index+1])
			}
			return result, true
		}
		if options && slices.ContainsFunc(grammar.longCodeFlags, func(flag string) bool {
			return strings.HasPrefix(remoteLeadingLiteral(args[index]), flag+"=")
		}) {
			// Attached program text, such as --eval='...', is literal unless
			// the word contains a substitution, which remoteDownload inspects.
			result.code = append(result.code, args[index])
			return result, true
		}
		if options && slices.Contains(grammar.longValueFlags, arg) {
			index++
			continue
		}
		if options && len(arg) > 1 && (arg[0] == '-' || (arg[0] == '+' && grammar.codeFromPositional)) && !strings.HasPrefix(arg, "--") {
			cluster := arg[1:]
			if position := strings.IndexAny(cluster, grammar.stopFlags); position >= 0 {
				return result, true
			}
			if position := strings.IndexAny(cluster, grammar.codeFlags); position >= 0 {
				if grammar.codeFromPositional {
					codeFlag = true
					continue
				}
				if position+1 < len(cluster) {
					// Inline program text, such as -e'...', is a literal.
					return result, true
				}
				if index+1 < len(args) {
					result.code = append(result.code, args[index+1])
				}
				return result, true
			}
			if strings.ContainsAny(cluster, grammar.stdinFlags) {
				stdinFlag = true
			}
			if position := strings.IndexAny(cluster, grammar.valueFlags); position >= 0 && position == len(cluster)-1 {
				index++
			}
			continue
		}
		if options && strings.HasPrefix(arg, "--") {
			continue
		}
		// First operand: program text for -c, a script path, or stdin.
		if codeFlag {
			result.code = append(result.code, args[index])
			return result, true
		}
		if stdinFlag {
			result.stdin = true
			return result, true
		}
		if arg == "-" || arg == "/dev/stdin" {
			result.stdin = true
			return result, true
		}
		result.code = append(result.code, args[index])
		return result, true
	}
	result.stdin = !codeFlag
	return result, true
}

// remoteLeadingLiteral returns the literal text before the first dynamic part
// of word, so --eval="$(curl ...)" yields --eval=.
func remoteLeadingLiteral(word *syntax.Word) string {
	var text strings.Builder
	var parts func([]syntax.WordPart) bool
	parts = func(values []syntax.WordPart) bool {
		for _, value := range values {
			switch value := value.(type) {
			case *syntax.Lit:
				text.WriteString(value.Value)
			case *syntax.SglQuoted:
				text.WriteString(value.Value)
			case *syntax.DblQuoted:
				if !parts(value.Parts) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	parts(word.Parts)
	return text.String()
}

// remoteUnwrapCommand removes privilege and environment wrappers so the
// executed command is analyzed, e.g. sudo -E bash or env VAR=1 sh.
func remoteUnwrapCommand(args []*syntax.Word) []*syntax.Word {
	for len(args) > 0 {
		switch path.Base(policyShellLiteral(args[0])) {
		case "sudo":
			args = args[1:]
			for len(args) > 0 {
				arg := policyShellLiteral(args[0])
				if arg == "--" {
					args = args[1:]
					break
				}
				if !strings.HasPrefix(arg, "-") {
					break
				}
				args = args[1:]
				if (arg == "-u" || arg == "-g" || arg == "-C" || arg == "-h" || arg == "-p") && len(args) > 0 {
					args = args[1:]
				}
			}
		case "env":
			args = args[1:]
			for len(args) > 0 {
				arg := policyShellLiteral(args[0])
				if !strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") {
					break
				}
				args = args[1:]
			}
		case "command", "exec", "nohup", "time":
			args = args[1:]
		default:
			return args
		}
	}
	return args
}

// remoteDownload returns the downloader whose standard output feeds the
// given node, or "" when none is found.
func remoteDownload(node syntax.Node) string {
	if node == nil {
		return ""
	}
	found := ""
	syntax.Walk(node, func(child syntax.Node) bool {
		if found != "" {
			return false
		}
		stmt, ok := child.(*syntax.Stmt)
		if !ok {
			return true
		}
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok {
			return true
		}
		if remoteRedirectsStdout(stmt) {
			return true
		}
		if name := remoteDownloader(call); name != "" {
			found = name
			return false
		}
		return true
	})
	return found
}

func remoteDownloader(call *syntax.CallExpr) string {
	args := remoteUnwrapCommand(call.Args)
	if len(args) == 0 {
		return ""
	}
	name := path.Base(policyShellLiteral(args[0]))
	switch name {
	case "curl":
		if !remoteCurlWritesStdout(args[1:]) {
			return ""
		}
	case "wget":
		if !remoteWgetWritesStdout(args[1:]) {
			return ""
		}
	default:
		return ""
	}
	return name
}

func remoteStdoutPath(value string) bool {
	return value == "-" || value == "/dev/stdout"
}

// remoteCurlValueFlags are curl short options that take a value.
const remoteCurlValueFlags = "AbcCdDeEFHKmoPQrtTuUwxXyYz"

// remoteWgetValueFlags are wget short options that take a value.
const remoteWgetValueFlags = "aABDeIilOoPQRtTUwX"

// remoteShortValue returns the value of the short flag at position in
// cluster, either attached or from the next argument, and whether the next
// argument was consumed.
func remoteShortValue(cluster string, position int, next []*syntax.Word) (string, bool) {
	if rest := cluster[position+1:]; rest != "" {
		return rest, false
	}
	if len(next) == 0 {
		return "", false
	}
	return policyShellLiteral(next[0]), true
}

// remoteCurlWritesStdout reports whether any transfer, as separated by
// --next or -:, writes its download to standard output.
func remoteCurlWritesStdout(args []*syntax.Word) bool {
	toFile := false
	for index := 0; index < len(args); index++ {
		arg := policyShellLiteral(args[index])
		switch {
		case arg == "--next":
			if !toFile {
				return true
			}
			toFile = false
		case arg == "--remote-name" || arg == "--remote-name-all" || arg == "--remote-header-name":
			toFile = true
		case arg == "--output":
			if index+1 >= len(args) || !remoteStdoutPath(policyShellLiteral(args[index+1])) {
				toFile = true
			}
			index++
		case strings.HasPrefix(arg, "--output="):
			if !remoteStdoutPath(strings.TrimPrefix(arg, "--output=")) {
				toFile = true
			}
		case len(arg) > 1 && arg[0] == '-' && arg[1] != '-':
			cluster := arg[1:]
			for position := 0; position < len(cluster); position++ {
				flag := cluster[position]
				switch {
				case flag == ':':
					if !toFile {
						return true
					}
					toFile = false
				case flag == 'O':
					toFile = true
				case strings.IndexByte(remoteCurlValueFlags, flag) >= 0:
					value, consumed := remoteShortValue(cluster, position, args[index+1:])
					if flag == 'o' && !remoteStdoutPath(value) {
						toFile = true
					}
					if consumed {
						index++
					}
					position = len(cluster)
				}
			}
		}
	}
	return !toFile
}

func remoteWgetWritesStdout(args []*syntax.Word) bool {
	for index := range args {
		arg := policyShellLiteral(args[index])
		switch {
		case arg == "--output-document":
			return index+1 < len(args) && remoteStdoutPath(policyShellLiteral(args[index+1]))
		case strings.HasPrefix(arg, "--output-document="):
			return remoteStdoutPath(strings.TrimPrefix(arg, "--output-document="))
		case len(arg) > 1 && arg[0] == '-' && arg[1] != '-':
			cluster := arg[1:]
			for position := range len(cluster) {
				flag := cluster[position]
				if flag == 'n' {
					// -nv, -nc and similar consume the rest of the cluster.
					break
				}
				if strings.IndexByte(remoteWgetValueFlags, flag) < 0 {
					continue
				}
				if flag == 'O' {
					value, _ := remoteShortValue(cluster, position, args[index+1:])
					return remoteStdoutPath(value)
				}
				break
			}
		}
	}
	return false
}
