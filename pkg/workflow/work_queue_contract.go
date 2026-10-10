package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
)

func workQueueLogicalContract(path string) (string, error) {
	return workQueueLogicalContractWithCache(path, parser.NewImportCache(filepath.Dir(path)))
}

func workQueueLogicalContractWithCache(path string, cache *parser.ImportCache) (string, error) {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("work-queue: read worker contract %s: %w", path, err)
	}
	return workQueueLogicalContractFromContentWithCache(path, string(content), cache)
}

func workQueueLogicalContractFromContent(path, content string) (string, error) {
	return workQueueLogicalContractFromContentWithCache(path, content, parser.NewImportCache(filepath.Dir(path)))
}

func workQueueLogicalContractFromContentWithCache(path, content string, cache *parser.ImportCache) (string, error) {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", fmt.Errorf("work-queue: parse worker contract %s: %w", path, err)
	}
	imports, err := parser.ProcessImportsFromFrontmatterWithSource(result.Frontmatter, filepath.Dir(path), cache, path, content)
	if err != nil {
		return "", fmt.Errorf("work-queue: resolve imported worker contract %s: %w", path, err)
	}
	imported, err := importedWorkQueueContract(imports)
	if err != nil {
		return "", fmt.Errorf("work-queue: normalize imported worker contract %s: %w", path, err)
	}
	contract := map[string]any{
		"version":  1,
		"claims":   3,
		"local":    workQueueAuthorityFields(result.Frontmatter),
		"imported": imported,
		"mounts":   imports.MergedSandboxAgentMounts,
	}
	encoded, err := json.Marshal(normalizeWorkQueueContract(contract))
	if err != nil {
		return "", fmt.Errorf("work-queue: encode worker contract %s: %w", path, err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func importedWorkQueueContract(imports *parser.ImportsResult) (map[string]any, error) {
	fields := map[string][]string{
		"tools":        {imports.MergedTools},
		"permissions":  {imports.MergedPermissions},
		"safe_outputs": imports.MergedSafeOutputs,
		"mcp_scripts":  imports.MergedMCPScripts,
		"network":      {imports.MergedNetwork},
		"checkout":     {imports.MergedCheckout},
		"mcp_servers":  {imports.MergedMCPServers},
	}
	result := map[string]any{}
	for key, fragments := range fields {
		values := []any{}
		for _, fragment := range fragments {
			decoder := json.NewDecoder(strings.NewReader(fragment))
			for {
				var value any
				err := decoder.Decode(&value)
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				switch key {
				case "mcp_scripts":
					value = workQueueScriptContracts(value)
				case "safe_outputs":
					value = workQueueSafeOutputContract(value)
				}
				values = append(values, value)
			}
		}
		result[key] = values
	}
	return result, nil
}

func workQueueAuthorityFields(frontmatter map[string]any) map[string]any {
	fields := map[string]any{}
	for _, name := range []string{"permissions", "tools", "safe-outputs", "network", "checkout", "mcp-servers", "mcp-scripts", "sandbox"} {
		if value, configured := frontmatter[name]; configured {
			switch name {
			case "mcp-scripts":
				value = workQueueScriptContracts(value)
			case "safe-outputs":
				value = workQueueSafeOutputContract(value)
			}
			fields[name] = value
		}
	}
	if triggers, ok := frontmatter["on"].(map[string]any); ok {
		if dispatch, ok := triggers["workflow_dispatch"].(map[string]any); ok {
			fields["inputs"] = dispatch["inputs"]
		}
	}
	return fields
}

func workQueueSafeOutputContract(value any) any {
	config, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := make(map[string]any, len(config))
	for name, field := range config {
		if name == "github-token" {
			continue
		}
		if name == "scripts" {
			field = workQueueScriptContracts(field)
		} else if settings, ok := field.(map[string]any); ok {
			copy := make(map[string]any, len(settings))
			for key, value := range settings {
				if key != "github-token" {
					copy[key] = value
				}
			}
			field = copy
		}
		result[name] = field
	}
	return result
}

func workQueueScriptContracts(value any) any {
	scripts, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := make(map[string]any, len(scripts))
	for name, value := range scripts {
		config, ok := value.(map[string]any)
		if !ok {
			result[name] = value
			continue
		}
		contract := make(map[string]any, len(config))
		for field, value := range config {
			if field != "run" && field != "script" && field != "description" {
				contract[field] = value
			}
		}
		result[name] = contract
	}
	return result
}

func normalizeWorkQueueContract(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, nested := range value {
			result[key] = normalizeWorkQueueContract(nested)
		}
		return result
	case []string:
		result := make([]any, 0, len(value))
		for _, nested := range value {
			result = append(result, normalizeWorkQueueContract(nested))
		}
		return result
	case []any:
		result := make([]any, 0, len(value))
		for _, nested := range value {
			result = append(result, normalizeWorkQueueContract(nested))
		}
		return result
	default:
		return value
	}
}
