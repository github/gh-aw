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
	setup, err := c.resolveWorkflowEngine(result, cleanPath, []byte(content), filepath.Dir(cleanPath))
	if err != nil {
		return nil, err
	}
	return setup.engineConfig, nil
}

// Apply a workflow-wide routing opt-out after the engine and imported defaults
// have been resolved, before either compilation or config inspection uses them.
func (c *Compiler) resolveWorkflowEngine(source *parser.FrontmatterResult, path string, content []byte, dir string) (*engineSetupResult, error) {
	opt, ok := source.Frontmatter["model-routing"].(bool)
	local := ok && !opt
	if local && parser.LegacyModelRoutingWarning(source.Frontmatter) != "" {
		return nil, NewValidationError("model-routing", "false", "both top-level and nested routing are configured", "Keep top-level model-routing: false and remove engine.model-routing to opt out.")
	}
	setup, err := c.setupEngineAndImports(source, path, content, dir)
	if err != nil {
		return nil, err
	}
	if !local && !setup.importsResult.RouterDisabled {
		return setup, nil
	}
	config := setup.engineConfig
	if config.ModelRouting != nil && config.ModelRouting.requested != "" {
		return nil, NewValidationError("model-routing", "false", "a router model cannot be combined with model-routing: false", "Remove the opt-out (including any imported one) to route, or remove the router selector and shortlist to opt out.")
	}
	// As with the nested syntax, a local opt-out wins over an imported
	// routing object, but an imported opt-out does not override one.
	if local || config.ModelRouting == nil {
		config.ModelRouting = nil
		config.ModelRoutingDisabled = true
	}
	return setup, nil
}
