// Package jsondecoderunknownfields reports JSON decoders that do not reject
// unknown fields.
package jsondecoderunknownfields

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "jsondecoderunknownfields"

// Analyzer reports JSON decoders that do not reject unknown fields.
var Analyzer = analyzerutil.New(linterName, "reports JSON decoders that do not reject unknown fields", run)

type decoderConstruction struct {
	call     *ast.CallExpr
	block    *ast.BlockStmt
	function ast.Node
}

type storageKey struct {
	base types.Object
	path string
}

type decoderEvent struct {
	pos      token.Pos
	block    *ast.BlockStmt
	direct   bool
	exit     bool
	function ast.Node
	call     *ast.CallExpr
	selector *ast.SelectorExpr
	lhs      []ast.Expr
	rhs      []ast.Expr
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	constructions, events := collectDecoderCalls(pass)
	for _, construction := range constructions {
		if isStrictBeforeDecode(pass, construction, events) {
			continue
		}
		position := pass.Fset.PositionFor(construction.call.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(position, noLintIndex, linterName) {
			continue
		}
		pass.ReportRangef(construction.call, "json.NewDecoder accepts unknown fields by default; call DisallowUnknownFields before Decode")
	}

	return nil, nil
}

type decoderCollector struct {
	pass          *analysis.Pass
	constructions []decoderConstruction
	events        []decoderEvent
	gotoBlocks    map[*ast.BlockStmt]bool
}

func collectDecoderCalls(pass *analysis.Pass) ([]decoderConstruction, []decoderEvent) {
	c := decoderCollector{pass: pass, gotoBlocks: make(map[*ast.BlockStmt]bool)}
	for _, file := range pass.Files {
		var stack []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			c.collectNode(node, stack)
			stack = append(stack, node)
			return true
		})
	}
	for i := range c.constructions {
		if c.gotoBlocks[c.constructions[i].block] {
			c.constructions[i].block = nil
		}
	}
	slices.SortStableFunc(c.events, func(a, b decoderEvent) int { return cmp.Compare(a.pos, b.pos) })
	return c.constructions, c.events
}

func stackAncestor(stack []ast.Node, depth int) ast.Node {
	for _, node := range slices.Backward(stack) {
		if depth == 0 {
			return node
		}
		depth--
	}
	return nil
}

func enclosingNodes(stack []ast.Node) (*ast.BlockStmt, ast.Node) {
	var block *ast.BlockStmt
	for _, node := range slices.Backward(stack) {
		if b, ok := node.(*ast.BlockStmt); ok && block == nil {
			block = b
		}
		switch node.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return block, node
		}
	}
	return block, nil
}

func (c *decoderCollector) collectNode(node ast.Node, stack []ast.Node) {
	block, function := enclosingNodes(stack)
	parent := stackAncestor(stack, 0)
	switch n := node.(type) {
	case *ast.BranchStmt:
		c.events = append(c.events, decoderEvent{pos: n.Pos(), exit: true, function: function})
		if n.Tok == token.GOTO {
			c.recordGoto(stack)
		}
	case *ast.ReturnStmt:
		c.events = append(c.events, decoderEvent{pos: n.End(), exit: true, function: function})
	case *ast.CallExpr:
		if isJSONNewDecoder(c.pass, n) {
			c.constructions = append(c.constructions, decoderConstruction{call: n, block: block, function: function})
		}
		_, expr := parent.(*ast.ExprStmt)
		_, inBlock := stackAncestor(stack, 1).(*ast.BlockStmt)
		c.events = append(c.events, decoderEvent{pos: n.Pos(), block: block, direct: expr && inBlock, call: n})
	case *ast.SelectorExpr:
		if n.Sel.Name == "Decode" {
			c.events = append(c.events, decoderEvent{pos: n.Pos(), selector: n})
		}
	case *ast.AssignStmt:
		_, direct := parent.(*ast.BlockStmt)
		c.events = append(c.events, decoderEvent{pos: n.End(), block: block, direct: direct, lhs: n.Lhs, rhs: n.Rhs})
	case *ast.RangeStmt:
		c.collectRange(n, block)
	case *ast.ValueSpec:
		c.collectValue(n, block, stack)
	}
}

