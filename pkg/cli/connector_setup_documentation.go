package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

// Wrapper documentation comes from the same normal imports used to load its
// declaration. No provider catalog, alternate source or credential lookup exists.
func loadConnectorSetupDocumentation(root, id string, declaration *workflow.DevelopmentConnector, inputs map[string]any) error {
	parsed, err := parser.ExtractFrontmatterFromContent(declaration.Markdown)
	if err != nil {
		return fmt.Errorf("read connector setup metadata: %w", err)
	}
	if parsed.Frontmatter["imports"] == nil {
		return nil
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	cache := parser.NewImportCache(root)
	cache.SetReadOnly(true)
	entry := map[string]any{"uses": declaration.Source}
	if len(inputs) > 0 {
		entry["with"] = inputs
	}
	imports, err := parser.ProcessImportsFromFrontmatterWithSource(
		map[string]any{"imports": []any{entry}}, root, cache,
		filepath.Join(root, "connector-development.md"), "",
	)
	if err != nil {
		return fmt.Errorf("resolve connector setup documentation imports: %w", err)
	}
	var markdown strings.Builder
	markdown.WriteString(declaration.Markdown)
	for _, imported := range imports.Connectors {
		if _, selected := imported.Registrations[id]; !selected {
			continue
		}
		source, err := parser.ResolveIncludePath(imported.Source, root, cache)
		if err != nil {
			return fmt.Errorf("resolve connector setup documentation source: %w", err)
		}
		content, err := parser.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read connector setup documentation source: %w", err)
		}
		markdown.WriteByte('\n')
		markdown.Write(content)
	}
	declaration.Markdown = markdown.String()
	return nil
}
