package lint

import (
	"context"
	"errors"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/taint"
)

type graderJavaScriptNode struct {
	node   *taint.JavaScriptNode
	parent *taint.JavaScriptNode
	depth  int
	locals map[string]struct{}
}

func graderFileIOAST(ctx context.Context, root *taint.JavaScriptNode, path string) ([]Diagnostic, error) {
	visited := 0
	var diagnostics []Diagnostic
	var visit func(graderJavaScriptNode) error
	visit = func(current graderJavaScriptNode) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current.node == nil {
			return nil
		}
		visited++
		if visited > 100000 || current.depth > 256 {
			return errors.New("custom grader JavaScript AST exceeds analysis limits; simplify the script")
		}
		locals := graderJavaScriptLocals(current.node, current.locals)
		if graderFileIOOperation(current.node, current.parent, locals) {
			diagnostics = append(diagnostics, Diagnostic{
				Path: path, Line: current.node.Line,
				Message: "Custom graders must not access files or load host modules; compute from the in-memory session, config, and helpers instead",
			})
		}
		for _, child := range current.node.Children() {
			childLocals := locals
			if child == current.node.Name && child != nil && child.Kind == "ComputedPropertyName" {
				childLocals = current.locals
			}
			if err := visit(graderJavaScriptNode{node: child, parent: current.node, depth: current.depth + 1, locals: childLocals}); err != nil {
				return err
			}
		}
		return nil
	}
	err := visit(graderJavaScriptNode{node: root})
	return diagnostics, err
}

func graderFileIOOperation(node, parent *taint.JavaScriptNode, locals map[string]struct{}) bool {
	switch node.Kind {
	case "ImportDeclaration", "ImportEqualsDeclaration", "ImportKeyword":
		return true
	case "ExportDeclaration":
		return node.ModuleSpecifier != nil
	case "Identifier":
		if !graderJavaScriptReference(node, parent) {
			return false
		}
		_, local := locals[node.Text]
		return !local && graderJavaScriptFileCapability(node.Text)
	}
	ref := graderJavaScriptReferenceName(node)
	root, _, _ := strings.Cut(ref, ".")
	if _, local := locals[root]; local {
		return false
	}
	for _, prefix := range []string{"globalThis.", "global.", "window.", "self."} {
		ref = strings.TrimPrefix(ref, prefix)
	}
	if graderJavaScriptFileCapability(ref) {
		return true
	}
	switch {
	case strings.HasPrefix(ref, "fs."), strings.HasPrefix(ref, "fsp."), strings.HasPrefix(ref, "fsPromises."),
		strings.HasPrefix(ref, "Deno."), strings.HasPrefix(ref, "process.mainModule."):
		return true
	}
	switch ref {
	case "process.getBuiltinModule", "process.binding", "process.dlopen", "module.require",
		"module.createRequire", "Bun.file", "Bun.write", "Bun.spawn", "Bun.spawnSync", "Bun.$",
		"navigator.storage", "navigator.storage.getDirectory", "navigator.webkitPersistentStorage",
		"navigator.webkitTemporaryStorage":
		return true
	}
	return false
}

func graderJavaScriptFileCapability(name string) bool {
	switch name {
	case "require", "module", "process", "fs", "fsp", "fsPromises", "Deno", "Bun", "FileReader", "FileReaderSync",
		"showOpenFilePicker", "showSaveFilePicker", "showDirectoryPicker", "requestFileSystem", "webkitRequestFileSystem":
		return true
	}
	return false
}

func graderJavaScriptReference(node, parent *taint.JavaScriptNode) bool {
	if parent == nil {
		return true
	}
	if parent.Name == node && parent.Kind != "ShorthandPropertyAssignment" || parent.PropertyName == node {
		return false
	}
	return parent.Kind != "ImportSpecifier" && parent.Kind != "ExportSpecifier"
}

func graderJavaScriptReferenceName(node *taint.JavaScriptNode) string {
	if node == nil {
		return ""
	}
	switch node.Kind {
	case "Identifier":
		return node.Text
	case "PropertyAccessExpression":
		if node.Name != nil && node.Name.Text != "" {
			base := graderJavaScriptReferenceName(node.Expression)
			if base != "" {
				return base + "." + node.Name.Text
			}
		}
	case "ElementAccessExpression":
		key := node.ArgumentExpression
		if key != nil && key.Text != "" && (key.Kind == "StringLiteral" || key.Kind == "NoSubstitutionTemplateLiteral") {
			base := graderJavaScriptReferenceName(node.Expression)
			if base != "" {
				return base + "." + key.Text
			}
		}
	case "ParenthesizedExpression", "NonNullExpression":
		return graderJavaScriptReferenceName(node.Expression)
	}
	return ""
}