func (c *decoderCollector) recordGoto(stack []ast.Node) {
	for _, node := range slices.Backward(stack) {
		switch node := node.(type) {
		case *ast.BlockStmt:
			c.gotoBlocks[node] = true
		case *ast.FuncDecl, *ast.FuncLit:
			return
		}
	}
}

func (c *decoderCollector) collectRange(n *ast.RangeStmt, block *ast.BlockStmt) {
	var lhs []ast.Expr
	if n.Key != nil {
		lhs = append(lhs, n.Key)
	}
	if n.Value != nil {
		lhs = append(lhs, n.Value)
	}
	c.events = append(c.events, decoderEvent{pos: n.X.End(), block: block, lhs: lhs})
}

func (c *decoderCollector) collectValue(n *ast.ValueSpec, block *ast.BlockStmt, stack []ast.Node) {
	var lhs []ast.Expr
	for _, name := range n.Names {
		lhs = append(lhs, name)
	}
	_, gen := stackAncestor(stack, 0).(*ast.GenDecl)
	_, decl := stackAncestor(stack, 1).(*ast.DeclStmt)
	_, inBlock := stackAncestor(stack, 2).(*ast.BlockStmt)
	c.events = append(c.events, decoderEvent{pos: n.End(), block: block, direct: gen && decl && inBlock, lhs: lhs, rhs: n.Values})
}

type aliasState struct{ may, must bool }

type decoderTracker struct {
	pass         *analysis.Pass
	construction decoderConstruction
	aliases      map[storageKey]aliasState
	strict       bool
}

func isStrictBeforeDecode(pass *analysis.Pass, construction decoderConstruction, events []decoderEvent) bool {
	if construction.block == nil {
		return false
	}
	// Only straight-line writes and strict calls in the construction's block
	// establish facts; writes in nested control flow can invalidate them.
	tracker := decoderTracker{pass: pass, construction: construction, aliases: make(map[storageKey]aliasState)}
	for _, event := range events {
		if event.pos <= construction.call.Pos() {
			continue
		}
		if event.pos >= construction.block.End() {
			break
		}
		if !tracker.processEvent(event) {
			return false
		}
	}
	return tracker.strict
}

func (t *decoderTracker) processEvent(event decoderEvent) bool {
	if event.exit && event.function == t.construction.function && !t.strict {
		return false
	}
	if event.selector != nil {
		return t.strict || !t.aliases[decoderStorage(t.pass, event.selector.X)].may
	}
	if event.call != nil {
		return t.processCall(event)
	}
	return t.processWrites(event)
}

func (t *decoderTracker) processCall(event decoderEvent) bool {
	for _, arg := range event.call.Args {
		if expressionContainsDecoder(t.pass, arg, t.aliases) {
			return false
		}
	}
	selector, ok := astutil.UnwrapParenExpr(event.call.Fun).(*ast.SelectorExpr)
	if !ok {
		return true
	}
	state := t.aliases[decoderStorage(t.pass, selector.X)]
	if selector.Sel.Name == "Decode" && state.may && !t.strict {
		return false
	}
	if selector.Sel.Name == "DisallowUnknownFields" && state.must &&
		event.direct && event.block == t.construction.block {
		t.strict = true
	}
	return true
}

func (t *decoderTracker) processWrites(event decoderEvent) bool {
	states := make(map[int]aliasState)
	for i := range event.lhs {
		rhs, ok := astutil.RhsExprForIndex(event.rhs, i)
		if !ok {
			continue
		}
		rhs = astutil.UnwrapParenExpr(rhs)
		if rhs == t.construction.call {
			states[i] = aliasState{may: true, must: true}
		} else if typ := t.pass.TypesInfo.TypeOf(rhs); typ != nil {
			if _, ok := types.Unalias(typ).(*types.Pointer); !ok {
				if mayContainDecoder(t.pass, rhs, t.aliases) {
					return false
				}
				continue
			}
			states[i] = t.aliases[decoderStorage(t.pass, rhs)]
		}
	}
	for i, lhs := range event.lhs {
		t.recordWrite(lhs, states[i], event.direct && event.block == t.construction.block)
	}
	return true
}

