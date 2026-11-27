// Package goroutineresourceleak detects goroutine resource leaks, missing
// cleanup, and unsynchronized goroutines launched from loops.
package goroutineresourceleak

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/linters/internal/parentmap"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:goroutineresourceleak")

// Analyzer is the goroutine-resource-leak analysis pass.
var Analyzer = analyzerutil.New(
	"goroutineresourceleak",
	"reports goroutine resource leaks, unmatched channel operations, and loop-launched goroutines without WaitGroup synchronization",
	run,
	parentmap.Analyzer,
)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	fileParents, err := parentmap.Of(pass)
	if err != nil {
		return nil, err
	}
	nodeFilter := []ast.Node{(*ast.GoStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		goStmt, ok := n.(*ast.GoStmt)
		if !ok {
			return
		}
		parents := fileParents[astutil.FileForPos(pass.Files, goStmt.Pos())]
		checkGoroutineResourceLeak(pass, goStmt, parents, noLintIndex, generatedFiles)
	})
}

// checkGoroutineResourceLeak analyzes a goroutine launch for resource leaks.
func checkGoroutineResourceLeak(
	pass *analysis.Pass,
	goStmt *ast.GoStmt,
	parents map[ast.Node]ast.Node,
	noLintIndex nolint.DirectiveIndex,
	generatedFiles filecheck.GeneratedIndex,
) {
	// GoStmt.Call is already a *ast.CallExpr.
	call := goStmt.Call
	if call == nil {
		return
	}

	position := pass.Fset.PositionFor(goStmt.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
		return
	}

	if nolint.HasDirectiveForLinter(position, noLintIndex, "goroutineresourceleak") {
		return
	}

	if checkLoopWaitGroupLeak(pass, goStmt, parents) {
		pass.ReportRangef(goStmt, "goroutine launched in a loop without matching WaitGroup Add and Wait calls")
	}

	funcLit, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.FuncLit)
	if !ok {
		return
	}

	// Check for resource leaks in the function body.
	if checkCancelLeak(pass, funcLit.Body) {
		pkgLog.Printf("flagging goroutine with context cancel leak at %s", position)
		pass.ReportRangef(goStmt, "goroutine launches context.WithCancel/WithTimeout/WithDeadline without deferred cancel; context and goroutine may leak")
		return
	}

	if checkMutexLeak(pass, funcLit.Body) {
		pkgLog.Printf("flagging goroutine with mutex lock leak at %s", position)
		pass.ReportRangef(goStmt, "goroutine acquires lock without deferred unlock; may deadlock if function panics")
		return
	}

	if checkChannelLeak(pass, funcLit.Body) {
		pkgLog.Printf("flagging goroutine with channel leak at %s", position)
		pass.ReportRangef(goStmt, "goroutine creates channel without safe buffered send/receive pairing or close; may deadlock")
		return
	}
}

// checkCancelLeak reports whether the function body creates a context cancel
// function without deferring its cleanup.
func checkCancelLeak(
	pass *analysis.Pass,
	body *ast.BlockStmt,
) bool {
	if body == nil {
		return false
	}

	return anyBlock(body, func(block *ast.BlockStmt) bool {
		for i, stmt := range block.List {
			cancel, ok := contextCancelAcquisition(pass, stmt)
			if ok && !cleanupFollows(pass, block.List[i+1:], receiverKey{base: cancel}, func(stmt ast.Stmt) bool {
				return isDeferredCancel(pass, stmt, cancel)
			}) {
				return true
			}
		}
		return false
	})
}

// checkMutexLeak reports whether the function body acquires a lock without
// guaranteeing defer unlock.
func checkMutexLeak(
	pass *analysis.Pass,
	body *ast.BlockStmt,
) bool {
	if body == nil {
		return false
	}

	return anyBlock(body, func(block *ast.BlockStmt) bool {
		for i, stmt := range block.List {
			key, unlock, ok := mutexLockAcquisition(pass, stmt)
			if ok && !cleanupFollows(pass, block.List[i+1:], key, func(stmt ast.Stmt) bool {
				return isDeferredUnlock(pass, stmt, key, unlock)
			}) {
				return true
			}
		}
		return false
	})
}

