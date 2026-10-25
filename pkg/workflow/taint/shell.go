package taint

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"mvdan.cc/sh/v3/syntax"
)

var shellLog = logger.New("workflow:taint:shell")

type shell struct {
	a                     *analyzer
	path                  string
	bindings              scope
	persisted             scope
	files                 scope
	vars                  scope
	markers               scope
	functions             map[string]*syntax.Stmt
	active                map[string]bool
	expandedArgs          map[*syntax.Word]value
	trace                 bool
	initialTrace          bool
	workingDirectory      string
	constants             map[string]string
	constantValues        map[string]map[string]struct{}
	constantUnknown       map[string]bool
	javascriptExecutables map[string]struct{}
}

type constantState struct {
	values  map[string]map[string]struct{}
	unknown map[string]bool
}

func (a *analyzer) script(text string, fields map[string]any, path string, bindings, persisted, files scope) error {
	if name, ok := fields["shell"].(string); ok && (name == "node" || name == "node {0}") {
		return a.javascriptInline(text, path, bindings, files, false)
	}
	s, tree, err := a.parseShell(text, fields, path, bindings, persisted, files)
	if err != nil {
		return err
	}
	if s == nil {
		return nil
	}
	// Iterate to a fixed point because assignments can feed later commands.
	for {
		s.trace = s.initialTrace
		before := s.size()
		s.stmts(tree.Stmts, false, nil)
		if a.failure != nil {
			return a.failure
		}
		if s.size() == before {
			break
		}
	}
	return nil
}

func (a *analyzer) parseShell(text string, fields map[string]any, path string, bindings, persisted, files scope) (*shell, *syntax.File, error) {
	s := &shell{
		a: a, path: path, bindings: bindings, persisted: persisted,
		files: files, vars: make(scope), markers: make(scope),
		functions: make(map[string]*syntax.Stmt), active: make(map[string]bool),
		expandedArgs: make(map[*syntax.Word]value),
		constants:    make(map[string]string), constantValues: make(map[string]map[string]struct{}),
		constantUnknown: make(map[string]bool),
	}
	s.setConstants("GITHUB_OUTPUT", []string{"$GITHUB_OUTPUT"}, false)
	for key, sources := range bindings {
		if name, ok := strings.CutPrefix(key, "shell."); ok {
			s.vars[name] = sources
		}
	}
	rendered, err := s.renderActionsScript(text)
	if err != nil {
		return nil, nil, err
	}
	shellName, err := optionalField[string](fields, "shell", path)
	if err != nil {
		a.gap(path, "shell could not be determined")
		return nil, nil, nil
	}
	if shellName != "" && shellName != "bash" && shellName != "sh" &&
		!strings.HasPrefix(shellName, "bash ") && !strings.HasPrefix(shellName, "sh ") {
		a.gap(path, "only bash and sh run scripts are modeled")
		return nil, nil, nil
	}
	s.initialTrace = shellInvocationTraces(shellName)
	s.trace = s.initialTrace
	s.workingDirectory, err = optionalField[string](fields, "working-directory", path)
	if err != nil {
		a.gap(path, "working directory could not be modeled")
		s.workingDirectory = ""
	}
	if strings.Contains(s.workingDirectory, "${{") {
		a.gap(path, "working directory could not be modeled")
		s.workingDirectory = ""
	}
	tree, parseErr := syntax.NewParser().Parse(strings.NewReader(rendered), "")
	if parseErr != nil {
		a.gap(path, "shell syntax could not be analyzed")
		return nil, nil, nil
	}
	s.javascriptExecutables = javascriptExecutables(tree)
	return s, tree, nil
}

func (s *shell) renderActionsScript(text string) (string, error) {
	if s.a.literalScript {
		return text, nil
	}
	rendered, _, err := s.a.interpolate(text, s.path, s.bindings, func(i int, sources value) string {
		marker := fmt.Sprintf("GH_AW_TAINT_PLACEHOLDER_%d_", i)
		// Actions interpolation precedes shell quoting.
		for suffix := 0; strings.Contains(text, marker); suffix++ {
			marker = fmt.Sprintf("GH_AW_TAINT_PLACEHOLDER_%d_%d_", i, suffix)
		}
		s.markers[marker] = sources
		return marker
	})
	return rendered, err
}

func (s *shell) size() int {
	n := 0
	for _, values := range []scope{s.vars, s.files, s.persisted, s.bindings} {
		for _, sources := range values {
			n += len(sources)
		}
	}
	return n
}

func (s *shell) word(word *syntax.Word) value {
	if word == nil {
		return nil
	}
	if sources, expanded := s.expandedArgs[word]; expanded {
		return sources
	}
	var result value
	syntax.Walk(word, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Lit:
			for marker, sources := range s.markers {
				if strings.Contains(n.Value, marker) {
					result = union(result, sources)
				}
			}
		case *syntax.SglQuoted:
			for marker, sources := range s.markers {
				if strings.Contains(n.Value, marker) {
					result = union(result, sources)
				}
			}
		case *syntax.ParamExp:
			if n.Exp != nil {
				switch n.Exp.Op.String() {
				case "+", ":+":
					// Alternate values reveal presence, not the parameter's contents.
					return true
				case "?", ":?":
					s.a.emit(Site{Kind: Log, Path: s.path, Line: int(n.Pos().Line())}, s.word(n.Exp.Word))
				}
			}
			if n.Param != nil {
				result = union(result, s.vars[n.Param.Value])
				if n.Excl {
					// Dynamic indirection can select any in-scope value.
					for _, sources := range s.vars {
						result = union(result, sources)
					}
				}
			}
		case *syntax.CmdSubst:
			child := *s
			child.vars = maps.Clone(s.vars)
			child.constants = maps.Clone(s.constants)
			child.constantValues = cloneConstantValues(s.constantValues)
			child.constantUnknown = maps.Clone(s.constantUnknown)
			result = union(result, child.stmts(n.Stmts, true, nil))
			return false
		}
		return true
	})
	return result
}

