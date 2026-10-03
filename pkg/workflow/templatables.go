// Package workflow – templatable field helpers
//
// A "templatable" field is a safe-output config field that:
//   - Does NOT affect the generated .lock.yml file (i.e. it carries no
//     compile-time information that changes the workflow YAML structure).
//   - CAN be supplied as a literal value (bool/string/int …) OR as a
//     GitHub Actions expression ("${{ inputs.foo }}") that is evaluated at
//     runtime when the env var containing the JSON config is expanded.
//
// # Go side
//
// TemplatableInt32 is a named type that handles JSON unmarshaling of both
// integer literals and GitHub Actions expression strings transparently.
// Use it for any frontmatter field that accepts "${{ inputs.N }}" alongside
// plain integers (e.g. timeout-minutes).
//
// preprocessBoolFieldAsString and preprocessIntFieldAsString live in
// config_preprocessing.go. They must be called before YAML unmarshaling so
// *string fields can accept both literal values and GitHub Actions expressions.
//
// # JS side
//
// parseBoolTemplatable and parseIntTemplatable (in templatable.cjs) are
// the counterparts used by safe-output handlers when reading the JSON
// config at runtime.

package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"gopkg.in/yaml.v3"
)

var templatablesLog = logger.New("workflow:templatables")

const templatableBoolErrorExample = "value must be a boolean or a GitHub Actions expression. Expected true, false, or an expression string. Example: <field>: true or <field>: ${{ inputs.flag }}"

// TemplatableInt32 represents an integer frontmatter field that also accepts
// GitHub Actions expression strings (e.g. "${{ inputs.timeout }}").  The
// underlying value is always stored as a string: numeric literals as their
// decimal representation, expressions verbatim.
//
// Use *TemplatableInt32 in struct fields with json:"field,omitempty" so that
// unset fields are omitted during marshaling.
//
// Example struct usage:
//
//	TimeoutMinutes *TemplatableInt32 `json:"timeout-minutes,omitempty"`
//
// Example frontmatter values both accepted:
//
//	timeout-minutes: 30
//	timeout-minutes: ${{ inputs.timeout }}
type TemplatableInt32 string

// UnmarshalJSON allows TemplatableInt32 to accept both JSON numbers (integer
// literals) and JSON strings that are GitHub Actions expressions.
// Free-form string literals that are not expressions are rejected with an error.
func (t *TemplatableInt32) UnmarshalJSON(data []byte) error {
	// Try a JSON number first (e.g. 30)
	var n int32
	if err := json.Unmarshal(data, &n); err == nil {
		*t = TemplatableInt32(strconv.FormatInt(int64(n), 10))
		return nil
	}
	// Try a JSON string (e.g. "${{ inputs.timeout }}")
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		templatablesLog.Printf("TemplatableInt32 rejected: not number or string: %s", data)
		return fmt.Errorf("value must be an integer or a GitHub Actions expression, got %s. Expected an integer literal or an expression string. Example: <field>: 30 or <field>: ${{ inputs.timeout }}", data)
	}
	if !isExpression(s) {
		templatablesLog.Printf("TemplatableInt32 rejected non-expression string: %q", s)
		return fmt.Errorf("value must be an integer or a GitHub Actions expression, got string %q. Expected an integer literal or an expression string. Example: <field>: 30 or <field>: ${{ inputs.timeout }}", s)
	}
	*t = TemplatableInt32(s)
	return nil
}

// MarshalJSON emits a JSON number for numeric literals and a JSON string for
// GitHub Actions expressions.
func (t *TemplatableInt32) MarshalJSON() ([]byte, error) {
	if n, err := strconv.Atoi(string(*t)); err == nil {
		return json.Marshal(n)
	}
	return json.Marshal(string(*t))
}

// String returns the underlying string representation of the value.
func (t *TemplatableInt32) String() string {
	return string(*t)
}

// IsExpression returns true if the value is a GitHub Actions expression
// (i.e. starts with "${{" and ends with "}}").
func (t *TemplatableInt32) IsExpression() bool {
	s := string(*t)
	return isExpression(s)
}