// Only scan past statements that cannot bypass cleanup or rebind the resource.
func cleanupFollows(pass *analysis.Pass, statements []ast.Stmt, resource receiverKey, matches func(ast.Stmt) bool) bool {
	for _, stmt := range statements {
		if matches(stmt) {
			return true
		}
		unsafe := false
		ast.Inspect(stmt, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ReturnStmt, *ast.BranchStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SelectStmt:
				unsafe = true
			case *ast.ExprStmt:
				if key, _, ok := mutexLockAcquisition(pass, n); ok && key == resource {
					unsafe = true
				}
				if call, ok := astutil.UnwrapParenExpr(n.X).(*ast.CallExpr); ok && calledBuiltin(pass, call) == "panic" {
					unsafe = true
				}
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					key, ok := getReceiverKey(pass, lhs)
					if ok && (key == resource || key == (receiverKey{base: resource.base})) {
						unsafe = true
					}
				}
			}
			return !unsafe
		})
		if unsafe {
			return false
		}
	}
	return false
}

// checkChannelLeak reports channels without a safe buffered send/receive pair
// or close.
func checkChannelLeak(
	pass *analysis.Pass,
	body *ast.BlockStmt,
) bool {
	if body == nil {
		return false
	}

	channels := createdChannels(pass, body)
	markChannelUsage(pass, body, channels)
	for _, usage := range channels {
		if usage.escaped {
			continue
		}
		if usage.buffered && usage.sent && usage.received {
			continue
		}
		if usage.closed && !usage.sent && !usage.received {
			continue
		}
		return true
	}
	return false
}

func createdChannels(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]*channelUsage {
	channels := make(map[types.Object]*channelUsage)
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if assignStmt, ok := n.(*ast.AssignStmt); ok {
			var rhs ast.Expr
			for _, value := range assignStmt.Rhs {
				rhs = value
				break
			}
			buffered, isMake := channelMake(pass, rhs)
			if len(assignStmt.Rhs) != 1 || !isMake {
				return true
			}
			for _, lhs := range assignStmt.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && ident.Name != "_" {
					if obj := pass.TypesInfo.ObjectOf(ident); obj != nil {
						channels[obj] = &channelUsage{buffered: buffered}
					}
					break
				}
			}
		}
		return true
	})
	return channels
}

func markChannelUsage(pass *analysis.Pass, body *ast.BlockStmt, channels map[types.Object]*channelUsage) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			markChannelEscape(pass, n, channels)
			return false
		}
		if sendStmt, ok := n.(*ast.SendStmt); ok {
			if usage := channelReference(pass, sendStmt.Chan, channels); usage != nil {
				usage.sent = true
			}
		}
		if callExpr, ok := n.(*ast.CallExpr); ok {
			builtin := calledBuiltin(pass, callExpr)
			if builtin == "close" {
				for _, arg := range callExpr.Args {
					if usage := channelReference(pass, arg, channels); usage != nil {
						usage.closed = true
					}
				}
			} else if builtin != "len" && builtin != "cap" && !pass.TypesInfo.Types[callExpr.Fun].IsType() {
				for _, arg := range callExpr.Args {
					markChannelEscape(pass, arg, channels)
				}
			}
		}
		switch n := n.(type) {
		case *ast.CompositeLit:
			markChannelEscape(pass, n, channels)
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				switch lhs := astutil.UnwrapParenExpr(lhs).(type) {
				case *ast.Ident:
					if lhs.Name != "_" && len(n.Lhs) == len(n.Rhs) {
						// Aliases transfer usage beyond this syntactic check.
						markChannelEscape(pass, n.Rhs[i], channels) //nolint:uncheckedsliceindex // i ranges over n.Lhs and lengths are equal
					}
				case *ast.SelectorExpr, *ast.IndexExpr:
					for _, rhs := range n.Rhs {
						markChannelEscape(pass, rhs, channels)
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if name.Name != "_" && len(n.Names) == len(n.Values) {
					markChannelEscape(pass, n.Values[i], channels) //nolint:uncheckedsliceindex // i ranges over n.Names and lengths are equal
				}
			}
		}
		if unaryExpr, ok := n.(*ast.UnaryExpr); ok && unaryExpr.Op == token.ARROW {
			if usage := channelReference(pass, unaryExpr.X, channels); usage != nil {
				usage.received = true
			}
		}
		return true
	})
}

