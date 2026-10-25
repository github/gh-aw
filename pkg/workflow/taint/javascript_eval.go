package taint

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"path"
	"strconv"
	"strings"
)

type javascript struct {
	a              *analyzer
	input          JavaScriptInput
	options        *JavaScriptOptions
	env            map[string]jsValue
	runtimeGlobals map[string]jsValue
	stdin          value
	stdout         value
	files          map[string][]Source
	file           string
	modules        map[string]jsValue
	loading        map[string]struct{}
	active         map[*jsFunction]bool
	returns        jsValue
	fastVerified   bool
	budget         int
	failure        error
	catchDepth     int
	summaryDepth   int
	thrown         value
}

func newJavaScript(a *analyzer, input JavaScriptInput, options *JavaScriptOptions) *javascript {
	j := &javascript{
		a: a, input: input, options: options, stdin: input.Stdin,
		file: input.Name, files: maps.Clone(input.Files), budget: 100000,
		modules: make(map[string]jsValue), loading: make(map[string]struct{}), active: make(map[*jsFunction]bool),
	}
	if j.files == nil {
		j.files = make(map[string][]Source)
	}
	j.runtimeGlobals = j.initialGlobals()
	j.env = j.globals()
	return j
}

func (j *javascript) globals() map[string]jsValue {
	env := maps.Clone(j.runtimeGlobals)
	env["module"] = jsValue{fields: map[string]jsValue{"exports": {fields: make(map[string]jsValue)}}}
	env["exports"] = env["module"].fields["exports"]
	return env
}

func (j *javascript) initialGlobals() map[string]jsValue {
	env := make(map[string]jsValue)
	for _, name := range []string{"process", "console", "JSON", "Buffer", "String", "Boolean", "Number", "Object", "Array", "Error", "TypeError", "RangeError", "SyntaxError", "require", "fetch", "core", "github", "context", "exec"} {
		env[name] = jsValue{ref: name}
	}
	env["global"] = jsValue{fields: make(map[string]jsValue)}
	processEnv := jsValue{ref: "process.env", fields: make(map[string]jsValue)}
	for name, sources := range j.input.Env {
		processEnv.fields[name] = jsValue{sources: sources, kind: jsString}
	}
	for prefix := range j.optionsRuntimePaths() {
		if suffix, ok := strings.CutPrefix(prefix, "${"); ok {
			if name, _, ok := strings.Cut(suffix, "}"); ok && len(j.input.Env[name]) == 0 {
				processEnv.fields[name] = jsStringLiteral("${" + name + "}")
			}
		}
	}
	argv := jsValue{ref: "process.argv", fields: make(map[string]jsValue)}
	offset := j.input.ArgvOffset
	if offset == 0 {
		offset = 2
	}
	for i, sources := range j.input.Args {
		argv.fields[jsIndex(i+offset)] = jsValue{sources: sources, kind: jsString}
	}
	env["process"] = jsValue{ref: "process", fields: map[string]jsValue{"env": processEnv, "argv": argv}}
	return env
}

func jsIndex(i int) string { return strconv.Itoa(i) }

func (j *javascript) run() error {
	if j.options != nil && j.options.fastEntries != nil && !j.options.fastEntries[j.input.Source] {
		return nil
	}
	if j.options != nil && j.options.fastMatched != nil {
		j.recordFastEntry()
	}
	if j.a.rule.ID == "" || j.a.rule.IsSource == nil || j.a.rule.IsSink == nil {
		return errors.New("JavaScript taint rule requires an id, sources, and sinks")
	}
	if j.options != nil && j.options.Mode != "" && j.options.Mode != JavaScriptFast && j.options.Mode != JavaScriptDeep {
		return errors.New("JavaScript analysis mode must be fast or deep")
	}
	if j.options == nil || j.options.Parser == nil {
		j.gap(nil, "JavaScript analysis unavailable; configure the pinned native TypeScript compiler module")
		return nil
	}
	tree, err := j.parse(j.input.Source)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		j.gap(nil, "JavaScript AST unavailable; check syntax, Node.js, and the pinned native TypeScript compiler module")
		return nil
	}
	if tree == nil || tree.Kind != "SourceFile" {
		j.gap(nil, "JavaScript parser returned an invalid AST")
		return nil
	}
	j.eval(tree)
	var transferred value
	transferred = union(transferred, j.stdout)
	for _, sources := range j.files {
		transferred = union(transferred, sources)
	}
	j.markUnverifiedOutput(transferred, nil)
	return j.failure
}

