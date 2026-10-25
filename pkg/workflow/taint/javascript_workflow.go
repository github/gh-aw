package taint

import (
	"fmt"
	"io/fs"
	"maps"
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func (a *analyzer) javascriptInline(text, name string, bindings scope, files scope, captured bool) error {
	rendered, markers, err := a.javascriptRender(text, name, bindings)
	if err != nil {
		return err
	}
	input := JavaScriptInput{Name: name, Source: rendered, Env: make(map[string][]Source), Files: javascriptFileSources(files), Literals: markers, CaptureStdout: captured}
	for key, sources := range bindings {
		if name, ok := strings.CutPrefix(key, "shell."); ok {
			input.Env[name] = sources
		}
	}
	if a.rule.JavaScript == nil && !javascriptInputHasSources(input) {
		return nil
	}
	j := newJavaScript(a, input, a.rule.JavaScript)
	if err := j.run(); err != nil {
		return err
	}
	if strings.HasSuffix(name, ".with.script") {
		j.transportOutput(&JavaScriptNode{Line: 1}, "result", j.returns.taint())
	}

	for name, sources := range j.files {
		files[name] = union(files[name], sources)
	}
	return nil
}

func (j *javascript) transportOutput(node *JavaScriptNode, name string, sources value) {
	j.files["$GITHUB_OUTPUT"] = union(j.files["$GITHUB_OUTPUT"], sources)
	transport := j.a.stepOutput
	if transport != nil && j.options != nil && j.options.fastMatched != nil && j.options.fastEntries == nil && len(j.options.fastSites[j.input.Name]) == 0 {
		copy := *transport
		copy.modeled = false
		transport = &copy
		if len(sources) > 0 {
			j.gap(node, "authored JavaScript output has no verified compiled entry; downstream output transport is not fully modeled")
		}
	}
	j.a.transportOutput(Site{Kind: Output, Path: j.scriptPath(), Line: node.Line}, name, sources, transport)
}

func (a *analyzer) javascriptRender(text, name string, bindings scope) (string, map[string][]Source, error) {
	markers := make(map[string][]Source)
	rendered, _, err := a.interpolate(text, name, bindings, func(i int, sources value) string {
		marker := fmt.Sprintf("GH_AW_JS_TAINT_%d_", i)
		for suffix := 0; strings.Contains(text, marker); suffix++ {
			marker = fmt.Sprintf("GH_AW_JS_TAINT_%d_%d_", i, suffix)
		}
		markers[marker] = sources
		return marker
	})
	return rendered, markers, err
}

func (s *shell) javascriptCommand(cmd *syntax.CallExpr, captured bool, stdin value) (value, bool) { //nolint:largefunc // Keep entry-point resolution and stream handling together.
	command, exists := sliceAt(cmd.Args, 0)
	if !exists {
		return nil, false
	}
	name := literalShellWord(command)
	line := int(command.Pos().Line())
	if name != "node" && name != "nodejs" && !s.javascriptExecutable(command) {
		return nil, false
	}
	if s.a.rule.JavaScript == nil || s.a.rule.JavaScript.Parser == nil {
		if len(stdin) == 0 && len(s.javascriptArguments(cmd.Args[1:])) == 0 {
			return nil, true
		}
		s.a.gapAt(s.path, line, "JavaScript analysis unavailable; configure the pinned native TypeScript compiler module")
		// No output transformation is known across an unavailable boundary.
		return nil, true
	}
	input := JavaScriptInput{
		Name: s.path, Env: make(map[string][]Source), Stdin: stdin,
		Files: javascriptFileSources(s.files), Literals: javascriptFileSources(s.markers), CaptureStdout: captured,
	}

	for name, sources := range s.vars {
		input.Env[name] = sources
	}
	arg := 1
	inline := false
	printExpression := false
	if word, ok := sliceAt(cmd.Args, arg); ok {
		switch literalShellWord(word) {
		case "-e", "--eval", "-p", "--print":
			inline = true
			printExpression = literalShellWord(word) == "-p" || literalShellWord(word) == "--print"
			arg++
		case "--":
			arg++
		}
	}
	entry, exists := sliceAt(cmd.Args, arg)
	if !exists {
		s.a.gapAt(s.path, line, "JavaScript entry point is unresolved")
		return nil, true
	}
	texts, known := s.javascriptWordTexts(entry)
	text, exists := sliceAt(texts, 0)
	if !known || !exists || len(texts) != 1 || strings.HasPrefix(text, "-") {
		s.a.gapAt(s.path, line, "JavaScript entry point or interpreter options are unresolved")
		return nil, true
	}
	for _, word := range cmd.Args[arg+1:] {
		input.Args = append(input.Args, s.word(word))
	}
	if inline {
		input.Source = text
		input.ArgvOffset = 1
		if printExpression {
			input.Source = "console.log(" + text + ")"
		}
	} else {
		sourcePath, ok := s.javascriptSourcePath(text)
		if !ok {
			s.a.gapAt(s.path, line, "JavaScript repository entry point is unresolved; configure verified runtime source paths")
			return nil, true
		}
		source, err := javascriptReadSource(s.a.rule.JavaScript.Files, sourcePath)
		if err != nil {
			s.a.gapAt(s.path, line, "JavaScript repository entry point source is unavailable")
			return nil, true
		}
		input.Source = string(source)
		// Resolve relative module imports from the repository source location,
		// while diagnostics retain the original workflow AST location.
		j := newJavaScript(s.a, input, s.a.rule.JavaScript)
		j.file = sourcePath
		if err := j.run(); err != nil {
			s.a.failure = err
		}
		s.javascriptFiles(j.files)
		return j.stdout, true
	}
	j := newJavaScript(s.a, input, s.a.rule.JavaScript)
	if err := j.run(); err != nil {
		s.a.failure = err
	}
	s.javascriptFiles(j.files)
	return j.stdout, true
}

func javascriptFileSources(files scope) map[string][]Source {
	result := make(map[string][]Source, len(files))
	for name, sources := range files {
		result[name] = sources
	}
	return result
}

func javascriptInputHasSources(input JavaScriptInput) bool {
	if len(input.Stdin) > 0 {
		return true
	}
	for _, sources := range input.Env {
		if len(sources) > 0 {
			return true
		}
	}
	for _, sources := range input.Literals {
		if len(sources) > 0 {
			return true
		}
	}
	return false
}

func (s *shell) javascriptFiles(files map[string][]Source) {
	for name, sources := range files {
		s.files[name] = union(s.files[name], sources)
	}
}

func (s *shell) javascriptArguments(words []*syntax.Word) value {
	var result value
	for _, word := range words {
		result = union(result, s.word(word))
	}
	return result
}

func (s *shell) javascriptWordTexts(word *syntax.Word) ([]string, bool) {
	constants := maps.Clone(s.constants)
	for prefix := range s.a.rule.JavaScript.RuntimePaths {
		if suffix, ok := strings.CutPrefix(prefix, "${"); ok {
			if name, _, ok := strings.Cut(suffix, "}"); ok {
				constants[name] = "${" + name + "}"
			}
		}
	}
	return wordTexts(word, constants, s.constantValues, s.constantUnknown)
}

func (s *shell) javascriptSourcePath(name string) (string, bool) {
	options := s.a.rule.JavaScript
	if options.Files == nil {
		return "", false
	}
	if mapped, ok := javascriptRuntimePath(options.RuntimePaths, name); ok {
		return mapped, fs.ValidPath(mapped)
	}
	name = path.Clean(path.Join(s.workingDirectory, name))
	return name, fs.ValidPath(name)
}

// Infer command identity only from a complete, literal Node locator expression.
// A variable named NODE or a path whose basename is node proves nothing.
func (s *shell) javascriptExecutable(word *syntax.Word) bool {
	if len(word.Parts) != 1 {
		return false
	}
	part, _ := sliceAt(word.Parts, 0)
	if quoted, ok := part.(*syntax.DblQuoted); ok && len(quoted.Parts) == 1 {
		part, _ = sliceAt(quoted.Parts, 0)
	}
	param, ok := part.(*syntax.ParamExp)
	if !ok || param.Param == nil || param.Index != nil || param.Exp != nil || param.Excl {
		return false
	}
	_, exists := s.javascriptExecutables[param.Param.Value]
	return exists
}

func javascriptExecutables(tree *syntax.File) map[string]struct{} {
	result := make(map[string]struct{})
	invalid := make(map[string]struct{})
	syntax.Walk(tree, func(node syntax.Node) bool {
		assign, ok := node.(*syntax.Assign)
		if !ok || assign.Name == nil {
			return true
		}
		name := assign.Name.Value
		if !javascriptLocator(assign.Value) {
			invalid[name] = struct{}{}
		} else {
			result[name] = struct{}{}
		}
		return true
	})
	for name := range invalid {
		delete(result, name)
	}
	return result
}

func javascriptLocator(word *syntax.Word) bool {
	if word == nil || len(word.Parts) != 1 {
		return false
	}
	part, _ := sliceAt(word.Parts, 0)
	sub, ok := part.(*syntax.CmdSubst)
	if !ok || len(sub.Stmts) != 1 {
		return false
	}
	fallback, valid := false, true
	syntax.Walk(sub, func(node syntax.Node) bool {
		if node == nil {
			return false
		}
		if binary, ok := node.(*syntax.BinaryCmd); ok && binary.Op.String() != "||" {
			valid = false
		}
		if redirect, ok := node.(*syntax.Redirect); ok {
			if redirect.N == nil || redirect.N.Value != "2" || redirect.Op.String() != ">" || literalShellWord(redirect.Word) != "/dev/null" {
				valid = false
			}
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			switch node.(type) {
			case *syntax.CmdSubst, *syntax.Stmt, *syntax.BinaryCmd, *syntax.Redirect, *syntax.Word, *syntax.Lit:
			default:
				valid = false
			}
			return true
		}
		args := make([]string, 0, len(call.Args))
		for _, arg := range call.Args {
			args = append(args, literalShellWord(arg))
		}
		switch strings.Join(args, " ") {
		case "which node", "command -v node":
		case "echo node":
			fallback = true
		default:
			valid = false
		}
		return false
	})
	return valid && fallback
}
