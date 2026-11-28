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
		Description:  "Adds bundled connector short-name imports, renames supported ado-* outputs to azure-devops-*, and moves linear-token secret references to writer-secret. Explicit resource grants and unsupported legacy options still require review.",
		IntroducedIn: "1.0.0",
		Guided:       true,
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			outputs, ok := frontmatter["safe-outputs"].(map[string]any)
			if !ok || len(outputs) == 0 {
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
				migrated := false
				for _, provider := range providers {
					selected := slices.DeleteFunc(slices.Clone(provider.outputs), func(key string) bool {
						_, exists := outputs[key]
						return !exists
					})
					if len(selected) > 0 {
						if err := migrateLegacyConnector(f, provider.name, selected); err != nil {
							return err
						}
						migrated = true
					}
				}
				if !migrated {
					return nil
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

func migrateLegacyConnector(f *codemod.Frontmatter, provider string, selected []string) error {
	path, entries, index, entry, err := findOrCreateConnectorImport(f, provider)
	if err != nil {
		return err
	}
	inputField := ""
	requiredInputs := requiredConnectorInputs(provider, selected)
	hasToken, err := addLegacyLinearTokenInput(f, provider, requiredInputs)
	if err != nil {
		return err
	}
	if len(requiredInputs) > 0 {
		entry, inputField, err = ensureConnectorInputs(entry, requiredInputs)
		if err != nil {
			return err
		}
	}
	entryIndex := index
	if index >= 0 {
		entries = slices.Replace(entries, index, index+1, entry)
	} else {
		entryIndex = len(entries)
		entries = append(entries, entry)
	}
	if err := f.Set(path, entries); err != nil {
		return err
	}
	if provider == "linear" && hasToken {
		destination := make([]any, 0, len(path)+3)
		for _, key := range path {
			destination = append(destination, key)
		}
		destination = append(destination, entryIndex)
		destination = append(destination, inputField, "writer-secret")
		if err := transferLegacyLinearToken(f, destination); err != nil {
			return err
		}
	}
	if provider == "azure-devops" {
		return renameLegacyADOOutputs(f, selected)
	}
	return nil
}

func findOrCreateConnectorImport(f *codemod.Frontmatter, provider string) ([]string, []any, int, any, error) {
	path, entries, err := legacyConnectorImportEntries(f)
	if err != nil {
		return nil, nil, -1, nil, err
	}
	index, count := -1, 0
	var entry any
	for candidateIndex, candidate := range entries {
		if isConnectorImport(candidate, provider) {
			index, entry = candidateIndex, candidate
			count++
		}
	}
	if count > 1 {
		return nil, nil, -1, nil, fmt.Errorf("cannot migrate %s: multiple connector imports are already registered; keep one before running gh aw fix", provider)
	}
	if count == 0 {
		if _, found := f.Lookup("connectors", provider); found {
			return nil, nil, -1, nil, fmt.Errorf("cannot migrate %s: an inline connector registration already exists; bind its inputs manually before running gh aw fix", provider)
		}
		entry = "connector:" + provider
	}
	return path, entries, index, entry, nil
}

func addLegacyLinearTokenInput(f *codemod.Frontmatter, provider string, inputs map[string]any) (bool, error) {
	if provider != "linear" {
		return false, nil
	}
	token, exists := f.Lookup("safe-outputs", "linear-token")
	if !exists {
		return false, nil
	}
	if token == nil {
		return false, errors.New("cannot migrate linear-token: value is null; bind a single secrets.NAME reference manually")
	}
	secretReference, ok := token.(string)
	if !ok || !connectorSecretReference.MatchString(secretReference) {
		return false, errors.New("cannot migrate linear-token: use a single secrets.NAME reference, then bind its name through the Linear import writer-secret")
	}
	inputs["writer-secret"] = connectorSecretReference.ReplaceAllString(secretReference, "$1")
	return true, nil
}

func transferLegacyLinearToken(f *codemod.Frontmatter, destination []any) error {
	if err := f.TransferInlineComment([]string{"safe-outputs", "linear-token"}, destination); err != nil {
		return err
	}
	return f.Remove("safe-outputs", "linear-token")
}

func requiredConnectorInputs(provider string, selected []string) map[string]any {
	inputs := make(map[string]any)
	switch provider {
	case "jira":
		inputs["site"] = ""
		if slices.Contains(selected, "jira-create-issue") {
			inputs["issue-types"] = []any{}
		}
	case "azure-devops":
		inputs["organization"] = ""
		inputs["project"] = ""
		inputs["project-name"] = ""
		inputs["work-item"] = ""
		inputs["related-work-item"] = ""
		for _, output := range selected {
			if output == "ado-assign-work-item" {
				inputs["assignee"] = ""
			}
		}
	}
	return inputs
}

func ensureConnectorInputs(entry any, required map[string]any) (any, string, error) {
	mapping := map[string]any{}
	sourceField := "uses"
	if existing, ok := entry.(map[string]any); ok {
		mapping = maps.Clone(existing)
		if _, hasUses := mapping["uses"]; !hasUses {
			if _, hasPath := mapping["path"]; hasPath {
				sourceField = "path"
			}
		}
		if _, hasWith := mapping["with"]; hasWith {
			if _, hasInputs := mapping["inputs"]; hasInputs {
				return nil, "", errors.New("cannot migrate connector: import has both with and inputs mappings")
			}
		}
	} else {
		mapping["uses"] = entry
	}

	inputField := "with"
	if sourceField == "path" {
		inputField = "inputs"
	}
	if _, hasInputs := mapping["inputs"]; hasInputs {
		inputField = "inputs"
	}
	if _, hasWith := mapping["with"]; hasWith {
		inputField = "with"
	}
	var inputs map[string]any
	if value, exists := mapping[inputField]; exists {
		existing, ok := value.(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("cannot migrate connector: import %s must be a mapping", inputField)
		}
		inputs = maps.Clone(existing)
	} else {
		inputs = make(map[string]any)
	}
	for key, value := range required {
		if current, exists := inputs[key]; exists {
			if key == "writer-secret" && current != value {
				return nil, "", errors.New("cannot migrate linear-token: existing writer-secret conflicts with the legacy token")
			}
			if current != nil {
				continue
			}
		}
		inputs[key] = value
	}
	mapping[inputField] = inputs
	return mapping, inputField, nil
}

func isConnectorImport(entry any, provider string) bool {
	source, ok := entry.(string)
	if mapping, isMapping := entry.(map[string]any); isMapping {
		source, ok = mapping["uses"].(string)
		if !ok {
			source, ok = mapping["path"].(string)
		}
	}
	if !ok {
		return false
	}

	source = strings.TrimSpace(source)
	if source == "connector:"+provider {
		return true
	}
	if remotePath, found := strings.CutPrefix(source, "github/gh-aw/"); found {
		source = remotePath
		if at := strings.IndexByte(source, '@'); at >= 0 {
			source = source[:at]
		}
	}
	for strings.HasPrefix(source, "./") {
		source = strings.TrimPrefix(source, "./")
	}
	source = strings.TrimPrefix(source, "/")

	importPath := repositoryConnectorDirectory + provider + ".md"
	return source == importPath || source == strings.TrimSuffix(importPath, ".md") ||
		source == "connectors/"+provider+".md" || source == "connectors/"+provider
}

func legacyConnectorImportEntries(f *codemod.Frontmatter) ([]string, []any, error) {
	path := []string{"imports"}
	imports, exists := f.Lookup(path...)
	if _, ok := imports.(map[string]any); ok {
		path = append(path, "aw")
		imports, exists = f.Lookup(path...)
	}
	if !exists || imports == nil {
		return path, nil, nil
	}
	entries, ok := imports.([]any)
	if !ok {
		return nil, nil, errors.New("cannot migrate connectors: imports or imports.aw must be a sequence")
	}
	return path, slices.Clone(entries), nil
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
