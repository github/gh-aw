package cli

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/codemod"
)

var connectorImportsCodemodLog = logger.New("cli:codemod_connector_imports")

var connectorSecretReference = regexp.MustCompile(`^\$\{\{\s*secrets\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}$`)

func getLegacyConnectorImportsCodemod() Codemod {
	return Codemod{
		ID:           "legacy-connector-imports",
		Name:         "Import connectors for legacy Linear, Jira, and ADO safe outputs",
		Description:  "Adds connector imports, renames supported ado-* outputs to azure-devops-*, and moves linear-token secret references to writer-secret. Explicit resource grants and unsupported legacy options still require review.",
		IntroducedIn: "1.0.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			outputs, ok := frontmatter["safe-outputs"].(map[string]any)
			if !ok {
				return content, false, nil
			}
			providers := []struct {
				name    string
				outputs []string
			}{
				{"linear", []string{"linear-create-issue", "linear-update-issue", "linear-add-comment", "linear-token"}},
				{"jira", []string{"jira-create-issue", "jira-update-issue", "jira-add-comment", "jira-add-label"}},
				{"azure-devops", []string{"ado-create-work-item", "ado-update-work-item", "ado-comment-on-work-item", "ado-assign-work-item", "ado-link-work-items"}},
			}
			result, applied, err := codemod.Apply(content, func(f *codemod.Frontmatter) error {
				for _, provider := range providers {
					if slices.ContainsFunc(provider.outputs, func(key string) bool {
						_, exists := outputs[key]
						return exists
					}) {
						if err := migrateLegacyConnector(f, provider.name, provider.outputs); err != nil {
							return err
						}
					}
				}
				return f.PruneEmpty("safe-outputs")
			})
			if applied {
				connectorImportsCodemodLog.Print("Migrated legacy safe outputs to connector imports; review explicit resource grants")
			}
			return result, applied, err
		},
	}
}

func migrateLegacyConnector(f *codemod.Frontmatter, provider string, keys []string) error {
	path, entries, err := legacyConnectorImportEntries(f)
	if err != nil {
		return err
	}
	importPath := repositoryConnectorDirectory + provider + ".md"
	index := slices.IndexFunc(entries, func(entry any) bool {
		if mapping, ok := entry.(map[string]any); ok {
			entry = mapping["uses"]
		}
		uses, ok := entry.(string)
		if !ok {
			return false
		}
		local := strings.TrimPrefix(strings.TrimSpace(uses), "./")
		return local == importPath || local == strings.TrimSuffix(importPath, ".md") ||
			local == "connectors/"+provider+".md" || local == "connectors/"+provider ||
			strings.HasPrefix(local, "github/gh-aw/"+importPath+"@")
	})
	var entry any = importPath
	if index >= 0 && index < len(entries) {
		entry = entries[index]
	} else if provider == "azure-devops" {
		entry = map[string]any{"uses": importPath, "with": map[string]any{
			"organization": "", "project": "", "project-name": "",
		}}
	}
	if provider == "linear" {
		entry, err = migrateLegacyLinearToken(f, entry)
		if err != nil {
			return err
		}
	}
	if index >= 0 && index < len(entries) {
		entries[index] = entry
	} else {
		entries = append(entries, entry)
	}
	if err := f.Set(path, entries); err != nil {
		return err
	}
	if provider == "linear" {
		return f.Remove("safe-outputs", "linear-token")
	}
	if provider == "azure-devops" {
		return renameLegacyADOOutputs(f, keys)
	}
	return nil
}

func legacyConnectorImportEntries(f *codemod.Frontmatter) ([]string, []any, error) {
	path := []string{"imports"}
	imports, exists := f.Lookup(path...)
	if _, ok := imports.(map[string]any); ok {
		path = append(path, "aw")
		imports, exists = f.Lookup(path...)
	}
	if !exists {
		return path, nil, nil
	}
	entries, ok := imports.([]any)
	if !ok {
		return nil, nil, errors.New("cannot migrate connectors: imports or imports.aw must be a sequence")
	}
	return path, slices.Clone(entries), nil
}

func migrateLegacyLinearToken(f *codemod.Frontmatter, entry any) (any, error) {
	token, exists := f.Lookup("safe-outputs", "linear-token")
	if !exists || token == nil {
		return entry, nil
	}
	text, ok := token.(string)
	if !ok || !connectorSecretReference.MatchString(text) {
		return nil, errors.New("cannot migrate linear-token: use a single secrets.NAME reference, then bind its name through the Linear import writer-secret")
	}
	secretName := connectorSecretReference.ReplaceAllString(text, "$1")
	mapping := map[string]any{"uses": entry}
	if existing, ok := entry.(map[string]any); ok {
		mapping = maps.Clone(existing)
	}
	inputs := map[string]any{}
	if value, exists := mapping["with"]; exists {
		existing, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("cannot migrate linear-token: import with must be a mapping")
		}
		inputs = maps.Clone(existing)
	}
	if current, exists := inputs["writer-secret"]; exists && current != secretName {
		return nil, errors.New("cannot migrate linear-token: existing writer-secret conflicts with the legacy token")
	}
	inputs["writer-secret"] = secretName
	mapping["with"] = inputs
	return mapping, nil
}

func renameLegacyADOOutputs(f *codemod.Frontmatter, keys []string) error {
	for _, key := range keys {
		if _, found := f.Lookup("safe-outputs", key); !found {
			continue
		}
		destination := strings.Replace(key, "ado-", "azure-devops-", 1)
		if _, found := f.Lookup("safe-outputs", destination); found {
			return fmt.Errorf("cannot migrate %s: %s already exists", key, destination)
		}
		if err := f.Rename([]string{"safe-outputs", key}, destination); err != nil {
			return err
		}
	}
	return nil
}