func (j *javascript) markUnverifiedOutput(sources value, node *JavaScriptNode) {
	if len(sources) == 0 || j.a.stepOutput == nil || j.options == nil ||
		j.options.fastMatched == nil || j.options.fastEntries != nil || j.fastVerified ||
		!j.a.stepOutput.modeled {
		return
	}
	j.a.stepOutput.modeled = false
	j.gap(node, "authored JavaScript output has no verified compiled entry; downstream output transport is not fully modeled")
}

func (j *javascript) parse(source string) (*JavaScriptNode, error) {
	ctx := j.options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return j.options.Parser.Parse(ctx, source)
}

func (j *javascript) gap(n *JavaScriptNode, reason string) {
	line := 0
	if n != nil {
		line = n.Line
	}
	j.a.gapAt(j.scriptPath(), line, reason)
}

func (j *javascript) emit(n *JavaScriptNode, kind Kind, sources value) {
	j.a.emit(Site{Kind: kind, Path: j.scriptPath(), Line: n.Line}, sources)
}

func (j *javascript) scriptPath() string {
	if j.file != j.input.Name && fs.ValidPath(j.file) {
		return j.input.Name + ".javascript[" + j.file + "]"
	}
	return j.input.Name + ".javascript"
}

func (j *javascript) key(n *JavaScriptNode) (string, bool) {
	v := j.eval(n)
	if v.literal {
		return v.text, true
	}
	j.gap(n, "dynamic JavaScript property access is not modeled")
	return "*", false
}