// IntValue returns the integer value for numeric literals.
// Returns 0 for GitHub Actions expressions, which are not evaluable at
// compile time.
func (t *TemplatableInt32) IntValue() int {
	if n, err := strconv.Atoi(string(*t)); err == nil {
		return n
	}
	return 0 // expression strings are not evaluable at compile time
}

// ToValue returns the native Go value for use in map literals and JSON output:
//   - an int for numeric literals (e.g. 30)
//   - a string for GitHub Actions expressions (e.g. "${{ inputs.timeout }}")
//
// This is the canonical helper for producing a map[string]any entry;
// callers should prefer it over calling IsExpression + IntValue/String manually.
func (t *TemplatableInt32) ToValue() any {
	if t.IsExpression() {
		return string(*t)
	}
	if n, err := strconv.Atoi(string(*t)); err == nil {
		return n
	}
	return string(*t)
}

// Ptr returns a pointer to a copy of t, convenient for constructing
// *TemplatableInt32 values inline.
func (t *TemplatableInt32) Ptr() *TemplatableInt32 {
	v := *t
	return &v
}

// TemplatableBool represents a boolean frontmatter field that also accepts
// GitHub Actions expression strings (e.g. "${{ inputs.enabled }}"). The
// underlying value is always stored as a string: boolean literals as "true" or
// "false", expressions verbatim.
type TemplatableBool string

// UnmarshalJSON allows TemplatableBool to accept both JSON booleans and JSON
// strings that are GitHub Actions expressions.
func (t *TemplatableBool) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if b {
			*t = TemplatableBool("true")
		} else {
			*t = TemplatableBool("false")
		}
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("%s, got %s", templatableBoolErrorExample, data)
	}
	if !isExpression(s) {
		return fmt.Errorf("%s, got string %q", templatableBoolErrorExample, s)
	}
	*t = TemplatableBool(s)
	return nil
}

// UnmarshalYAML allows TemplatableBool to accept both YAML booleans and GitHub
// Actions expression strings.
func (t *TemplatableBool) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!bool":
			if node.Value == "true" {
				*t = TemplatableBool("true")
			} else {
				*t = TemplatableBool("false")
			}
			return nil
		case "!!str":
			if !isExpression(node.Value) {
				return fmt.Errorf("%s, got string %q", templatableBoolErrorExample, node.Value)
			}
			*t = TemplatableBool(node.Value)
			return nil
		}
	}
	return errors.New(templatableBoolErrorExample)
}

// MarshalJSON emits a JSON boolean for literal values and a JSON string for
// GitHub Actions expressions.
func (t *TemplatableBool) MarshalJSON() ([]byte, error) {
	switch string(*t) {
	case "true":
		return json.Marshal(true)
	case "false":
		return json.Marshal(false)
	default:
		return json.Marshal(string(*t))
	}
}

// String returns the underlying string representation of the value.
func (t *TemplatableBool) String() string {
	return string(*t)
}

func templatableBoolPtrToStringPtr(value *TemplatableBool) *string {
	if value == nil {
		return nil
	}
	s := value.String()
	return &s
}

func templatableBoolIsTrue(value *TemplatableBool) bool {
	return value != nil && value.String() == "true"
}

// templatableBoolEnvVarValue returns only staged values that must be preserved
// in env vars at runtime. Literal false is treated the same as unset, while
// literal true and GitHub Actions expressions must be propagated.
func templatableBoolEnvVarValue(value *TemplatableBool) *string {
	if value == nil {
		return nil
	}
	s := value.String()
	if s == "true" || isExpression(s) {
		return &s
	}
	return nil
}

func resolveSafeOutputsStagedValue(trialMode bool, staged *TemplatableBool) *string {
	if trialMode {
		s := "true"
		return &s
	}
	return templatableBoolEnvVarValue(staged)
}

