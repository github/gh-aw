package taint

import (
	"context"
	"io/fs"
)

// JavaScriptNode is a projection of the native TypeScript compiler's AST.
// Unrecognized syntax is retained in Other, never dropped by the parser.
type JavaScriptNode struct {
	Kind                string            `json:"kind"`
	Text                string            `json:"text"`
	Line                int               `json:"line"`
	Name                *JavaScriptNode   `json:"name"`
	Expression          *JavaScriptNode   `json:"expression"`
	Initializer         *JavaScriptNode   `json:"initializer"`
	DeclarationList     *JavaScriptNode   `json:"declarationList"`
	Body                *JavaScriptNode   `json:"body"`
	Block               *JavaScriptNode   `json:"block"`
	Left                *JavaScriptNode   `json:"left"`
	Right               *JavaScriptNode   `json:"right"`
	OperatorToken       *JavaScriptNode   `json:"operatorToken"`
	Condition           *JavaScriptNode   `json:"condition"`
	WhenTrue            *JavaScriptNode   `json:"whenTrue"`
	WhenFalse           *JavaScriptNode   `json:"whenFalse"`
	ThenStatement       *JavaScriptNode   `json:"thenStatement"`
	ElseStatement       *JavaScriptNode   `json:"elseStatement"`
	TryBlock            *JavaScriptNode   `json:"tryBlock"`
	CatchClause         *JavaScriptNode   `json:"catchClause"`
	FinallyBlock        *JavaScriptNode   `json:"finallyBlock"`
	VariableDeclaration *JavaScriptNode   `json:"variableDeclaration"`
	ArgumentExpression  *JavaScriptNode   `json:"argumentExpression"`
	ModuleSpecifier     *JavaScriptNode   `json:"moduleSpecifier"`
	ImportClause        *JavaScriptNode   `json:"importClause"`
	NamedBindings       *JavaScriptNode   `json:"namedBindings"`
	PropertyName        *JavaScriptNode   `json:"propertyName"`
	Incrementor         *JavaScriptNode   `json:"incrementor"`
	Operand             *JavaScriptNode   `json:"operand"`
	Statements          []*JavaScriptNode `json:"statements"`
	Declarations        []*JavaScriptNode `json:"declarations"`
	Arguments           []*JavaScriptNode `json:"arguments"`
	Parameters          []*JavaScriptNode `json:"parameters"`
	Elements            []*JavaScriptNode `json:"elements"`
	Properties          []*JavaScriptNode `json:"properties"`
	TemplateSpans       []*JavaScriptNode `json:"templateSpans"`
	Clauses             []*JavaScriptNode `json:"clauses"`
	Other               []*JavaScriptNode `json:"other"`
}

// Children includes every projected child, including otherwise unmodeled syntax.
func (n *JavaScriptNode) Children() []*JavaScriptNode {
	if n == nil {
		return nil
	}
	var result []*JavaScriptNode
	for _, child := range []*JavaScriptNode{
		n.Name, n.Expression, n.Initializer, n.DeclarationList, n.Body, n.Block,
		n.Left, n.Right, n.OperatorToken, n.Condition, n.WhenTrue, n.WhenFalse,
		n.ThenStatement, n.ElseStatement, n.TryBlock, n.CatchClause, n.FinallyBlock,
		n.VariableDeclaration, n.ArgumentExpression, n.ModuleSpecifier,
		n.ImportClause, n.NamedBindings, n.PropertyName, n.Incrementor, n.Operand,
	} {
		if child != nil {
			result = append(result, child)
		}
	}
	for _, list := range [][]*JavaScriptNode{
		n.Statements, n.Declarations, n.Arguments, n.Parameters, n.Elements,
		n.Properties, n.TemplateSpans, n.Clauses, n.Other,
	} {
		result = append(result, list...)
	}
	return result
}

type JavaScriptParser interface {
	Parse(context.Context, string) (*JavaScriptNode, error)
}

type JavaScriptParseResult struct {
	Node *JavaScriptNode
	Err  error
}

// JavaScriptBatchParser amortizes native compiler startup across independent
// scripts. A syntax failure belongs to its script, not the entire batch.
type JavaScriptBatchParser interface {
	ParseBatch(context.Context, []string) ([]JavaScriptParseResult, error)
}

type JavaScriptMode string

const (
	JavaScriptFast JavaScriptMode = "fast"
	JavaScriptDeep JavaScriptMode = "deep"
)

// JavaScriptOptions enables native AST analysis explicitly. Files is the
// repository source filesystem, not the runner's filesystem. RuntimePaths maps
// operator-verified runner asset directories to corresponding repository paths.
// Without a mapping, generated/dynamic paths remain unresolved.
type JavaScriptOptions struct {
	Context      context.Context
	Parser       JavaScriptParser
	Files        fs.FS
	RuntimePaths map[string]string
	// An empty mode preserves the original, deep library API behavior.
	Mode JavaScriptMode
	// fastEntries limits compiled entry traversal to authored scripts. Imported
	// modules still use the normal verified repository source resolver.
	fastEntries map[string]bool
	fastMatched map[string][]string
	fastSites   map[string][]string
}

type JavaScriptInput struct {
	Name          string
	Source        string
	Env           map[string][]Source
	Stdin         []Source
	Args          [][]Source
	Files         map[string][]Source
	Literals      map[string][]Source
	CaptureStdout bool
	ArgvOffset    int
}

type JavaScriptResult struct {
	Result
	Stdout []Source
	Files  map[string][]Source
}

// AnalyzeJavaScript never executes the input or resolves a secret value.
// Sources are caller-provided symbolic bindings (plus Rule.IsSource), sinks
// reuse Rule.IsSink, and Rule.IsJavaScriptFilter declassifies whole expressions.
func AnalyzeJavaScript(input JavaScriptInput, rule Rule, options JavaScriptOptions) (JavaScriptResult, error) {
	a := &analyzer{rule: rule}
	j := newJavaScript(a, input, &options)
	if err := j.run(); err != nil {
		return JavaScriptResult{}, err
	}
	return JavaScriptResult{Result: a.result, Stdout: j.stdout, Files: j.files}, nil
}
