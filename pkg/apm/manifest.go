package apm

import (
	"bytes"
	"fmt"
)

// Manifest is a validated OpenAPM document. Exported metadata is a snapshot;
// changing it does not mutate the preserved document. Accessors return copies.
type Manifest struct {
	document        *document
	Name            string
	Version         string
	Warnings        []string
	dependencies    []Dependency
	devDependencies []Dependency
}

// ParseManifest validates the pinned OpenAPM structural contract. Explicit
// $schema input uses OpenAPM's string-scalar profile; schema-less input admits
// conventional YAML 1.2 scalar types, as used by existing APM documents.
func ParseManifest(content []byte) (*Manifest, error) {
	doc, err := parseDocument(content, "apm.yml")
	if err != nil {
		return nil, err
	}
	if err := validateDocument(doc.fields, ManifestSchemaID, manifestSchemaLoader); err != nil {
		return nil, fmt.Errorf("apm.yml: %w", err)
	}
	metadata, err := decodeFields[struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}](doc.fields)
	if err != nil {
		return nil, fmt.Errorf("apm.yml: %w", err)
	}
	result := &Manifest{document: doc, Name: metadata.Name, Version: metadata.Version}
	result.dependencies, err = parseDependencies(doc.fields, "dependencies")
	if err != nil {
		return nil, fmt.Errorf("apm.yml: %w", err)
	}
	result.devDependencies, err = parseDependencies(doc.fields, "devDependencies")
	if err != nil {
		return nil, fmt.Errorf("apm.yml: %w", err)
	}
	if !semverPattern.MatchString(result.Version) {
		result.Warnings = append(result.Warnings, "apm.yml: version is not SemVer 2.0.0")
	}
	if _, exists := doc.fields["workspaces"]; exists {
		result.Warnings = append(result.Warnings, "apm.yml: workspaces is reserved for OpenAPM v0.2")
	}
	return result, nil
}

// Field returns a copy of a manifest field, including unknown extensions.
func (m *Manifest) Field(name string) any {
	return cloneField(m.document.fields[name])
}

// Bytes returns a copy of the original YAML, including extensions and comments.
func (m *Manifest) Bytes() []byte {
	return bytes.Clone(m.document.raw)
}