func (s *shell) assign(assign *syntax.Assign) {
	if assign.Name == nil {
		return
	}
	if assign.Value == nil && assign.Array == nil {
		return
	}
	assigned := s.word(assign.Value)
	s.vars[assign.Name.Value] = union(s.vars[assign.Name.Value], assigned)
	texts, known := wordTexts(assign.Value, s.constants, s.constantValues, s.constantUnknown)
	if known && len(assigned) == 0 && assign.Array == nil {
		s.setConstants(assign.Name.Value, texts, false)
	} else if len(assigned) == 0 {
		s.setConstants(assign.Name.Value, texts, true)
	} else {
		s.setConstants(assign.Name.Value, nil, true)
	}
	if assign.Array != nil {
		for _, elem := range assign.Array.Elems {
			s.vars[assign.Name.Value] = union(s.vars[assign.Name.Value], s.word(elem.Value))
		}
	}
}

func (s *shell) setConstants(name string, values []string, unknown bool) {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		unique[value] = struct{}{}
	}
	if len(unique) == 0 {
		delete(s.constantValues, name)
		delete(s.constants, name)
	} else {
		s.constantValues[name] = unique
		if len(unique) == 1 && !unknown {
			for value := range unique {
				s.constants[name] = value
			}
		} else {
			delete(s.constants, name)
		}
	}
	if unknown {
		s.constantUnknown[name] = true
	} else {
		delete(s.constantUnknown, name)
	}
}

func (s *shell) constantState() constantState {
	return constantState{
		values:  cloneConstantValues(s.constantValues),
		unknown: maps.Clone(s.constantUnknown),
	}
}

func (s *shell) restoreConstants(state constantState) {
	s.constantValues = cloneConstantValues(state.values)
	s.constantUnknown = maps.Clone(state.unknown)
	s.constants = make(map[string]string)
	for name, values := range state.values {
		if len(values) == 1 && !state.unknown[name] {
			for value := range values {
				s.constants[name] = value
			}
		}
	}
}

func mergeConstantStates(states ...constantState) constantState {
	merged := constantState{
		values: make(map[string]map[string]struct{}), unknown: make(map[string]bool),
	}
	names := make(map[string]struct{})
	for _, state := range states {
		for name := range state.values {
			names[name] = struct{}{}
		}
		for name := range state.unknown {
			names[name] = struct{}{}
		}
	}
	for name := range names {
		values := make(map[string]struct{})
		unknown := false
		for _, state := range states {
			current := state.values[name]
			if len(current) == 0 || state.unknown[name] {
				unknown = true
			}
			for value := range current {
				values[value] = struct{}{}
			}
		}
		if len(values) > 0 {
			merged.values[name] = values
		}
		if unknown {
			merged.unknown[name] = true
		}
	}
	return merged
}

func cloneConstantValues(values map[string]map[string]struct{}) map[string]map[string]struct{} {
	cloned := make(map[string]map[string]struct{}, len(values))
	for name, candidates := range values {
		cloned[name] = maps.Clone(candidates)
	}
	return cloned
}

func (s *shell) stmts(stmts []*syntax.Stmt, captured bool, stdin value) value {
	var result value
	for _, stmt := range stmts {
		result = union(result, s.stmt(stmt, captured, stdin))
	}
	return result
}

func (s *shell) stmt(stmt *syntax.Stmt, captured bool, stdin value) value {
	redirected := false
	for _, redir := range stmt.Redirs {
		switch redir.Op.String() {
		case ">", ">>", ">&", "&>", "&>>":
			redirected = redirected || redir.N == nil || redir.N.Value == "1" || strings.HasPrefix(redir.Op.String(), "&")
		case "<":
			stdin = union(stdin, s.readFileWord(redir.Word))
		case "<<", "<<-", "<<<": //nolint:generatedyamlheredoc // This analyzes authored heredocs; it does not generate shell.
			stdin = union(stdin, s.word(redir.Hdoc), s.word(redir.Word))
		}
	}
	out := s.command(stmt.Cmd, captured || redirected, stdin)
	for _, redir := range stmt.Redirs {
		s.redirectOutput(stmt, redir, out)
	}
	if redirected {
		return nil
	}
	return out
}

func (s *shell) redirectOutput(stmt *syntax.Stmt, redir *syntax.Redirect, out value) {
	switch redir.Op.String() {
	case ">", ">>", "&>", "&>>":
		if redir.N != nil && redir.N.Value != "1" && !strings.HasPrefix(redir.Op.String(), "&") {
			return
		}
		target := wordText(redir.Word, s.constants)
		if !specialTarget(target) {
			targets, known := s.paths(redir.Word)
			if !known && len(union(out, s.word(redir.Word))) > 0 {
				s.a.gapAt(s.path, int(redir.Pos().Line()), "dynamic file targets are not modeled")
			}
			for _, target := range targets {
				s.write(target, out, s.outputName(stmt.Cmd, target), int(stmt.Pos().Line()), stmt.Cmd)
			}
			return
		}
		payload := s.outputName(stmt.Cmd, target)
		if payload == "" {
			for _, source := range stmt.Redirs {
				if source.Hdoc != nil {
					payload = heredocOutputName(source.Hdoc, s.constants, s.markers)
					if target == "$GITHUB_OUTPUT" {
						payload = "" // A heredoc can contain several output records.
					}
					if payload != "" {
						break
					}
				}
			}
		}
		s.write(target, out, payload, int(stmt.Pos().Line()), stmt.Cmd)
	case ">&":
		if staticWord(redir.Word) == "2" {
			s.a.emit(Site{Kind: Log, Path: s.path, Line: int(stmt.Pos().Line())}, out)
		}
	}
}

