// This file provides the built-in engine definition loader.
//
// Built-in engine definitions are stored as shared agentic workflow Markdown files
// embedded in the binary. Each file uses YAML frontmatter with a top-level "engine:"
// key. The engine definition form is validated as part of the shared workflow schema
// when files are processed as imports during compilation.
//
// # Embedded Resources
//
// Engine Markdown files live in data/engines/*.md and are embedded at compile time
// via the //go:embed directive below. Adding a new built-in engine requires only a
// new .md file in that directory — no Go code changes are needed.
//
// # Builtin Virtual FS
//
// Each embedded .md file is also registered in the parser's builtin virtual FS under
// the path "@builtin:engines/<id>.md". This allows the compiler to inject the file
// as an import when the short-form "engine: <id>" is encountered.
package workflow

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
)

var engineDefinitionLoaderLog = logger.New("workflow:engine_definition_loader")

//go:embed data/engines/*.md
var builtinEngineFS embed.FS

// engineDefinitionFile is the on-disk wrapper that holds the engine definition
// under the top-level "engine" key.
type engineDefinitionFile struct {
	Engine EngineDefinition `yaml:"engine"`
}

// extractMarkdownFrontmatterYAML extracts the YAML content between the first pair of
// "---" delimiters in a Markdown file. Both LF and CRLF line endings are supported.
func extractMarkdownFrontmatterYAML(content []byte) ([]byte, error) {
	s := string(content)
	const sep = "---"

	// Find the opening delimiter
	start := strings.Index(s, sep)
	if start == -1 {
		return nil, errors.New("no frontmatter opening delimiter found")
	}
	s = s[start+len(sep):]

	// Find the closing delimiter, supporting both LF and CRLF line endings.
	endLF := strings.Index(s, "\n"+sep)
	endCRLF := strings.Index(s, "\r\n"+sep)

	end := -1
	switch {
	case endLF >= 0 && endCRLF >= 0:
		end = min(endLF, endCRLF)
	case endLF >= 0:
		end = endLF
	case endCRLF >= 0:
		end = endCRLF
	}

	if end == -1 {
		return nil, errors.New("no frontmatter closing delimiter found")
	}
	return []byte(strings.TrimSpace(s[:end])), nil
}

// builtinEnginePath returns the canonical builtin virtual-FS path for an engine id.
func builtinEnginePath(engineID string) string {
	return parser.BuiltinPathPrefix + "engines/" + engineID + ".md"
}

// loadBuiltinEngineDefinitions reads all *.md files from the embedded data/engines/
// directory, parses each EngineDefinition from its frontmatter, and registers the file
// content in the parser's builtin virtual FS.
// It panics on parse errors to surface misconfigured built-in definitions early.
func loadBuiltinEngineDefinitions() []*EngineDefinition {
	engineDefinitionLoaderLog.Print("Loading built-in engine definitions from embedded Markdown files")

	var definitions []*EngineDefinition

	err := fs.WalkDir(builtinEngineFS, "data/engines", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		def, err := loadBuiltinEngineDefinition(path)
		if err != nil {
			return err
		}
		definitions = append(definitions, def)
		return nil
	})

	if err != nil {
		panic(fmt.Sprintf("failed to walk embedded engine definitions directory: %v", err))
	}

	engineDefinitionLoaderLog.Printf("Loaded %d built-in engine definitions", len(definitions))
	return definitions
}

func loadBuiltinEngineDefinition(path string) (*EngineDefinition, error) {
	data, readErr := builtinEngineFS.ReadFile(path)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read embedded engine file %s: %w", path, readErr)
	}

	// Extract the frontmatter YAML from the Markdown file.
	frontmatterYAML, fmErr := extractMarkdownFrontmatterYAML(data)
	if fmErr != nil {
		return nil, fmt.Errorf("failed to extract frontmatter from %s: %w", path, fmErr)
	}

	// Parse the engine definition from the frontmatter.
	var wrapper engineDefinitionFile
	if parseErr := yaml.Unmarshal(frontmatterYAML, &wrapper); parseErr != nil {
		return nil, fmt.Errorf("failed to parse embedded engine file %s: %w", path, parseErr)
	}

	def := wrapper.Engine

	// Default runtime-id to engine id when omitted.
	if def.RuntimeID == "" {
		def.RuntimeID = def.ID
	}

	// Register the full .md content in the parser's builtin virtual FS so the
	// file can be resolved and read during import processing.
	parser.RegisterBuiltinVirtualFile(builtinEnginePath(def.ID), data)

	// Register the JSON key that the import processor produces for this built-in
	// so that parseEngineDefinitionFromJSON will cache it. The import processor
	// marshals the raw "engine:" value (a map[string]any) to JSON; we replicate
	// that here so the cache key matches exactly.
	var rawFM map[string]any
	if jsonErr := yaml.Unmarshal(frontmatterYAML, &rawFM); jsonErr != nil {
		engineDefinitionLoaderLog.Printf("Warning: failed to unmarshal frontmatter for cache-key registration of %s: %v", path, jsonErr)
	} else if engineVal, ok := rawFM["engine"]; ok {
		if jsonKey, jsonErr := json.Marshal(engineVal); jsonErr != nil {
			engineDefinitionLoaderLog.Printf("Warning: failed to marshal engine value to JSON for cache-key registration of %s: %v", path, jsonErr)
		} else {
			registerBuiltinEngineDefinitionJSON(string(jsonKey))
		}
	}

	engineDefinitionLoaderLog.Printf("Loaded built-in engine definition: id=%s runtime-id=%s", def.ID, def.RuntimeID)
	return &def, nil
}
