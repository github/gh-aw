package workflow

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
)

// Resolve registrations through the existing import pipeline before native schema
// validation. The normal engine import pass reuses the same confined import cache.
func (c *Compiler) normalizeImportedConnectors(frontmatter map[string]any, path, content string) error {
	if !frontmatterDeclaresImports(frontmatter) {
		return nil
	}
	imports, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, filepath.Dir(path), c.getSharedImportCache(), path, content)
	if err != nil {
		return err
	}
	if len(imports.Connectors) == 0 {
		return nil
	}
	registrations, err := importedConnectorRegistrations(frontmatter, imports.Connectors)
	if err != nil {
		return err
	}
	frontmatter["connectors"] = registrations
	return nil
}

func importedConnectorRegistrations(frontmatter map[string]any, imports []parser.ConnectorImport) (map[string]any, error) {
	registrations := map[string]any{}
	configs := map[string]map[string]any{}
	if existing, present := frontmatter["connectors"]; present {
		values, ok := existing.(map[string]any)
		if !ok {
			return nil, errors.New("connectors must be an object")
		}
		maps.Copy(registrations, values)
	}
	for _, imported := range imports {
		for _, alias := range sliceutil.SortedKeys(imported.Registrations) {
			if _, duplicate := registrations[alias]; duplicate {
				return nil, fmt.Errorf("import %q: connector %q is already registered; use distinct aliases instead of overriding service authority", imported.Source, alias)
			}
			value, ok := imported.Registrations[alias].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("import %q: connector %q must be an object", imported.Source, alias)
			}
			config := maps.Clone(value)
			registrations[alias] = config
			configs[alias] = config
		}
	}
	if err := applyImportedConnectorSelections(frontmatter, configs); err != nil {
		return nil, err
	}
	return registrations, nil
}

func applyImportedConnectorSelections(frontmatter map[string]any, configs map[string]map[string]any) error {
	aliases := sliceutil.SortedKeys(configs)
	if err := normalizeImportedTriggerNames(frontmatter, aliases); err != nil {
		return err
	}
	if err := validateQualifiedConnectorNames(frontmatter, aliases); err != nil {
		return err
	}
	for _, alias := range aliases {
		for _, section := range []string{"tools", "context"} {
			consumeImportedConnectorSection(frontmatter, configs[alias], section, alias)
		}
		for _, section := range []string{"on", "safe-outputs"} {
			if err := consumeQualifiedConnectorSection(frontmatter, configs[alias], section, alias); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeImportedTriggerNames(frontmatter map[string]any, aliases []string) error {
	var names []string
	switch value := frontmatter["on"].(type) {
	case string:
		names = []string{value}
	case []string:
		names = value
	case []any:
		for _, item := range value {
			name, ok := item.(string)
			if !ok {
				return errors.New("on trigger lists must contain event names")
			}
			names = append(names, name)
		}
	default:
		return nil
	}
	hasConnector := false
	for _, name := range names {
		for _, alias := range aliases {
			hasConnector = hasConnector || strings.HasPrefix(name, alias+"-")
		}
	}
	if !hasConnector {
		return nil
	}
	events := map[string]any{}
	for _, name := range names {
		if _, duplicate := events[name]; duplicate {
			return fmt.Errorf("duplicate trigger selection %q", name)
		}
		events[name] = nil
	}
	frontmatter["on"] = events
	return nil
}

func validateQualifiedConnectorNames(frontmatter map[string]any, aliases []string) error {
	builtins, err := parser.GetSafeOutputFieldKeys()
	if err != nil {
		return err
	}
	for _, section := range []string{"on", "safe-outputs"} {
		values, ok := frontmatter[section].(map[string]any)
		if !ok {
			continue
		}
		for _, name := range sliceutil.SortedKeys(values) {
			var owner string
			for _, alias := range aliases {
				if !strings.HasPrefix(name, alias+"-") {
					continue
				}
				if owner != "" {
					return fmt.Errorf("%s.%s has ambiguous connector prefixes %q and %q; use unambiguous connector ids", section, name, owner, alias)
				}
				owner = alias
			}
			if owner == "" {
				continue
			}
			if section == "on" && (slices.Contains(validGitHubEventTypes, name) || ghAwOnSectionKeys[name]) {
				return fmt.Errorf("connector %q cannot shadow built-in trigger field %q", owner, name)
			}
			// Existing Linear/Jira/ADO operations target third-party APIs, not GitHub.
			_, registeredOperation := safeOutputHandlersByKey[name]
			legacyProvider := slices.Contains([]string{"linear", "jira", "ado"}, owner) && registeredOperation
			if section == "safe-outputs" && slices.Contains(builtins, name) && !legacyProvider {
				return fmt.Errorf("connector %q cannot shadow built-in GitHub safe output %q", owner, name)
			}
		}
	}
	return nil
}

func consumeQualifiedConnectorSection(frontmatter, config map[string]any, section, alias string) error {
	values, ok := frontmatter[section].(map[string]any)
	if !ok {
		return nil
	}
	remaining := maps.Clone(values)
	var names []string
	for _, key := range sliceutil.SortedKeys(values) {
		operation, qualified := strings.CutPrefix(key, alias+"-")
		if !qualified {
			continue
		}
		options, object := values[key].(map[string]any)
		if values[key] != nil && (!object || len(options) != 0) {
			return fmt.Errorf("%s.%s accepts null or {}; connector behavior is declared in its spec", section, key)
		}
		names = append(names, operation)
		delete(remaining, key)
	}
	if len(names) == 0 {
		return nil
	}
	config[section] = names
	if len(remaining) == 0 {
		delete(frontmatter, section)
	} else {
		frontmatter[section] = remaining
	}
	return nil
}

func consumeImportedConnectorSection(frontmatter, config map[string]any, section, alias string) {
	values, ok := frontmatter[section].(map[string]any)
	if !ok {
		return
	}
	if selected, exists := values[alias]; exists {
		config[section] = selected
		values = maps.Clone(values)
		delete(values, alias)
		if len(values) == 0 {
			delete(frontmatter, section)
		} else {
			frontmatter[section] = values
		}
	}
}