func (s *shell) pathFor(path string) string {
	if specialTarget(path) || filepath.IsAbs(path) || s.workingDirectory == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(s.workingDirectory, path))
}

func (s *shell) paths(word *syntax.Word) ([]string, bool) {
	texts, known := wordTexts(word, s.constants, s.constantValues, s.constantUnknown)
	paths := make([]string, 0, len(texts))
	seen := make(map[string]struct{}, len(texts))
	for _, text := range texts {
		if text == "" || containsMarker(text, s.markers) {
			known = false
			continue
		}
		path := s.pathFor(text)
		if _, exists := seen[path]; !exists {
			paths = append(paths, path)
			seen[path] = struct{}{}
		}
	}
	return paths, known
}

func (s *shell) readPaths(pattern string) ([]string, bool) {
	if !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}, true
	}
	var matches []string
	for file := range s.files {
		matched, err := filepath.Match(filepath.ToSlash(pattern), filepath.ToSlash(file))
		if err != nil {
			return nil, false
		}
		if matched {
			matches = append(matches, file)
		}
	}
	slices.Sort(matches)
	return matches, true
}

func (s *shell) readFileWord(word *syntax.Word) value {
	targets, known := s.paths(word)
	pathSources := s.word(word)
	relevant := s.files.hasSources() || s.persisted.hasSources() || len(pathSources) > 0
	if !known && relevant {
		s.a.gapAt(s.path, int(word.Pos().Line()), "dynamic file targets are not modeled")
	}
	var sources value
	for _, target := range targets {
		files, modeled := s.readPaths(target)
		if !modeled && relevant {
			s.a.gapAt(s.path, int(word.Pos().Line()), "file patterns could not be modeled")
		}
		for _, file := range files {
			sources = union(sources, s.files[file])
		}
	}
	return sources
}

func (s *shell) command(command syntax.Command, captured bool, stdin value) value { //nolint:largefunc // Shell node cases share one stream and scope model.
	switch cmd := command.(type) {
	case *syntax.CallExpr:
		return s.call(cmd, captured, stdin)
	case *syntax.DeclClause:
		for _, assign := range cmd.Args {
			s.assign(assign)
		}
	case *syntax.Block:
		return s.stmts(cmd.Stmts, captured, stdin)
	case *syntax.Subshell:
		child := *s
		child.vars = maps.Clone(s.vars)
		child.constants = maps.Clone(s.constants)
		child.constantValues = cloneConstantValues(s.constantValues)
		child.constantUnknown = maps.Clone(s.constantUnknown)
		return child.stmts(cmd.Stmts, captured, stdin)
	case *syntax.BinaryCmd:
		if cmd.Op.String() == "|" || cmd.Op.String() == "|&" {
			trace := s.trace
			constants := s.constantState()
			left := s.stmt(cmd.X, true, stdin)
			s.trace = trace
			right := s.stmt(cmd.Y, captured, left)
			s.trace = trace
			s.restoreConstants(constants)
			return right
		}
		left := s.stmt(cmd.X, captured, stdin)
		leftTrace := s.trace
		leftConstants := s.constantState()
		s.restoreConstants(leftConstants)
		s.trace = leftTrace
		right := s.stmt(cmd.Y, captured, stdin)
		rightTrace := s.trace
		rightConstants := s.constantState()
		s.trace = leftTrace || rightTrace
		s.restoreConstants(mergeConstantStates(leftConstants, rightConstants))
		return union(left, right)
	case *syntax.IfClause:
		if len(cmd.Cond) == 0 {
			return s.stmts(cmd.Then, captured, stdin)
		}
		result := s.stmts(cmd.Cond, captured, stdin)
		trace := s.trace
		constants := s.constantState()
		then := s.stmts(cmd.Then, captured, stdin)
		thenTrace := s.trace
		thenConstants := s.constantState()
		s.restoreConstants(constants)
		s.trace = trace
		var otherwise value
		if cmd.Else != nil {
			otherwise = s.command(cmd.Else, captured, stdin)
		} else {
			s.restoreConstants(constants)
		}
		otherwiseConstants := s.constantState()
		s.trace = thenTrace || s.trace
		s.restoreConstants(mergeConstantStates(thenConstants, otherwiseConstants))
		result = union(result, then, otherwise)
		return result
	case *syntax.ForClause:
		if loop, ok := cmd.Loop.(*syntax.WordIter); ok {
			for _, item := range loop.Items {
				s.vars[loop.Name.Value] = union(s.vars[loop.Name.Value], s.word(item))
			}
		}
		constants := s.constantState()
		result := s.stmts(cmd.Do, captured, stdin)
		s.restoreConstants(mergeConstantStates(constants, s.constantState()))
		return result
	case *syntax.WhileClause:
		result := s.stmts(cmd.Cond, captured, stdin)
		constants := s.constantState()
		result = union(result, s.stmts(cmd.Do, captured, stdin))
		s.restoreConstants(mergeConstantStates(constants, s.constantState()))
		return result
	case *syntax.CaseClause:
		var result value
		constants := s.constantState()
		trace := s.trace
		traceAfterBranches := trace
		states := []constantState{constants}
		for _, item := range cmd.Items {
			s.trace = trace
			result = union(result, s.stmts(item.Stmts, captured, stdin))
			traceAfterBranches = traceAfterBranches || s.trace
			states = append(states, s.constantState())
			s.restoreConstants(constants)
		}
		s.trace = traceAfterBranches
		s.restoreConstants(mergeConstantStates(states...))
		return result
	case *syntax.FuncDecl:
		if cmd.Name != nil {
			s.functions[cmd.Name.Value] = cmd.Body
		}
	}
	return nil
}

