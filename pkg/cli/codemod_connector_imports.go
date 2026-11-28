package cli

import (
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
					if !slices.ContainsFunc(provider.outputs, func(key string) bool {
						_, exists := outputs[key]
						return exists
					}) {
						continue
					}
					path := ".github/workflows/connectors/" + provider.name + ".md"
					imports, exists := f.Lookup("imports")
					var entries []any
					if exists {
						var ok bool
						entries, ok = imports.([]any)
						if !ok {
							return fmt.Errorf("cannot migrate %s: imports must be a sequence", provider.name)
						}
						entries = slices.Clone(entries)
					}
					index := slices.IndexFunc(entries, func(entry any) bool {
						if mapping, ok := entry.(map[string]any); ok {
							entry = mapping["uses"]
						}
						uses, ok := entry.(string)
						return ok && (uses == path || uses == strings.TrimSuffix(path, ".md") ||
							strings.HasPrefix(uses, "github/gh-aw/"+path+"@"))
					})
					var secretName string
					if provider.name == "linear" {
						if token, exists := outputs["linear-token"]; exists && token != nil {
							text, ok := token.(string)
							match := connectorSecretReference.FindStringSubmatch(text)
							if !ok || match == nil {
								return fmt.Errorf("cannot migrate linear-token: use a single secrets.NAME reference, then bind its name through the Linear import writer-secret")
							}
							secretName = match[1]
						}
					}
					if index < 0 {
						entry := any(path)
						if provider.name == "azure-devops" {
							entry = map[string]any{"uses": path, "with": map[string]any{
								"organization": "", "project": "", "project-name": "",
							}}
						}
						if secretName != "" {
							entry = map[string]any{"uses": path, "with": map[string]any{"writer-secret": secretName}}
						}
						entries = append(slices.Clone(entries), entry)
					} else if secretName != "" {
						entry := map[string]any{"uses": entries[index]}
						if mapping, ok := entries[index].(map[string]any); ok {
							entry = maps.Clone(mapping)
						}
						inputs, exists := entry["with"]
						if !exists {
							inputs = map[string]any{}
							entry["with"] = inputs
						}
						mapping, ok := inputs.(map[string]any)
						if !ok {
							return fmt.Errorf("cannot migrate linear-token: import with must be a mapping")
						}
						if current, exists := mapping["writer-secret"]; exists && current != secretName {
							return fmt.Errorf("cannot migrate linear-token: existing writer-secret conflicts with the legacy token")
						}
						mapping = maps.Clone(mapping)
						mapping["writer-secret"] = secretName
						entry["with"] = mapping
						entries[index] = entry
					}
					if err := f.Set([]string{"imports"}, entries); err != nil {
						return err
					}
					if provider.name == "linear" {
						if err := f.Remove("safe-outputs", "linear-token"); err != nil {
							return err
						}
					}
					if provider.name == "azure-devops" {
						for _, key := range provider.outputs {
							destination := strings.Replace(key, "ado-", "azure-devops-", 1)
							if _, found := outputs[key]; !found {
								continue
							}
							if _, found := f.Lookup("safe-outputs", destination); found {
								return fmt.Errorf("cannot migrate %s: %s already exists", key, destination)
							}
							if err := f.Rename([]string{"safe-outputs", key}, destination); err != nil {
								return err
							}
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
