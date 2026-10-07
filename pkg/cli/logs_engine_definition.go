package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
)

// Resolve only catalogued checkout definitions, never code from downloaded artifacts.
func loadLocalLogParserEngine(id string) (*workflow.BehaviorDefinedEngine, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		catalogPath := filepath.Join(directory, ".github", "aw", "engines.json")
		content, err := os.ReadFile(catalogPath)
		if err == nil {
			return loadLocalLogParserEngineAt(directory, id, content)
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return nil, nil
		}
		directory = parent
	}
}

func resolvedLocalLogParserDefinition(root, definitionPath, id string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve engine definition root: %w", err)
	}
	resolvedDefinition, err := filepath.EvalSymlinks(definitionPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to resolve engine %q log parser definition: %w", id, err)
	}
	resolvedShared := filepath.Join(resolvedRoot, ".github", "workflows", "shared") + string(filepath.Separator)
	if !strings.HasPrefix(resolvedDefinition, resolvedShared) || filepath.Ext(resolvedDefinition) != ".md" {
		return "", fmt.Errorf("engine %q log parser definition is outside the shared engine directory", id)
	}
	return resolvedDefinition, nil
}

func loadLocalLogParserEngineAt(root, id string, content []byte) (*workflow.BehaviorDefinedEngine, error) {
	var catalog struct {
		Engines []struct {
			ID     string `json:"id"`
			Import string `json:"import"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(content, &catalog); err != nil {
		return nil, err
	}
	for _, entry := range catalog.Engines {
		if entry.ID != id || strings.ContainsAny(id, "/\\.") {
			continue
		}
		const prefix = "github/gh-aw/"
		if !strings.HasPrefix(entry.Import, prefix) {
			continue
		}
		importPath, _, _ := strings.Cut(strings.TrimPrefix(entry.Import, prefix), "@")
		definitionPath := filepath.Join(root, filepath.FromSlash(importPath))
		sharedRoot := filepath.Join(root, ".github", "workflows", "shared") + string(filepath.Separator)
		if !strings.HasPrefix(definitionPath, sharedRoot) || filepath.Ext(definitionPath) != ".md" {
			return nil, fmt.Errorf("engine %q log parser definition is outside the shared engine directory", id)
		}
		resolvedDefinition, err := resolvedLocalLogParserDefinition(root, definitionPath, id)
		if err == nil && resolvedDefinition == "" {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		definitionContent, err := os.ReadFile(resolvedDefinition)
		if err != nil {
			return nil, err
		}
		frontmatter, err := parser.ExtractFrontmatterFromContent(string(definitionContent))
		if err != nil {
			return nil, err
		}
		engineYAML, err := yaml.Marshal(frontmatter.Frontmatter["engine"])
		if err != nil {
			return nil, err
		}
		var definition workflow.EngineDefinition
		if err := yaml.Unmarshal(engineYAML, &definition); err != nil {
			return nil, err
		}
		if definition.ID != id {
			return nil, fmt.Errorf("engine %q log parser definition has id %q", id, definition.ID)
		}
		if definition.Behaviors == nil || definition.Behaviors.LogParser == "" {
			return nil, nil
		}
		return workflow.NewBehaviorDefinedEngine(&definition)
	}
	return nil, nil
}