func (s *shell) call(cmd *syntax.CallExpr, captured bool, stdin value) value {
	if len(cmd.Args) == 0 || len(cmd.Assigns) == 0 {
		return s.callCommand(cmd, captured, stdin)
	}
	child := *s
	child.vars = maps.Clone(s.vars)
	child.constants = maps.Clone(s.constants)
	child.constantValues = cloneConstantValues(s.constantValues)
	child.constantUnknown = maps.Clone(s.constantUnknown)
	child.expandedArgs = maps.Clone(s.expandedArgs)
	for _, arg := range cmd.Args[1:] {
		child.expandedArgs[arg] = s.word(arg)
	}
	for _, assign := range cmd.Assigns {
		child.assign(assign)
		if child.trace {
			child.a.emit(Site{Kind: Log, Path: child.path, Line: int(assign.Pos().Line())}, child.word(assign.Value))
		}
	}
	withoutAssignments := *cmd
	withoutAssignments.Assigns = nil
	out := child.callCommand(&withoutAssignments, captured, stdin)
	s.trace = child.trace
	return out
}

func (s *shell) callCommand(cmd *syntax.CallExpr, captured bool, stdin value) value { //nolint:largefunc // Keep command-specific stream semantics together.
	if len(cmd.Args) == 0 {
		for _, assign := range cmd.Assigns {
			if s.trace {
				s.a.emit(Site{Kind: Log, Path: s.path, Line: int(assign.Pos().Line())}, s.word(assign.Value))
			}
			s.assign(assign)
		}
		return nil
	}
	name := ""
	var args value
	for index, arg := range cmd.Args {
		if index == 0 {
			name = literalShellWord(arg)
		}
		args = union(args, s.word(arg))
	}
	line := int(cmd.Pos().Line())
	site := Site{Kind: Log, Path: s.path, Line: line}
	if s.trace {
		s.a.emit(site, args)
		for _, assign := range cmd.Assigns {
			s.a.emit(site, s.word(assign.Value))
		}
	}
	if body := s.functions[name]; body != nil {
		return s.function(name, body, cmd.Args[1:], captured, stdin)
	}
	if out, handled := s.javascriptCommand(cmd, captured, stdin); handled {
		return out
	}
	var out value
	switch name {
	case ":", "true", "false":
		// Expansions and xtrace are already evaluated; these discard their inputs.
	case "test", "[":
		if !safeShellPredicate(name, cmd.Args[1:]) {
			if len(args) > 0 {
				s.a.gapAt(s.path, line, "shell test diagnostics may expose secret values")
			}
		}
	case "echo", "printf":
		out = args
		if !captured && isMaskRegistration(cmd) {
			out = nil
		}
	case "env":
		out = s.env(cmd, stdin)
	case "printenv":
		if len(cmd.Args) == 1 {
			for _, sources := range s.vars {
				out = union(out, sources)
			}
		} else {
			for _, arg := range cmd.Args[1:] {
				out = union(out, s.vars[staticWord(arg)])
			}
		}
	case "cat", "head", "tail", "base64", "openssl", "jq", "sed", "awk", "tr", "cut", "tee":
		out = union(args, stdin)
		for _, arg := range cmd.Args[1:] {
			out = union(out, s.readFileWord(arg))
		}
		if name == "tee" {
			for _, arg := range cmd.Args[1:] {
				targets, known := s.paths(arg)
				if !known && len(stdin) > 0 {
					s.a.gapAt(s.path, line, "dynamic file targets are not modeled")
				}
				for _, target := range targets {
					if target != "" && !strings.HasPrefix(target, "-") {
						s.write(target, stdin, "", line, nil)
					}
				}
			}
		}
	case "curl", "wget":
		s.network(name, cmd.Args[1:], line, stdin)
	case "set":
		if len(cmd.Args) == 1 {
			for _, sources := range s.vars {
				out = union(out, sources)
			}
		}
		s.trace = applyTraceOptions(s.trace, cmd.Args[1:])
	default:
		if len(args)+len(stdin) > 0 {
			shellLog.Printf("Unmodeled command at %s:%d (tainted arguments=%t, tainted stdin=%t)", s.path, line, len(args) > 0, len(stdin) > 0)
			s.a.gapAt(s.path, line, "external command data flow is not modeled")
		}
	}
	if !captured {
		s.a.emit(site, out)
	}
	return out
}

func (s *shell) env(cmd *syntax.CallExpr, stdin value) value {
	child := *s
	child.vars = maps.Clone(s.vars)
	child.constants = maps.Clone(s.constants)
	child.constantValues = cloneConstantValues(s.constantValues)
	child.constantUnknown = maps.Clone(s.constantUnknown)
	args := cmd.Args[1:]
	commandIndex := 0
	for commandIndex < len(args) {
		word, ok := sliceAt(args, commandIndex)
		if !ok {
			break
		}
		text := staticWord(word)
		switch text {
		case "-i", "--ignore-environment":
			clear(child.vars)
			clear(child.constants)
			clear(child.constantValues)
			clear(child.constantUnknown)
		case "-u", "--unset":
			commandIndex = unsetEnvironmentVariable(&child, args, commandIndex)
		case "--":
			commandIndex++
			goto command
		default:
			if name, _, ok := strings.Cut(text, "="); ok && name != "" {
				child.setEnvironmentArgument(name, word)
			} else if strings.HasPrefix(text, "-") {
				s.a.gap(s.path, "env options are not fully modeled")
			} else {
				goto command
			}
		}
		commandIndex++
	}
	return child.environmentOutput()

command:
	if commandIndex >= len(args) {
		return child.environmentOutput()
	}
	commandArgs := &syntax.CallExpr{Args: args[commandIndex:]}
	child.expandedArgs = maps.Clone(s.expandedArgs)
	for _, arg := range commandArgs.Args[1:] {
		child.expandedArgs[arg] = s.word(arg)
	}
	commandWord, ok := sliceAt(commandArgs.Args, 0)
	if ok && !knownShellCommand(literalShellWord(commandWord)) {
		for _, sources := range child.vars {
			if len(sources) > 0 {
				s.a.gap(s.path, "env command environment data flow is not modeled")
				break
			}
		}
	}
	return child.call(commandArgs, true, stdin)
}