func channelReference(pass *analysis.Pass, expr ast.Expr, channels map[types.Object]*channelUsage) *channelUsage {
	expr = astutil.UnwrapParenExpr(expr)
	if call, ok := expr.(*ast.CallExpr); ok && pass.TypesInfo.Types[call.Fun].IsType() && len(call.Args) == 1 {
		return channelReference(pass, call.Args[0], channels) //nolint:uncheckedsliceindex // len(call.Args) is checked above
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return channels[pass.TypesInfo.ObjectOf(ident)]
	}
	return nil
}

// An opaque handoff prevents this local syntactic check from proving a leak.
func markChannelEscape(pass *analysis.Pass, node ast.Node, channels map[types.Object]*channelUsage) {
	_, capturing := node.(*ast.FuncLit)
	ast.Inspect(node, func(n ast.Node) bool {
		if !capturing {
			if unary, ok := n.(*ast.UnaryExpr); ok && unary.Op == token.ARROW {
				return false
			}
			if binary, ok := n.(*ast.BinaryExpr); ok {
				switch binary.Op {
				case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
					return false
				}
			}
			if call, ok := n.(*ast.CallExpr); ok {
				if builtin := calledBuiltin(pass, call); builtin == "len" || builtin == "cap" {
					return false
				}
			}
		}
		if ident, ok := n.(*ast.Ident); ok {
			if usage := channels[pass.TypesInfo.ObjectOf(ident)]; usage != nil {
				usage.escaped = true
			}
		}
		return true
	})
}

func calledBuiltin(pass *analysis.Pass, call *ast.CallExpr) string {
	ident, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.Ident)
	if !ok {
		return ""
	}
	builtin, ok := pass.TypesInfo.ObjectOf(ident).(*types.Builtin)
	if !ok {
		return ""
	}
	return builtin.Name()
}

type channelUsage struct {
	sent     bool
	received bool
	closed   bool
	buffered bool
	escaped  bool
}

// isContextWithCancelCall reports whether call is context.WithCancel,
// context.WithTimeout, or context.WithDeadline.
func isContextWithCancelCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if !astutil.IsPkgSelector(pass, sel, "context") {
		return false
	}
	switch sel.Sel.Name {
	case "WithCancel", "WithTimeout", "WithDeadline":
		return true
	default:
		return false
	}
}

// isMutexType reports whether expr is a sync.Mutex or sync.RWMutex.
func isMutexType(pass *analysis.Pass, expr ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return false
	}

	// Handle pointers to mutex.
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}

	// Handle named types like sync.Mutex.
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}

	pkgPath := obj.Pkg().Path()
	name := obj.Name()

	return pkgPath == "sync" && (name == "Mutex" || name == "RWMutex")
}

type receiverKey struct {
	base  types.Object
	field types.Object
}

func anyBlock(body *ast.BlockStmt, check func(*ast.BlockStmt) bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if block, ok := n.(*ast.BlockStmt); ok && check(block) {
			found = true
			return false
		}
		return !found
	})
	return found
}

