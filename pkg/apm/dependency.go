package apm

import (
	"fmt"
)

// DependencyKind distinguishes declaration forms without imposing a transport,
// host, resolver, or installation policy on callers.
type DependencyKind string

const (
	ShorthandDependency DependencyKind = "shorthand"
	GitDependency       DependencyKind = "git"
	RegistryDependency  DependencyKind = "registry"
	LocalDependency     DependencyKind = "local"
)

// Dependency is a typed view of an APM dependency declaration. Shorthand remains
// verbatim because interpreting it requires resolver/default-registry context.
type Dependency struct {
	Kind       DependencyKind `json:"-"`
	Shorthand  string         `json:"-"`
	Git        string         `json:"git"`
	ID         string         `json:"id"`
	Registry   string         `json:"registry"`
	Version    string         `json:"version"`
	Ref        string         `json:"ref"`
	Path       string         `json:"path"`
	Alias      string         `json:"alias"`
	Skills     []string       `json:"skills"`
	Prerelease bool           `json:"prerelease"`
	fields     map[string]any
}

// Field returns a copy of any object-form field, including vendor extensions.
func (d Dependency) Field(name string) any {
	return cloneField(d.fields[name])
}

// Fields returns all original object-form fields as an independent copy.
func (d Dependency) Fields() map[string]any {
	return cloneMap(d.fields)
}

func parseDependencies(fields map[string]any, blockName string) ([]Dependency, error) {
	block, ok := fields[blockName].(map[string]any)
	if !ok {
		return nil, nil
	}
	raw, ok := block["apm"].([]any)
	if !ok {
		return nil, nil
	}
	dependencies := make([]Dependency, 0, len(raw))
	for i, value := range raw {
		if shorthand, ok := value.(string); ok {
			dependencies = append(dependencies, Dependency{Kind: ShorthandDependency, Shorthand: shorthand})
			continue
		}
		fields, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.apm[%d] must be a shorthand string or dependency object", blockName, i)
		}
		dependency, err := decodeFields[Dependency](fields)
		if err != nil {
			return nil, fmt.Errorf("%s.apm[%d]: %w", blockName, i, err)
		}
		switch {
		case dependency.Git != "":
			dependency.Kind = GitDependency
		case dependency.ID != "":
			dependency.Kind = RegistryDependency
		case dependency.Path != "":
			dependency.Kind = LocalDependency
		default:
			return nil, fmt.Errorf("%s.apm[%d] has no dependency source", blockName, i)
		}
		dependency.Alias = trimSchemaWhitespace(dependency.Alias)
		dependency.fields = fields
		dependencies = append(dependencies, dependency)
	}
	return dependencies, nil
}

// APMDependencies returns independent typed declaration views from the selected
// production or development block. It performs no dependency resolution.
func (m *Manifest) APMDependencies(development bool) []Dependency {
	dependencies := m.dependencies
	if development {
		dependencies = m.devDependencies
	}
	result := append([]Dependency(nil), dependencies...)
	for i := range result {
		result[i].Skills = append([]string(nil), result[i].Skills...)
		if fields, ok := cloneField(result[i].fields).(map[string]any); ok {
			result[i].fields = fields
		}
	}
	return result
}

// ServiceDependencies returns preserved MCP declarations from the selected
// block. Registry lookup and runtime-specific configuration belong to callers.
func (m *Manifest) ServiceDependencies(development bool) []any {
	name := "dependencies"
	if development {
		name = "devDependencies"
	}
	block, ok := m.Field(name).(map[string]any)
	if !ok {
		return nil
	}
	if services, ok := block["mcp"].([]any); ok {
		return services
	}
	return nil
}

// TargetRestrictions returns the declared target set. An empty result means no
// manifest restriction; it does not select a deployment target automatically.
func (m *Manifest) TargetRestrictions() []string {
	value := m.Field("targets")
	if value == nil {
		value = m.Field("target")
	}
	switch value := value.(type) {
	case string:
		return []string{value}
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if target, ok := item.(string); ok {
				result = append(result, target)
			}
		}
		return result
	default:
		return nil
	}
}