func unsetEnvironmentVariable(child *shell, args []*syntax.Word, index int) int {
	index++
	if nameWord, ok := sliceAt(args, index); ok {
		name := staticWord(nameWord)
		delete(child.vars, name)
		delete(child.constants, name)
		delete(child.constantValues, name)
		delete(child.constantUnknown, name)
	}
	return index
}

func (s *shell) setEnvironmentArgument(name string, word *syntax.Word) {
	s.vars[name] = s.word(word)
	literals, known := wordTexts(word, s.constants, s.constantValues, s.constantUnknown)
	values := make([]string, 0, len(literals))
	for _, literal := range literals {
		if _, value, found := strings.Cut(literal, "="); found {
			values = append(values, value)
		}
	}
	if len(s.vars[name]) > 0 {
		s.setConstants(name, nil, true)
	} else if known {
		s.setConstants(name, values, false)
	} else {
		s.setConstants(name, literals, true)
	}
}

func (s *shell) environmentOutput() value {
	var out value
	for _, sources := range s.vars {
		out = union(out, sources)
	}
	return out
}

func (s *shell) network(command string, args []*syntax.Word, line int, stdin value) {
	var request value
	for index := 0; index < len(args); index++ {
		word, ok := sliceAt(args, index)
		if !ok {
			continue
		}
		arg := staticWord(word)
		if arg == "-H" || arg == "--header" {
			if header, ok := sliceAt(args, index+1); ok {
				index++
				if !authenticationHeader(staticWord(header)) {
					request = union(request, s.networkHeader(command, header, stdin, ""))
				}
			}
			continue
		}
		if header, found := strings.CutPrefix(arg, "--header="); found {
			if !authenticationHeader(header) {
				request = union(request, s.networkHeader(command, word, stdin, "--header="))
			}
			continue
		}
		if arg == "-u" || arg == "--user" || arg == "--oauth2-bearer" ||
			arg == "--password" || arg == "--http-password" {
			index++
			continue
		}
		if arg == "--post-file" || arg == "-T" || arg == "--upload-file" {
			if file, ok := sliceAt(args, index+1); ok {
				index++
				request = union(request, s.networkFileBody(file, stdin, arg != "--post-file", ""))
			}
			continue
		}
		if option, _, found := strings.Cut(arg, "="); found && (option == "--post-file" || option == "--upload-file") {
			request = union(request, s.networkFileBody(word, stdin, option != "--post-file", option+"="))
			continue
		}
		if strings.HasPrefix(arg, "--user=") || strings.HasPrefix(arg, "--oauth2-bearer=") {
			continue
		}
		if arg == "-d" || arg == "--data" || arg == "--data-binary" ||
			arg == "--data-urlencode" || arg == "--json" || arg == "-F" || arg == "--form" ||
			arg == "--data-raw" || arg == "--form-string" {
			if body, ok := sliceAt(args, index+1); ok {
				index++
				request = union(request, s.networkBody(body, stdin, arg, ""))
			}
			continue
		}
		if option, _, found := strings.Cut(arg, "="); found {
			switch option {
			case "--data", "--data-binary", "--data-urlencode", "--json", "--form", "--data-raw", "--form-string":
				request = union(request, s.networkBody(word, stdin, option, option+"="))
				continue
			}
		}
		request = union(request, s.word(word))
	}
	s.a.emit(Site{Kind: Network, Path: s.path, Line: line}, request)
}

func (s *shell) networkHeader(command string, word *syntax.Word, stdin value, prefix string) value {
	if command == "curl" {
		return s.networkBody(word, stdin, "--header", prefix)
	}
	return s.word(word)
}

func (s *shell) networkBody(word *syntax.Word, stdin value, option, prefix string) value {
	direct := s.word(word)
	contents := direct
	if option == "--data-raw" || option == "--form-string" {
		return contents
	}
	texts, known := wordTexts(word, s.constants, s.constantValues, s.constantUnknown)
	for _, text := range texts {
		text = strings.TrimPrefix(text, prefix)
		if containsMarker(text, s.markers) {
			known = false
			continue
		}
		var targets []string
		switch option {
		case "-F", "--form":
			_, body, found := strings.Cut(text, "=")
			if found {
				if target, upload := strings.CutPrefix(body, "@"); upload {
					targets = strings.Split(target, ",")
				} else if target, file := strings.CutPrefix(body, "<"); file {
					targets = []string{target}
				}
				for index, target := range targets {
					targets[index], _, _ = strings.Cut(target, ";")
				}
			}
		case "--data-urlencode":
			// An '=' before '@' selects literal content, not a filename.
			if name, target, found := strings.Cut(text, "@"); found && !strings.Contains(name, "=") {
				targets = []string{target}
			}
		default:
			if target, found := strings.CutPrefix(text, "@"); found {
				targets = []string{target}
			}
		}
		for _, target := range targets {
			// Curl's quoted multipart filenames need their own escaping parser.
			if (option == "-F" || option == "--form") && strings.ContainsAny(target, "\"\\") {
				s.a.gap(s.path, "quoted multipart upload files are not modeled")
				continue
			}
			if target == "-" {
				contents = union(contents, stdin)
				continue
			}
			contents = union(contents, s.networkFileBodyValue(target, stdin, false))
		}
	}
	if !known && ((len(direct) == 0 && (s.files.hasSources() || len(stdin) > 0)) || strings.Contains(staticWord(word), "@")) {
		s.a.gap(s.path, "dynamic network upload files are not modeled")
	}
	return contents
}

