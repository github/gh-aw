package apm

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	ManifestSchemaID = "https://microsoft.github.io/apm/specs/schemas/manifest-v0.1.48.schema.json"
	LockfileSchemaID = "https://microsoft.github.io/apm/specs/schemas/lockfile-v0.1.schema.json"
)

//go:embed schemas/manifest-v0.1.48.schema.json
var manifestSchema string

//go:embed schemas/lockfile-v0.1.schema.json
var lockfileSchema string

var manifestSchemaLoader = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileSchema(ManifestSchemaID, manifestSchema)
})
var lockfileSchemaLoader = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileSchema(LockfileSchemaID, lockfileSchema)
})
var semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)
var aliasPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
var schemaPatterns = map[string]*regexp.Regexp{
	`^(all|[a-z][a-z0-9-]*)$`: regexp.MustCompile(`^(all|[a-z][a-z0-9-]*)$`),
	`^https?://`:              regexp.MustCompile(`^https?://`),
	`^[Hh][Tt][Tt][Pp][Ss]?:`: regexp.MustCompile(`^[Hh][Tt][Tt][Pp][Ss]?:`),
	`^(sha256:[0-9a-f]{64}|sha384:[0-9a-f]{96}|sha512:[0-9a-f]{128}|[0-9a-f]{64})$`: regexp.MustCompile(`^(sha256:[0-9a-f]{64}|sha384:[0-9a-f]{96}|sha512:[0-9a-f]{128}|[0-9a-f]{64})$`),
	`^[0-9a-f]{40}$`:      regexp.MustCompile(`^[0-9a-f]{40}$`),
	`^x-[a-z][a-z0-9-]*$`: regexp.MustCompile(`^x-[a-z][a-z0-9-]*$`),
}

func validateDocument(fields map[string]any, id string, loader func() (*jsonschema.Schema, error)) error {
	if selector, exists := fields["$schema"]; exists && selector != id {
		return fmt.Errorf("unsupported $schema; use %s", id)
	}
	schema, err := loader()
	if err != nil {
		return err
	}
	if err := schema.Validate(fields); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}
	return nil
}

func compileSchema(id, content string) (*jsonschema.Schema, error) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(content), &schema); err != nil {
		return nil, err
	}
	if id == ManifestSchemaID {
		// req-mf-004 makes SemVer advisory; the prose overrides the schema.
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			return nil, errors.New("embedded APM schema has no properties")
		}
		properties["version"] = map[string]any{"type": "string"}
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseRegexpEngine(schemaRegexp)
	if err := compiler.AddResource(id, schema); err != nil {
		return nil, err
	}
	return compiler.Compile(id)
}

type aliasRegexp struct{ *regexp.Regexp }

func (r aliasRegexp) MatchString(value string) bool {
	value = trimSchemaWhitespace(value)
	return value != "." && value != ".." && r.Regexp.MatchString(value)
}

// The pinned schema uses two ECMAScript end-of-input/lookahead patterns.
// These equivalent bounded matchers avoid another regex dependency.
func schemaRegexp(pattern string) (jsonschema.Regexp, error) {
	if pattern == `\S` {
		return nonWhitespaceRegexp{}, nil
	}
	if pattern == `^\S+(?![\s\S])` {
		return nonWhitespaceRegexp{whole: true}, nil
	}
	if pattern == `^\s*(?!\.{1,2}\s*(?![\s\S]))[a-zA-Z0-9._-]+\s*(?![\s\S])` {
		return aliasRegexp{aliasPattern}, nil
	}
	if compiled, ok := schemaPatterns[pattern]; ok {
		return compiled, nil
	}
	return nil, fmt.Errorf("unsupported embedded schema pattern %q", pattern)
}

type nonWhitespaceRegexp struct{ whole bool }

func (r nonWhitespaceRegexp) String() string {
	if r.whole {
		return `^\S+(?![\s\S])`
	}
	return `\S`
}

func (r nonWhitespaceRegexp) MatchString(value string) bool {
	found := false
	for _, char := range value {
		if schemaWhitespace(char) {
			if r.whole {
				return false
			}
		} else {
			found = true
		}
	}
	return found
}

func trimSchemaWhitespace(value string) string {
	return strings.TrimFunc(value, schemaWhitespace)
}

func schemaWhitespace(char rune) bool {
	switch char {
	case '\t', '\v', '\n', '\f', '\r', '\uFEFF', '\u2028', '\u2029':
		return true
	default:
		return unicode.Is(unicode.Zs, char)
	}
}