func resolveCombinedSafeOutputsStagedValue(trialMode bool, staged ...*TemplatableBool) *string {
	if trialMode {
		value := "true"
		return &value
	}
	var expressions []string
	var singleExpression *string
	for _, value := range staged {
		resolved := templatableBoolEnvVarValue(value)
		if resolved == nil {
			continue
		}
		if *resolved == "true" {
			return resolved
		}
		expression := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(*resolved, "${{"), "}}"))
		expressions = append(expressions, expression)
		singleExpression = resolved
	}
	switch len(expressions) {
	case 0:
		return nil
	case 1:
		return singleExpression
	default:
		value := "${{ " + strings.Join(expressions, " || ") + " }}"
		return &value
	}
}

// buildTemplatableEnvVar returns a YAML environment variable entry for a
// templatable field. If value is a GitHub Actions expression it is
// embedded unquoted so that GitHub Actions can evaluate it at runtime;
// otherwise the literal string is quoted. Returns nil if value is nil.
func buildTemplatableEnvVar(envVarName string, value *string) []string {
	if value == nil {
		return nil
	}
	v := *value
	if isExpression(v) {
		return []string{fmt.Sprintf("          %s: %s\n", envVarName, v)}
	}
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, v)}
}

// buildTemplatableBoolEnvVar returns a YAML environment variable entry for a
// templatable boolean field. If value is a GitHub Actions expression it is
// embedded unquoted so that GitHub Actions can evaluate it at runtime;
// otherwise the literal string is quoted. Returns nil if value is nil.
func buildTemplatableBoolEnvVar(envVarName string, value *string) []string {
	return buildTemplatableEnvVar(envVarName, value)
}

// AddTemplatableBool adds a templatable boolean field to the handler config.
//
// The stored JSON value depends on the content of *value:
//   - "true"  → JSON boolean true   (backward-compatible with existing handlers)
//   - "false" → JSON boolean false
//   - any other string (GitHub Actions expression) → stored as a JSON string so
//     that GitHub Actions can evaluate it at runtime when the env var that
//     contains the JSON config is expanded
//   - nil → field is omitted
func (b *handlerConfigBuilder) AddTemplatableBool(key string, value *string) *handlerConfigBuilder {
	if value == nil {
		return b
	}
	switch *value {
	case "true":
		b.config[key] = true
	case "false":
		b.config[key] = false
	default:
		b.config[key] = *value // expression string – evaluated at runtime
	}
	return b
}

// buildTemplatableIntEnvVar returns a YAML environment variable entry for a
// templatable integer field. If value is a GitHub Actions expression it is
// embedded unquoted so that GitHub Actions can evaluate it at runtime;
// otherwise the literal string is quoted. Returns nil if value is nil.
func buildTemplatableIntEnvVar(envVarName string, value *string) []string {
	return buildTemplatableEnvVar(envVarName, value)
}

// AddTemplatableInt adds a templatable integer field to the handler config.
//
// The stored JSON value depends on the content of *value:
//   - a numeric string (e.g. "5") → JSON number (backward-compatible with existing handlers)
//   - any other string (GitHub Actions expression) → stored as a JSON string so
//     that GitHub Actions can evaluate it at runtime when the env var that
//     contains the JSON config is expanded
//   - nil → field is omitted
func (b *handlerConfigBuilder) AddTemplatableInt(key string, value *string) *handlerConfigBuilder {
	if value == nil {
		return b
	}
	v := *value
	// If it parses as an integer, store as JSON number for backward compatibility
	if n, err := strconv.Atoi(v); err == nil {
		if n > 0 {
			b.config[key] = n
		}
		return b
	}
	// Otherwise it's a GitHub Actions expression – store as string
	b.config[key] = v
	return b
}

// defaultIntStr returns a pointer to the string representation of n.
// Used to set default values for templatable integer fields when the field is nil.
func defaultIntStr(n int) *string {
	s := strconv.Itoa(n)
	return &s
}

const templatableBoolOrIntErrorExample = "value must be a boolean, a non-negative integer (0–100), or a GitHub Actions expression. Expected true/false, an integer from 0 to 100, or an expression string. Example: deduplicate-by-title: true, deduplicate-by-title: 1, or deduplicate-by-title: ${{ inputs.dedup }}"