func (s *shell) networkFileBody(word *syntax.Word, stdin value, stdinUpload bool, prefix string) value {
	texts, known := wordTexts(word, s.constants, s.constantValues, s.constantUnknown)
	var contents value
	for _, text := range texts {
		if containsMarker(text, s.markers) {
			known = false
			continue
		}
		contents = union(contents, s.networkFileBodyValue(strings.TrimPrefix(text, prefix), stdin, stdinUpload))
	}
	if !known {
		s.a.gap(s.path, "dynamic network upload files are not modeled")
	}
	return contents
}

func (s *shell) networkFileBodyValue(path string, stdin value, stdinUpload bool) value {
	// Curl accepts "-" and "." as stdin; wget --post-file requires a regular file.
	if stdinUpload && (path == "-" || path == ".") {
		return stdin
	}
	if path == "" {
		return nil
	}
	files, modeled := s.readPaths(s.pathFor(path))
	if !modeled {
		s.a.gap(s.path, "dynamic network upload files are not modeled")
	}
	var contents value
	for _, file := range files {
		contents = union(contents, s.files[file])
	}
	return contents
}

func knownShellCommand(name string) bool {
	switch name {
	case "echo", "printf", "env", "printenv", "cat", "head", "tail", "base64", "openssl",
		"jq", "sed", "awk", "tr", "cut", "tee", "curl", "wget", "set", "true", "false", ":", "test", "[":
		return true
	default:
		return false
	}
}

func isMaskRegistration(cmd *syntax.CallExpr) bool {
	if len(cmd.Args) < 2 {
		return false
	}
	command, ok := sliceAt(cmd.Args, 0)
	if !ok {
		return false
	}
	switch staticWord(command) {
	case "echo":
		argument, ok := sliceAt(cmd.Args, 1)
		if !ok {
			return false
		}
		text := staticWord(argument)
		return len(cmd.Args) == 2 && strings.HasPrefix(text, "::add-mask::") &&
			!strings.ContainsAny(text, "\r\n")
	case "printf":
		if len(cmd.Args) != 3 {
			return false
		}
		formatWord, ok := sliceAt(cmd.Args, 1)
		if !ok {
			return false
		}
		valueWord, ok := sliceAt(cmd.Args, 2)
		if !ok {
			return false
		}
		format := staticWord(formatWord)
		return (format == "::add-mask::%s" || format == "::add-mask::%s\\n") &&
			!strings.ContainsAny(staticWord(valueWord), "\r\n")
	default:
		return false
	}
}

func shellInvocationTraces(shellName string) bool {
	fields := strings.Fields(shellName)
	tracing := false
	for index := 1; index < len(fields); index++ {
		flag, ok := sliceAt(fields, index)
		if !ok {
			break
		}
		if flag == "-o" || flag == "+o" {
			if next, ok := sliceAt(fields, index+1); ok && next == "xtrace" {
				tracing = flag == "-o"
				index++
			}
		} else if strings.HasPrefix(flag, "-") && strings.Contains(flag[1:], "x") {
			tracing = true
		} else if strings.HasPrefix(flag, "+") && strings.Contains(flag[1:], "x") {
			tracing = false
		}
	}
	return tracing
}

func applyTraceOptions(current bool, args []*syntax.Word) bool {
	for index := 0; index < len(args); index++ {
		word, ok := sliceAt(args, index)
		if !ok {
			break
		}
		flag := staticWord(word)
		if flag == "-o" || flag == "+o" {
			if next, ok := sliceAt(args, index+1); ok && staticWord(next) == "xtrace" {
				current = flag == "-o"
			}
			index++
		} else if strings.HasPrefix(flag, "-") && strings.Contains(flag[1:], "x") {
			current = true
		} else if strings.HasPrefix(flag, "+") && strings.Contains(flag[1:], "x") {
			current = false
		}
	}
	return current
}

func specialTarget(target string) bool {
	return target == "$GITHUB_STEP_SUMMARY" || target == "$GITHUB_OUTPUT" || target == "$GITHUB_ENV"
}

func wordTexts(word *syntax.Word, constants map[string]string, alternatives map[string]map[string]struct{}, unknownValues map[string]bool) ([]string, bool) {
	if word == nil {
		return nil, false
	}
	known := true
	texts := []string{""}
	appendValue := func(values []string) {
		if len(texts) > 0 && len(values) > 64/len(texts) {
			known = false
			texts = nil
			return
		}
		combined := make([]string, 0, len(texts)*len(values))
		for _, prefix := range texts {
			for _, value := range values {
				combined = append(combined, prefix+value)
			}
		}
		texts = combined
	}
	syntax.Walk(word, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Lit:
			appendValue([]string{n.Value})
		case *syntax.SglQuoted:
			appendValue([]string{n.Value})
		case *syntax.ParamExp:
			if n.Param == nil {
				known = false
				return false
			}
			name := n.Param.Value
			var candidates []string
			for value := range alternatives[name] {
				candidates = append(candidates, value)
			}
			if len(candidates) == 0 {
				if value, ok := constants[name]; ok {
					candidates = []string{value}
				} else {
					known = false
					texts = nil
					return false
				}
			}
			if unknownValues[name] || n.Excl {
				known = false
			}
			slices.Sort(candidates)
			appendValue(candidates)
			return false
		case *syntax.CmdSubst:
			known = false
			texts = nil
			return false
		}
		return known
	})
	return texts, known
}

