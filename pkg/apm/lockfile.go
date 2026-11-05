package apm

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
)

// LockedDependency exposes standard source, pin and deployment metadata.
// Unknown fields and extensions remain available through Field.
type LockedDependency struct {
	RepoURL                string            `json:"repo_url"`
	MaterializationRepoURL string            `json:"materialization_repo_url"`
	Host                   string            `json:"host"`
	Port                   int               `json:"port"`
	ResolvedRef            string            `json:"resolved_ref"`
	ResolvedCommit         string            `json:"resolved_commit"`
	VirtualPath            string            `json:"virtual_path"`
	IsVirtual              bool              `json:"is_virtual"`
	Depth                  int               `json:"depth"`
	Source                 string            `json:"source"`
	LocalPath              string            `json:"local_path"`
	PackageType            string            `json:"package_type"`
	SkillSubset            []string          `json:"skill_subset"`
	IsDev                  bool              `json:"is_dev"`
	Constraint             string            `json:"constraint"`
	Alias                  string            `json:"alias"`
	RegistryPrefix         string            `json:"registry_prefix"`
	Version                string            `json:"version"`
	TreeSHA256             string            `json:"tree_sha256"`
	ResolvedURL            string            `json:"resolved_url"`
	ResolvedHash           string            `json:"resolved_hash"`
	ContentHash            string            `json:"content_hash"`
	ResolvedBy             string            `json:"resolved_by"`
	ResolvedTag            string            `json:"resolved_tag"`
	ResolvedAt             string            `json:"resolved_at"`
	Name                   string            `json:"name"`
	DeclaredLicense        string            `json:"declared_license"`
	DeployedFiles          []string          `json:"deployed_files"`
	DeployedFileHashes     map[string]string `json:"deployed_file_hashes"`
	fields                 map[string]any
}

// Field returns an independent copy of any original lock-entry field.
func (d LockedDependency) Field(name string) any {
	return cloneField(d.fields[name])
}

// Lockfile is a validated OpenAPM lock document with preserved original bytes.
type Lockfile struct {
	document *document
	Version  string
	entries  []LockedDependency
}

func ParseLockfile(content []byte) (*Lockfile, error) {
	doc, err := parseDocument(content, "apm.lock.yaml")
	if err != nil {
		return nil, err
	}
	if err := validateDocument(doc.fields, LockfileSchemaID, lockfileSchemaLoader); err != nil {
		return nil, fmt.Errorf("apm.lock.yaml: %w", err)
	}
	data, err := decodeFields[struct {
		Version      string             `json:"lockfile_version"`
		Dependencies []LockedDependency `json:"dependencies"`
	}](doc.fields)
	if err != nil {
		return nil, fmt.Errorf("apm.lock.yaml: %w", err)
	}
	raw, ok := doc.fields["dependencies"].([]any)
	if !ok {
		return nil, errors.New("apm.lock.yaml: dependencies must be an array")
	}
	for i, entry := range data.Dependencies {
		fields, ok := raw[i].(map[string]any) //nolint:uncheckedsliceindex // data.Dependencies was decoded from this same validated raw array.
		if !ok {
			return nil, fmt.Errorf("apm.lock.yaml: dependencies[%d] must be an object", i)
		}
		data.Dependencies[i].fields = fields
		if entry.Source == "" && (entry.RepoURL == "" || entry.ResolvedCommit == "") {
			return nil, fmt.Errorf("apm.lock.yaml: dependencies[%d] must record repo_url and resolved_commit for Git sources", i)
		}
		if entry.Source == "registry" {
			if fields["repo_url"] == nil || fields["resolved_url"] == nil || fields["resolved_hash"] == nil {
				return nil, fmt.Errorf("apm.lock.yaml: dependencies[%d] must record registry identity, URL and hash", i)
			}
		}
	}
	return &Lockfile{document: doc, Version: data.Version, entries: data.Dependencies}, nil
}

func (l *Lockfile) Field(name string) any {
	return cloneField(l.document.fields[name])
}

func (l *Lockfile) Dependencies() []LockedDependency {
	entries := append([]LockedDependency(nil), l.entries...)
	for i := range entries {
		entries[i].SkillSubset = append([]string(nil), entries[i].SkillSubset...)
		entries[i].DeployedFiles = append([]string(nil), entries[i].DeployedFiles...)
		entries[i].DeployedFileHashes = maps.Clone(entries[i].DeployedFileHashes)
	}
	return entries
}

// Bytes returns a copy of the original YAML without discarding unknown data.
func (l *Lockfile) Bytes() []byte {
	return bytes.Clone(l.document.raw)
}
