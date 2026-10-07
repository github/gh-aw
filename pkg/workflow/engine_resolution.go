package workflow

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/github/gh-aw/pkg/parser"
)

// ResolveEffectiveEngineConfig resolves engine settings from the workflow and its imports/includes.
func (c *Compiler) ResolveEffectiveEngineConfig(content, markdownPath string) (*EngineConfig, error) {
	if markdownPath == "" {
		return nil, errors.New("workflow path is required to resolve engine imports")
	}
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse workflow frontmatter: %w", err)
	}

	c.configureGHESCompatibility()
	c.engineRegistry = NewEngineRegistry()
	c.engineCatalog = NewEngineCatalog(c.engineRegistry)
	cleanPath := filepath.Clean(markdownPath)
	setup, err := c.setupEngineAndImports(result, cleanPath, []byte(content), filepath.Dir(cleanPath))
	if err != nil {
		return nil, err
	}
	return setup.engineConfig, nil
}