func (t *decoderTracker) recordWrite(lhs ast.Expr, value aliasState, direct bool) {
	key := decoderStorage(t.pass, lhs)
	if key.base == nil {
		return
	}
	typ := t.pass.TypesInfo.TypeOf(lhs)
	mayWriteContainer := key.path != ""
	if typ != nil {
		_, isStruct := types.Unalias(typ).Underlying().(*types.Struct)
		mayWriteContainer = mayWriteContainer || isStruct
	}
	_, indirect := astutil.UnwrapParenExpr(lhs).(*ast.StarExpr)
	if indirect && isJSONDecoder(t.pass, lhs) && t.aliases[key].may {
		if _, pointer := types.Unalias(typ).(*types.Pointer); !pointer {
			t.strict = false
		}
	}
	t.invalidateAliases(key, direct, indirect && isJSONDecoder(t.pass, lhs), mayWriteContainer)
	if direct {
		t.aliases[key] = value
	} else {
		old := t.aliases[key]
		t.aliases[key] = aliasState{may: old.may || value.may}
	}
}

func (t *decoderTracker) invalidateAliases(key storageKey, direct, indirect, mayWriteContainer bool) {
	for existing, state := range t.aliases {
		if existing.base == key.base && (existing.path == key.path || strings.HasPrefix(existing.path, key.path+".")) {
			if direct {
				delete(t.aliases, existing)
			} else {
				state.must = false
				t.aliases[existing] = state
			}
		} else if existing.base == key.base && fieldParent(existing.path) == fieldParent(key.path) {
			continue
		} else if indirect || (mayWriteContainer && existing.path != "" &&
			(strings.Contains(key.path, ".*") || strings.Contains(existing.path, ".*"))) {
			state.must = false
			t.aliases[existing] = state
		}
	}
}

func expressionContainsDecoder(pass *analysis.Pass, expr ast.Expr, aliases map[storageKey]aliasState) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if expression, ok := node.(ast.Expr); ok && mayContainDecoder(pass, expression, aliases) {
			found = true
			return false
		}
		return !found
	})
	return found
}

func mayContainDecoder(pass *analysis.Pass, expr ast.Expr, aliases map[storageKey]aliasState) bool {
	if unary, ok := astutil.UnwrapParenExpr(expr).(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	key := decoderStorage(pass, expr)
	if key.base == nil {
		return false
	}
	for existing, state := range aliases {
		if state.may && existing.base == key.base &&
			(existing.path == key.path || strings.HasPrefix(existing.path, key.path+".")) {
			return true
		}
	}
	return false
}

func fieldParent(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[:i]
	}
	return path
}

func isJSONNewDecoder(pass *analysis.Pass, call *ast.CallExpr) bool {
	pkgPath, funcName, ok := astutil.PackageCall(pass, call)
	return ok && pkgPath == "encoding/json" && funcName == "NewDecoder"
}

func isJSONDecoder(pass *analysis.Pass, expr ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return false
	}
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Name() == "Decoder" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "encoding/json"
}

func decoderStorage(pass *analysis.Pass, expr ast.Expr) storageKey {
	switch expr := astutil.UnwrapParenExpr(expr).(type) {
	case *ast.Ident:
		return storageKey{base: pass.TypesInfo.ObjectOf(expr)}
	case *ast.SelectorExpr:
		selection := pass.TypesInfo.Selections[expr]
		if selection == nil || selection.Kind() != types.FieldVal {
			return storageKey{}
		}
		key := decoderStorage(pass, expr.X)
		var path strings.Builder
		path.WriteString(key.path)
		t := pass.TypesInfo.TypeOf(expr.X)
		for _, index := range selection.Index() {
			if t == nil {
				return storageKey{}
			}
			if pointer, ok := types.Unalias(t).(*types.Pointer); ok {
				path.WriteString(".*")
				t = pointer.Elem()
			}
			structure, ok := types.Unalias(t).Underlying().(*types.Struct)
			if !ok || index >= structure.NumFields() {
				return storageKey{}
			}
			field := structure.Field(index)
			path.WriteByte('.')
			path.WriteString(field.Name())
			t = field.Type()
		}
		key.path = path.String()
		return key
	case *ast.StarExpr:
		key := decoderStorage(pass, expr.X)
		if isJSONDecoder(pass, expr.X) {
			return key
		}
		key.path += ".*"
		return key
	}
	return storageKey{}
}