func contextCancelAcquisition(pass *analysis.Pass, stmt ast.Stmt) (types.Object, bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) < 2 {
		return nil, false
	}
	var rhs ast.Expr
	for _, value := range assign.Rhs {
		rhs = value
		break
	}
	call, ok := astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
	if !ok || !isContextWithCancelCall(pass, call) {
		return nil, false
	}
	var cancelExpr ast.Expr
	for i, value := range assign.Lhs {
		if i == 1 {
			cancelExpr = value
			break
		}
	}
	cancel, ok := cancelExpr.(*ast.Ident)
	if !ok || cancel.Name == "_" {
		return nil, false
	}
	object := pass.TypesInfo.ObjectOf(cancel)
	return object, object != nil
}

func isDeferredCancel(pass *analysis.Pass, stmt ast.Stmt, cancel types.Object) bool {
	deferStmt, ok := stmt.(*ast.DeferStmt)
	if !ok || deferStmt.Call == nil {
		return false
	}
	ident, ok := astutil.UnwrapParenExpr(deferStmt.Call.Fun).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(ident) == cancel
}

func mutexLockAcquisition(pass *analysis.Pass, stmt ast.Stmt) (receiverKey, string, bool) {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return receiverKey{}, "", false
	}
	call, ok := astutil.UnwrapParenExpr(exprStmt.X).(*ast.CallExpr)
	if !ok {
		return receiverKey{}, "", false
	}
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Lock" && sel.Sel.Name != "RLock") {
		return receiverKey{}, "", false
	}
	key, ok := mutexReceiverKey(pass, sel.X)
	if !ok {
		return receiverKey{}, "", false
	}
	unlock := "Unlock"
	if sel.Sel.Name == "RLock" {
		unlock = "RUnlock"
	}
	return key, unlock, true
}

func isDeferredUnlock(pass *analysis.Pass, stmt ast.Stmt, key receiverKey, unlock string) bool {
	deferStmt, ok := stmt.(*ast.DeferStmt)
	if !ok || deferStmt.Call == nil {
		return false
	}
	sel, ok := astutil.UnwrapParenExpr(deferStmt.Call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != unlock {
		return false
	}
	deferredKey, ok := mutexReceiverKey(pass, sel.X)
	return ok && deferredKey == key
}

func mutexReceiverKey(pass *analysis.Pass, receiver ast.Expr) (receiverKey, bool) {
	if !isMutexType(pass, receiver) {
		return receiverKey{}, false
	}
	return getReceiverKey(pass, receiver)
}

func getReceiverKey(pass *analysis.Pass, receiver ast.Expr) (receiverKey, bool) {
	switch expr := astutil.UnwrapParenExpr(receiver).(type) {
	case *ast.Ident:
		object := pass.TypesInfo.ObjectOf(expr)
		if object != nil {
			return receiverKey{base: object}, true
		}
	case *ast.SelectorExpr:
		if selection := pass.TypesInfo.Selections[expr]; selection != nil {
			field := selection.Obj()
			if base, ok := astutil.UnwrapParenExpr(expr.X).(*ast.Ident); ok {
				if object := pass.TypesInfo.ObjectOf(base); object != nil {
					return receiverKey{base: object, field: field}, true
				}
			}
			return receiverKey{base: field}, field != nil
		}
	}
	return receiverKey{}, false
}

func channelMake(pass *analysis.Pass, expr ast.Expr) (bool, bool) {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok {
		return false, false
	}
	ident, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.Ident)
	if !ok {
		return false, false
	}
	if builtin, ok := pass.TypesInfo.ObjectOf(ident).(*types.Builtin); !ok || builtin.Name() != "make" {
		return false, false
	}
	var channelType ast.Expr
	var capacity ast.Expr
	for i, arg := range call.Args {
		switch i {
		case 0:
			channelType = arg
		case 1:
			capacity = arg
		}
	}
	if _, ok := channelType.(*ast.ChanType); !ok {
		return false, false
	}
	if capacity == nil {
		return false, true
	}
	value := pass.TypesInfo.Types[capacity].Value
	return value != nil && constant.Sign(value) > 0, true
}
