package taint

import (
	"fmt"
	"maps"
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
