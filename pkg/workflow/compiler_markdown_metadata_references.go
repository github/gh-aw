package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Catalog values retain only information absent from the two existing headers.
// References use JSON pointers so authored field names cannot collide with them.
func deduplicateMarkdownMetadata(header string, manifest *GHAWManifest) error {
	index := map[string]string{}
	for line := range strings.SplitSeq(header, "\n") {
		if source, found := strings.CutPrefix(line, "# gh-aw-metadata: "); found {
			var value any
			decoder := json.NewDecoder(strings.NewReader(source))
			if err := decoder.Decode(&value); err != nil {
				return err
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				if err != nil {
					return fmt.Errorf("invalid trailing metadata header JSON: %w", err)
				}
				return errors.New("metadata header must contain exactly one JSON value")
			}
			indexMarkdownMetadataValues(index, value, "/metadata")
		}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return err
	}
	delete(value, "markdown_metadata")
	indexMarkdownMetadataValues(index, value, "/manifest")
	for i, record := range manifest.MarkdownMetadata.Records {
		if record.Error != "" {
			continue
		}
		var value any
		if err := json.Unmarshal(record.Value, &value); err != nil {
			return err
		}
		record.References = map[string]string{}
		value = deduplicateMarkdownMetadataValue(value, "", index, record.References)
		record.Value, err = json.Marshal(value)
		if err != nil {
			return err
		}
		manifest.MarkdownMetadata.Records[i] = record
	}
	return nil
}

func metadataPointerPart(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func appendMetadataPointer(pointer, key string) string {
	return pointer + "/" + metadataPointerPart(key) //nolint:manualpathconcat // JSON pointers must preserve empty and dot segments; path.Join would corrupt them.
}

func indexMarkdownMetadataValues(index map[string]string, value any, pointer string) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	// Empty containers, booleans and numbers are smaller than a reference and do
	// not carry duplicated field information worth moving out of the catalog.
	if text, ok := value.(string); ok && text != "" {
		if _, exists := index[string(encoded)]; !exists {
			index[string(encoded)] = pointer
		}
	}
	switch item := value.(type) {
	case map[string]any:
		if len(item) > 0 {
			if _, exists := index[string(encoded)]; !exists {
				index[string(encoded)] = pointer
			}
		}
		for _, key := range sortedMetadataKeys(item) {
			indexMarkdownMetadataValues(index, item[key], appendMetadataPointer(pointer, key))
		}
	case []any:
		if len(item) > 0 {
			if _, exists := index[string(encoded)]; !exists {
				index[string(encoded)] = pointer
			}
		}
		for i, child := range item {
			indexMarkdownMetadataValues(index, child, fmt.Sprintf("%s/%d", pointer, i))
		}
	}
}

func sortedMetadataKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func deduplicateMarkdownMetadataValue(value any, pointer string, index, references map[string]string) any {
	encoded, err := json.Marshal(value)
	if err == nil {
		if source, exists := index[string(encoded)]; exists {
			references[pointer] = source
			return nil
		}
	}
	switch item := value.(type) {
	case map[string]any:
		for _, key := range sortedMetadataKeys(item) {
			child := appendMetadataPointer(pointer, key)
			item[key] = deduplicateMarkdownMetadataValue(item[key], child, index, references)
			if _, referenced := references[child]; referenced {
				delete(item, key)
			}
		}
	case []any:
		for i, child := range item {
			item[i] = deduplicateMarkdownMetadataValue(child, fmt.Sprintf("%s/%d", pointer, i), index, references)
		}
	}
	return value
}
