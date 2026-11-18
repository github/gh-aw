package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func (c *Compiler) mergeImportedCaches(frontmatter map[string]any, workflowData *WorkflowData, mergedCaches []string) error {
	if len(mergedCaches) == 0 {
		return nil
	}

	var caches []any
	for _, importedCache := range mergedCaches {
		config, err := decodeCacheConfig(importedCache)
		if err != nil {
			return fmt.Errorf("invalid imported cache configuration: %w", err)
		}
		caches, err = appendCacheConfig(caches, config)
		if err != nil {
			return fmt.Errorf("invalid imported cache configuration: %w", err)
		}
	}
	if mainCache, exists := frontmatter["cache"]; exists {
		var err error
		caches, err = appendCacheConfig(caches, mainCache)
		if err != nil {
			return fmt.Errorf("invalid main cache configuration: %w", err)
		}
	}
	if len(caches) > 0 {
		workflowData.Cache = c.extractTopLevelYAMLSection(map[string]any{"cache": caches}, "cache")
	}
	return nil
}

func decodeCacheConfig(source string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(source))
	var config, trailing any
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("trailing or invalid JSON data: %w", err)
	}
	return config, nil
}

func appendCacheConfig(caches []any, config any) ([]any, error) {
	switch value := config.(type) {
	case map[string]any:
		return append(caches, value), nil
	case []any:
		for _, entry := range value {
			if _, ok := entry.(map[string]any); !ok {
				return nil, fmt.Errorf("expected cache entry object, got %T", entry)
			}
			caches = append(caches, entry)
		}
		return caches, nil
	default:
		return nil, fmt.Errorf("expected cache object or array, got %T", config)
	}
}