func (j *javascript) eval(n *JavaScriptNode) jsValue { //nolint:largefunc // AST dispatch keeps conservative fallback visible.
	if n == nil {
		return jsValue{}
	}
	if j.options != nil && j.options.Context != nil && j.options.Context.Err() != nil {
		j.failure = j.options.Context.Err()
		return jsValue{}
	}
	j.budget--
	if j.budget < 0 {
		j.gap(n, "JavaScript analysis budget exhausted")
		return jsValue{}
	}
	if j.a.rule.IsJavaScriptFilter != nil && j.a.rule.IsJavaScriptFilter(n) {
		// Evaluate children first: declassification must not hide side effects.
		j.children(n)
		return jsValue{}
	}
	switch n.Kind {
	case "SourceFile":
		// CommonJS declarations are module-local, even when they shadow a
		// runtime global whose object is shared with other modules.
		for name := range jsLocalNames(&JavaScriptNode{Body: n}) {
			delete(j.env, name)
		}
		fallthrough
	case "Block", "VariableDeclarationList", "CaseBlock", "CaseClause", "DefaultClause":
		for _, child := range n.Statements {
			if child.Kind == "FunctionDeclaration" && child.Name != nil {
				j.env[child.Name.Text] = j.functionValue(child)
			}
		}
		return j.children(n)
	case "VariableStatement":
		return j.eval(n.DeclarationList)
	case "VariableDeclaration":
		v := j.eval(n.Initializer)
		j.bind(n.Name, v)
		return v
	case "Identifier":
		if n.Text == "__dirname" {
			return jsStringLiteral(path.Dir(j.file))
		}
		if n.Text == "__filename" {
			return jsStringLiteral(j.file)
		}
		v := j.env[n.Text]
		v.sources = union(v.sources, j.a.source(n.Text, j.input.Name, nil))
		v.sources = union(v.sources, j.input.Literals[n.Text])
		return v
	case "NumericLiteral":
		return jsValue{text: n.Text, literal: true, kind: jsNumber}
	case "RegularExpressionLiteral":
		return jsValue{text: n.Text, literal: true, kind: jsRegExp}
	case "StringLiteral", "NoSubstitutionTemplateLiteral", "TemplateHead", "TemplateMiddle", "TemplateTail":
		v := jsStringLiteral(n.Text)
		for marker, sources := range j.input.Literals {
			if strings.Contains(n.Text, marker) {
				v.sources = union(v.sources, sources)
				v.literal = false
				v.lines, v.mask = 0, jsNotMask
			}
		}
		return v
	case "TrueKeyword", "FalseKeyword", "NullKeyword", "ThisKeyword", "EndOfFileToken", "EndOfFile":
		return jsValue{}
	case "PropertyAccessExpression", "ElementAccessExpression":
		base := j.eval(n.Expression)
		key := jsName(n.Name)
		if n.Kind == "ElementAccessExpression" {
			var known bool
			key, known = j.key(n.ArgumentExpression)
			if !known {
				return jsValue{}
			}
		}
		v := base.property(key)
		if base.ref == "process.env" {
			v.kind = jsString
		}
		if v.ref != "" {
			v.sources = union(v.sources, j.a.source(v.ref, j.input.Name, nil))
		}
		// Opaque JSON/object sources taint all projected members, even if
		// the member itself is not explicitly represented.
		return v
	case "ObjectLiteralExpression":
		v := jsValue{fields: make(map[string]jsValue)}
		for _, property := range n.Properties {
			switch property.Kind {
			case "PropertyAssignment":
				key := jsName(property.Name)
				if property.Name != nil && property.Name.Kind == "ComputedPropertyName" {
					key, _ = j.key(property.Name.Expression)
				}
				v.fields[key] = j.eval(property.Initializer)
			case "ShorthandPropertyAssignment":
				v.fields[jsName(property.Name)] = j.eval(property.Name)
			case "SpreadAssignment":
				spread := j.eval(property.Expression)
				v.sources = union(v.sources, spread.sources)
				maps.Copy(v.fields, spread.fields)
			case "MethodDeclaration":
				v.fields[jsName(property.Name)] = j.functionValue(property)
			default:
				v.sources = union(v.sources, j.eval(property).taint())
				j.gap(property, "JavaScript object member is not modeled")
			}
		}
		return v
	case "ArrayLiteralExpression":
		v := jsValue{fields: make(map[string]jsValue), kind: jsArray}
		for i, item := range n.Elements {
			v.fields[jsIndex(i)] = j.eval(item)
		}
		return v
	case "FunctionDeclaration", "FunctionExpression", "ArrowFunction", "MethodDeclaration":
		return j.functionValue(n)
	case "CallExpression", "NewExpression":
		return j.call(n)
	case "AwaitExpression", "ParenthesizedExpression", "AsExpression", "NonNullExpression", "ExpressionStatement", "SpreadElement":
		return j.eval(n.Expression)
	case "ReturnStatement":
		v := j.eval(n.Expression)
		j.returns = jsUnionPreserving(j.returns, v)
		return v
	case "BinaryExpression":
		left := j.eval(n.Left)
		right := j.eval(n.Right)
		op := ""
		if n.OperatorToken != nil {
			op = n.OperatorToken.Kind
		}
		switch op {
		case "EqualsToken", "PlusEqualsToken", "BarBarEqualsToken", "QuestionQuestionEqualsToken":
			v := right
			if op == "PlusEqualsToken" && (left.kind == jsString || right.kind == jsString) {
				v = jsConcatStrings(left, right)
			} else if op != "EqualsToken" {
				v = jsUnionPreserving(left, right)
			}
			j.bind(n.Left, v)
			return v
		case "EqualsEqualsToken", "EqualsEqualsEqualsToken", "ExclamationEqualsToken", "ExclamationEqualsEqualsToken",
			"LessThanToken", "GreaterThanToken", "LessThanEqualsToken", "GreaterThanEqualsToken", "InKeyword", "InstanceOfKeyword":
			return jsValue{}
		case "PlusToken":
			if left.kind == jsString || right.kind == jsString {
				return jsConcatStrings(left, right)
			}
		}
		return jsUnion(left, right)
	case "PrefixUnaryExpression", "TypeOfExpression", "VoidExpression", "DeleteExpression":
		v := j.children(n)
		// Prefix unary operators are carried separately by the bridge.
		if n.Text == "!" || n.Kind == "TypeOfExpression" || n.Kind == "VoidExpression" {
			return jsValue{}
		}
		return v
	case "ConditionalExpression":
		j.eval(n.Condition)
		return jsUnionPreserving(j.eval(n.WhenTrue), j.eval(n.WhenFalse))
	case "IfStatement":
		j.eval(n.Expression)
		j.eval(n.ThenStatement)
		j.eval(n.ElseStatement)
		return jsValue{}
	case "TryStatement":
		previousThrows := j.thrown
		if n.CatchClause != nil {
			j.thrown = nil
			j.catchDepth++
		}
		j.eval(n.TryBlock)
		if n.CatchClause != nil {
			j.catchDepth--
			caught := j.thrown
			j.thrown = previousThrows
			if n.CatchClause.VariableDeclaration != nil {
				j.bind(n.CatchClause.VariableDeclaration.Name, jsValue{sources: caught})
			}
			j.eval(n.CatchClause.Block)
		}
		j.eval(n.FinallyBlock)
		return jsValue{}
	case "ThrowStatement":
		v := j.eval(n.Expression)
		if j.catchDepth > 0 {
			j.thrown = union(j.thrown, v.taint())
		} else {
			j.emit(n, Log, v.taint())
		}
		return v
	case "TemplateExpression", "TemplateSpan":
		return j.template(n)
	case "WhileStatement", "DoStatement", "ForStatement", "ForOfStatement", "ForInStatement":
		// A second pass covers loop-carried aliases; unresolved longer
		// fixed points remain explicit rather than claiming completion.
		j.children(n)
		v := j.children(n)
		j.gap(n, "JavaScript loop convergence is not fully modeled")
		return v
	case "ImportDeclaration":
		return j.importDeclaration(n)
	case "ExportAssignment":
		j.env["module"].fields["exports"] = j.eval(n.Expression)
		return jsValue{}
	case "EmptyStatement", "BreakStatement", "ContinueStatement":
		return jsValue{}
	default:
		v := j.children(n)
		j.gap(n, "JavaScript syntax or control flow is not modeled")
		return v
	}
}