// TemplatableBoolOrInt represents a field that accepts a boolean, a non-negative integer
// (0–100), or a GitHub Actions expression string (e.g. "${{ inputs.dedup }}").
// The underlying value is stored as a string:
//   - booleans as "true" or "false"
//   - integers as their decimal representation (e.g. "0", "1")
//   - expressions verbatim (e.g. "${{ inputs.dedup }}")
//
// Use *TemplatableBoolOrInt in struct fields with yaml:"field,omitempty" so that
// unset fields are omitted during YAML marshaling.
//
// Example struct usage:
//
//	DeduplicateByTitle *TemplatableBoolOrInt `yaml:"deduplicate-by-title,omitempty"`
//
// Example frontmatter values all accepted:
//
//	deduplicate-by-title: true
//	deduplicate-by-title: 1
//	deduplicate-by-title: ${{ inputs.dedup }}
type TemplatableBoolOrInt string

// UnmarshalYAML allows TemplatableBoolOrInt to accept YAML booleans, YAML integers,
// and GitHub Actions expression strings.
func (t *TemplatableBoolOrInt) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("%s", templatableBoolOrIntErrorExample)
	}
	switch node.Tag {
	case "!!bool":
		*t = TemplatableBoolOrInt(node.Value) // "true" or "false"
		return nil
	case "!!int":
		n, err := strconv.Atoi(node.Value)
		if err != nil || n < 0 || n > 100 {
			return fmt.Errorf("integer must be between 0 and 100, got %q. Expected a value in that range. Example: deduplicate-by-title: 1", node.Value)
		}
		*t = TemplatableBoolOrInt(node.Value)
		return nil
	case "!!str":
		if !isExpression(node.Value) {
			return fmt.Errorf("%s, got string %q", templatableBoolOrIntErrorExample, node.Value)
		}
		*t = TemplatableBoolOrInt(node.Value)
		return nil
	}
	return fmt.Errorf("%s", templatableBoolOrIntErrorExample)
}

// UnmarshalJSON allows TemplatableBoolOrInt to accept JSON booleans, JSON numbers,
// and JSON strings that are GitHub Actions expressions.
func (t *TemplatableBoolOrInt) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if b {
			*t = TemplatableBoolOrInt("true")
		} else {
			*t = TemplatableBoolOrInt("false")
		}
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		if n < 0 || n > 100 {
			return fmt.Errorf("integer must be between 0 and 100, got %d. Expected a value in that range. Example: deduplicate-by-title: 1", n)
		}
		*t = TemplatableBoolOrInt(strconv.Itoa(n))
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("%s, got %s", templatableBoolOrIntErrorExample, data)
	}
	if !isExpression(s) {
		return fmt.Errorf("%s, got string %q", templatableBoolOrIntErrorExample, s)
	}
	*t = TemplatableBoolOrInt(s)
	return nil
}

// MarshalJSON emits a JSON boolean for "true"/"false", a JSON integer for numeric
// strings, and a JSON string for GitHub Actions expressions.
func (t *TemplatableBoolOrInt) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.ToValue())
}

// String returns the underlying string representation of the value.
func (t *TemplatableBoolOrInt) String() string {
	return string(*t)
}

// IsExpression returns true if the value is a GitHub Actions expression.
func (t *TemplatableBoolOrInt) IsExpression() bool {
	return isExpression(string(*t))
}

// ToValue returns the native Go value for use in map literals and JSON output:
//   - true/false for boolean literals
//   - an int for numeric literals
//   - a string for GitHub Actions expressions
func (t *TemplatableBoolOrInt) ToValue() any {
	s := string(*t)
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return s // expression string – evaluated at runtime
}

// templatableIntValue parses a *string templatable integer value to int.
// Returns 0 if value is nil or is a GitHub Actions expression (not evaluable at compile time).
// Returns the parsed integer for literal numeric strings.
func templatableIntValue(value *string) int {
	if value == nil {
		return 0
	}
	if n, err := strconv.Atoi(*value); err == nil {
		return n
	}
	return 0 // expression strings are not evaluable at compile time
}
