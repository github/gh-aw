package actionslanguageservice

import (
	"maps"
	"sync"
)

// ValidateWorkflowWithCacheMode validates compiler output with the cache-mode
// field supported by GitHub Actions but absent from the pinned upstream schema.
// The ordinary parser and its conformance schema remain unchanged.
func ValidateWorkflowWithCacheMode(name, content string) error {
	schema, err := loadCacheModeSchema()
	if err != nil {
		return err
	}
	workflow, err := parseWorkflow(name, content, schema)
	if err != nil {
		return err
	}
	return validateConditions(name, workflow)
}

var loadCacheModeSchema = sync.OnceValues(func() (map[string]definition, error) {
	upstream, err := loadSchema()
	if err != nil {
		return nil, err
	}
	schema := maps.Clone(upstream)
	schema["cache-mode"] = definition{AllowedValues: []string{"read", "write", "write-only", "none"}}
	for _, name := range []string{"workflow-root", "workflow-root-strict", "job-factory", "workflow-job"} {
		d := schema[name]
		mapping := *d.Mapping
		mapping.Properties = maps.Clone(mapping.Properties)
		mapping.Properties["cache-mode"] = property{Type: "cache-mode"}
		d.Mapping = &mapping
		schema[name] = d
	}
	return schema, nil
})