func sliceAt[T any](values []T, index int) (T, bool) {
	if index >= 0 && index < len(values) {
		return values[index], true
	}
	var zero T
	return zero, false
}

func commandOutputName(command syntax.Command, markers scope) string {
	switch cmd := command.(type) {
	case *syntax.CallExpr:
		if len(cmd.Args) < 2 {
			return ""
		}
		nameWord, ok := sliceAt(cmd.Args, 0)
		if !ok {
			return ""
		}
		name := staticWord(nameWord)
		if name != "echo" && name != "printf" {
			return ""
		}
		for _, arg := range cmd.Args[1:] {
			text := staticWord(arg)
			key, _, found := strings.Cut(text, "=")
			if !found {
				key, _, found = strings.Cut(text, "<<")
			}
			if found {
				key = strings.TrimSpace(key)
				if strings.ContainsAny(key, "$%") || containsMarker(key, markers) {
					return ""
				}
				return key + "="
			}
		}
	case *syntax.Block:
		for _, stmt := range cmd.Stmts {
			if name := commandOutputName(stmt.Cmd, markers); name != "" {
				return name
			}
		}
	case *syntax.BinaryCmd:
		if cmd.X != nil {
			if name := commandOutputName(cmd.X.Cmd, markers); name != "" {
				return name
			}
		}
		if cmd.Y != nil {
			return commandOutputName(cmd.Y.Cmd, markers)
		}
	}
	return ""
}

func (s *shell) outputName(command syntax.Command, target string) string {
	if target != "$GITHUB_OUTPUT" {
		return commandOutputName(command, s.markers)
	}
	// Compound output may contain several distinct records. Track the union
	// under a wildcard rather than treating its first name as the entire write.
	call, ok := command.(*syntax.CallExpr)
	if !ok || len(call.Args) < 2 {
		return ""
	}
	commandWord, _ := sliceAt(call.Args, 0)
	first, _ := sliceAt(call.Args, 1)
	name := literalShellWord(commandWord)
	if name == "echo" && len(call.Args) == 2 && singleOutputWord(first) {
		return literalOutputName(first, s.markers)
	}
	if name == "printf" && len(call.Args) == 3 {
		second, _ := sliceAt(call.Args, 2)
		format := strings.TrimSuffix(literalShellWord(first), `\n`)
		if format == "%s" && singleOutputWord(second) {
			return literalOutputName(second, s.markers)
		}
		if key, ok := strings.CutSuffix(format, "=%s"); ok && outputIdentifier.MatchString(key) && !containsMarker(key, s.markers) && singleOutputWord(second) {
			return key + "="
		}
	}
	return ""
}

func singleOutputWord(word *syntax.Word) bool {
	if strings.ContainsAny(staticWord(word), "\r\n\\") {
		return false
	}
	known := true
	syntax.Walk(word, func(node syntax.Node) bool {
		if quoted, ok := node.(*syntax.SglQuoted); ok && quoted.Dollar {
			// ANSI-C escapes are raw in the AST and may introduce extra records
			// or change names. Do not infer transport names without decoding them.
			known = false
		}
		return known
	})
	return known
}

func (s *shell) outputMayBeMultiline(command syntax.Command) bool {
	call, ok := command.(*syntax.CallExpr)
	if !ok || len(call.Args) < 2 {
		return true
	}
	commandWord, exists := sliceAt(call.Args, 0)
	if !exists {
		return true
	}
	name := literalShellWord(commandWord)
	wordIndex := 1
	if name == "printf" {
		wordIndex = 2
	}
	word, exists := sliceAt(call.Args, wordIndex)
	if !exists {
		return true
	}
	uncertain := false
	syntax.Walk(word, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst:
			uncertain = true
		case *syntax.Lit:
			uncertain = uncertain || strings.ContainsAny(n.Value, "\\\r\n")
		case *syntax.SglQuoted:
			uncertain = uncertain || n.Dollar || strings.ContainsAny(n.Value, "\\\r\n")
		case *syntax.Word, *syntax.DblQuoted:
		default:
			uncertain = true
		}
		return !uncertain
	})
	return uncertain
}

func (s *shell) outputRecordComplete(command syntax.Command) bool {
	call, ok := command.(*syntax.CallExpr)
	if !ok || len(call.Args) < 2 {
		return false
	}
	commandWord, exists := sliceAt(call.Args, 0)
	if !exists {
		return false
	}
	switch literalShellWord(commandWord) {
	case "echo":
		return true
	case "printf":
		format, exists := sliceAt(call.Args, 1)
		if !exists {
			return false
		}
		texts, known := wordTexts(format, s.constants, s.constantValues, s.constantUnknown)
		text, hasText := sliceAt(texts, 0)
		return known && hasText && len(texts) == 1 && strings.HasSuffix(text, `\n`)
	default:
		return false
	}
}

// The name must precede every expansion. wordText alone is insufficient:
// command substitutions are deliberately omitted from its diagnostic text.
func literalOutputName(word *syntax.Word, markers scope) string {
	var prefix strings.Builder
	known, complete := true, false
	syntax.Walk(word, func(node syntax.Node) bool {
		if node == nil || !known || complete {
			return false
		}
		switch n := node.(type) {
		case *syntax.Word, *syntax.DblQuoted:
			return true
		case *syntax.Lit:
			prefix.WriteString(n.Value)
		case *syntax.SglQuoted:
			prefix.WriteString(n.Value)
		default:
			known = false
			return false
		}
		text := prefix.String()
		name, _, found := strings.Cut(text, "=")
		if !found {
			name, _, found = strings.Cut(text, "<<")
		}
		if found {
			complete = true
			prefix.Reset()
			prefix.WriteString(name)
		}
		return false
	})
	name := prefix.String()
	if !known || !complete || containsMarker(name, markers) || !outputIdentifier.MatchString(name) {
		return ""
	}
	return name + "="
}