func (j *javascript) children(n *JavaScriptNode) jsValue {
	var values []jsValue
	for _, child := range []*JavaScriptNode{
		n.Expression, n.Initializer, n.DeclarationList, n.Body, n.Left, n.Right, n.Condition,
		n.WhenTrue, n.WhenFalse, n.ThenStatement, n.ElseStatement, n.TryBlock, n.FinallyBlock, n.Incrementor, n.Operand, n.Block,
	} {
		values = append(values, j.eval(child))
	}
	for _, list := range [][]*JavaScriptNode{n.Statements, n.Declarations, n.Arguments, n.Elements, n.Properties, n.TemplateSpans, n.Clauses, n.Other} {
		for _, child := range list {
			values = append(values, j.eval(child))
		}
	}
	return jsUnion(values...)
}

func (j *javascript) functionValue(n *JavaScriptNode) jsValue {
	return jsValue{fn: &jsFunction{node: n, env: j.env, file: j.file}}
}

func (j *javascript) invoke(n *JavaScriptNode, fn *jsFunction, args []jsValue) jsValue {
	if len(j.active) > 32 || j.active[fn] {
		j.gap(n, "recursive JavaScript call is not modeled")
		return jsValue{}
	}
	env, file, returns := j.env, j.file, j.returns
	j.env, j.file, j.returns = maps.Clone(fn.env), fn.file, jsValue{}
	shadowed := jsLocalNames(fn.node)
	for name := range shadowed {
		delete(j.env, name)
	}
	for i, parameter := range fn.node.Parameters {
		v := j.eval(parameter.Initializer)
		if arg, ok := sliceAt(args, i); ok {
			v = jsUnionPreserving(v, arg)
		}
		if jsHasKind(parameter.Other, "DotDotDotToken") {
			v = jsValue{}
			if i < len(args) {
				v = jsUnion(args[i:]...)
			}
		}
		j.bind(parameter.Name, v)
	}
	j.active[fn] = true
	body := j.eval(fn.node.Body)
	delete(j.active, fn)
	result := j.returns
	if fn.node.Body != nil && fn.node.Body.Kind != "Block" {
		result = jsUnionPreserving(result, body)
	}
	for name, previous := range fn.env {
		if _, local := shadowed[name]; !local {
			fn.env[name] = jsUnionPreserving(previous, j.env[name])
		}
	}
	j.env, j.file, j.returns = env, file, returns
	return result
}

func (j *javascript) optionsRuntimePaths() map[string]string {
	if j.options == nil {
		return nil
	}
	return j.options.RuntimePaths
}

func jsRefModule(ref string) string {
	return strings.TrimPrefix(ref, "node:")
}
