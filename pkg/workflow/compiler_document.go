package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/workflow/aw"
)

// ParseWorkflowDocument validates and resolves an AW source AST using the same
// schema, imports, defaults, diagnostics, and path rules as ParseWorkflowString.
// The document is not mutated. virtualPath anchors relative imports and errors.
func (c *Compiler) ParseWorkflowDocument(document *aw.Document, virtualPath string) (*WorkflowData, error) {
	content, err := aw.Emit(document)
	if err != nil {
		return nil, fmt.Errorf("serializing workflow document: %w", err)
	}
	return c.ParseWorkflowString(content, virtualPath)
}