func containsMarker(text string, markers scope) bool {
	for marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func heredocOutputName(word *syntax.Word, constants map[string]string, markers scope) string {
	text := wordText(word, constants)
	text, _, _ = strings.Cut(text, "\n")
	key, _, found := strings.Cut(text, "=")
	if !found {
		key, _, found = strings.Cut(text, "<<")
	}
	if !found {
		return ""
	}
	key = strings.TrimSpace(key)
	if strings.ContainsAny(key, "$%") || containsMarker(key, markers) {
		return ""
	}
	return key + "="
}

func authenticationHeader(header string) bool {
	name, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(header)), ":")
	switch name {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "x-goog-api-key", "private-token":
		return true
	default:
		return false
	}
}

func (s *shell) function(name string, body *syntax.Stmt, args []*syntax.Word, captured bool, stdin value) value {
	if s.active[name] {
		s.a.gap(s.path, "recursive shell function calls are not modeled")
		return nil
	}
	arguments := make([]value, 0, len(args))
	var aggregate value
	for _, arg := range args {
		sources := s.word(arg)
		arguments = append(arguments, sources)
		aggregate = union(aggregate, sources)
	}
	previous := maps.Clone(s.vars)
	for index, sources := range arguments {
		s.vars[strconv.Itoa(index+1)] = sources
	}
	s.vars["@"] = aggregate
	s.vars["*"] = aggregate
	s.active[name] = true
	out := s.stmt(body, captured, stdin)
	delete(s.active, name)
	for key := range s.vars {
		if key == "@" || key == "*" || isPositional(key) {
			delete(s.vars, key)
		}
	}
	for key, sources := range previous {
		if key == "@" || key == "*" || isPositional(key) {
			s.vars[key] = sources
		}
	}
	return out
}

func isPositional(name string) bool {
	_, err := strconv.Atoi(name)
	return err == nil
}

func staticWord(word *syntax.Word) string {
	return wordText(word, nil)
}

func wordText(word *syntax.Word, constants map[string]string) string {
	if word == nil {
		return ""
	}
	var text strings.Builder
	syntax.Walk(word, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Lit:
			text.WriteString(n.Value)
		case *syntax.SglQuoted:
			text.WriteString(n.Value)
		case *syntax.ParamExp:
			if n.Param != nil {
				if literal, exists := constants[n.Param.Value]; exists {
					text.WriteString(literal)
				} else {
					text.WriteString("$" + n.Param.Value)
				}
			}
			return false
		case *syntax.CmdSubst:
			return false
		}
		return true
	})
	return text.String()
}

func (s *shell) write(target string, sources value, payload string, line int, command syntax.Command) {
	site := Site{Path: s.path, Line: line}
	switch target {
	case "$GITHUB_STEP_SUMMARY":
		site.Kind = Summary
		s.a.emit(site, sources)
	case "$GITHUB_OUTPUT", "$GITHUB_ENV":
		name, _, found := strings.Cut(payload, "=")
		if !found {
			name, _, _ = strings.Cut(payload, "<<")
		}
		name = strings.TrimSpace(name)
		if target == "$GITHUB_OUTPUT" {
			site.Kind = Output
			s.files[target] = union(s.files[target], sources)
			s.a.transportOutputRecord(site, name, sources, s.a.stepOutput, s.outputMayBeMultiline(command), s.outputRecordComplete(command))
		} else if name != "" {
			key := "env." + strings.ToLower(name)
			s.persisted[key] = union(s.persisted[key], sources)
			s.persisted["shell."+name] = union(s.persisted["shell."+name], sources)
		} else if len(sources) > 0 {
			s.a.gap(s.path, "dynamic GITHUB_ENV names are not modeled")
		}
	case "", "/dev/null":
	default:
		s.files[target] = union(s.files[target], sources)
	}
}

func artifactIncludes(patterns []string, file string) bool {
	included := false
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		pattern, excluded := strings.CutPrefix(pattern, "!")
		if artifactPatternMatches(pattern, file) {
			included = !excluded
		}
	}
	return included
}

func artifactPatternMatches(pattern, file string) bool {
	pattern = filepath.ToSlash(strings.TrimSuffix(pattern, "/"))
	file = filepath.ToSlash(filepath.Clean(file))
	if pattern == "" || pattern == "." || pattern == "**" {
		return true
	}
	patternParts := strings.Split(pattern, "/")
	fileParts := strings.Split(file, "/")
	type position struct{ pattern, file int }
	memo := make(map[position]bool)
	visited := make(map[position]bool)
	var match func(int, int) bool
	match = func(patternIndex, fileIndex int) bool {
		current := position{pattern: patternIndex, file: fileIndex}
		if visited[current] {
			return memo[current]
		}
		visited[current] = true
		patternPart, ok := sliceAt(patternParts, patternIndex)
		if !ok {
			memo[current] = true
			return memo[current]
		}
		if patternPart == "**" {
			memo[current] = match(patternIndex+1, fileIndex) ||
				fileIndex < len(fileParts) && match(patternIndex, fileIndex+1)
			return memo[current]
		}
		filePart, ok := sliceAt(fileParts, fileIndex)
		if !ok {
			return false
		}
		matched, err := filepath.Match(patternPart, filePart)
		memo[current] = err == nil && matched && match(patternIndex+1, fileIndex+1)
		return memo[current]
	}
	return match(0, 0)
}
