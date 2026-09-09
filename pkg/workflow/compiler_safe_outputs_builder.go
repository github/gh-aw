package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/github/gh-aw/pkg/jsonutil"
	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputsBuilderLog = logger.New("workflow:safe_outputs_builder")

// handlerConfigBuilder provides a fluent API for building handler configurations
type handlerConfigBuilder struct {
	config map[string]any
}

const templatableJSONExpressionPrefix = "__GH_AW_TEMPLATABLE_JSON_EXPRESSION__"

// templatableJSONExpressionCounter assigns a unique id to every templatableJSONExpression
// created during compilation. The id is embedded in the placeholder used during the
// two-pass marshalling in marshalSafeOutputsConfig, so that a placeholder can never
// collide with user-controlled expression text (which would otherwise corrupt
// unrelated values in the same config payload).
var templatableJSONExpressionCounter uint64

// templatableJSONExpression is a marker type that causes marshalSafeOutputsConfig to
// splice the raw (unquoted) expression text into the JSON output instead of a quoted
// JSON string, so that GitHub Actions expressions evaluating to JSON arrays are
// embedded as JSON values rather than JSON strings.
type templatableJSONExpression struct {
	expr string
	id   uint64
}

// newTemplatableJSONExpression wraps expr with a unique id so that its placeholder
// cannot collide with any user-supplied content elsewhere in the config.
func newTemplatableJSONExpression(expr string) templatableJSONExpression {
	id := atomic.AddUint64(&templatableJSONExpressionCounter, 1)
	return templatableJSONExpression{expr: expr, id: id}
}

func (e templatableJSONExpression) placeholder() string {
	return fmt.Sprintf("%s_%d__", templatableJSONExpressionPrefix, e.id)
}

func (e templatableJSONExpression) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.placeholder())
}

// wrapExpressionWithToJSON ensures a GitHub Actions expression is serialized through
// toJSON(...) before being spliced into the config as a raw JSON value. This keeps the
// output valid JSON even when the expression evaluates to an empty string at runtime
// (toJSON("") => `""`, still valid JSON), while arrays and JSON-text strings continue to
// be handled the same way they already are by the runtime handler.
//
// Callers must only pass expressions that satisfy isExpression (i.e. the whole string is
// wrapped in a well-formed "${{ ... }}"); AddTemplatableJSONSlice enforces this.
func wrapExpressionWithToJSON(expr string) string {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(expr), "${{"), "}}"))
	if strings.HasPrefix(inner, "toJSON(") {
		return expr
	}
	return "${{ toJSON(" + inner + ") }}"
}

// newHandlerConfigBuilder creates a new handler config builder
func newHandlerConfigBuilder() *handlerConfigBuilder {
	return &handlerConfigBuilder{
		config: map[string]any{},
	}
}

// AddIfPositive adds an integer field only if the value is greater than 0
func (b *handlerConfigBuilder) AddIfPositive(key string, value int) *handlerConfigBuilder {
	if value > 0 {
		b.config[key] = value
	}
	return b
}

// AddIfNotEmpty adds a string field only if the value is not empty
func (b *handlerConfigBuilder) AddIfNotEmpty(key string, value string) *handlerConfigBuilder {
	if value != "" {
		b.config[key] = value
	}
	return b
}

// AddStringSlice adds a string slice field only if the slice is not empty
func (b *handlerConfigBuilder) AddStringSlice(key string, value []string) *handlerConfigBuilder {
	if len(value) > 0 {
		b.config[key] = value
	}
	return b
}

// AddMapSlice adds a slice of string maps field only if the slice is not empty.
// Useful for structured list fields such as allowed-transitions.
func (b *handlerConfigBuilder) AddMapSlice(key string, value []map[string]string) *handlerConfigBuilder {
	if len(value) > 0 {
		b.config[key] = value
	}
	return b
}

// AddTemplatableStringSlice adds a string slice field that may contain a GitHub Actions
// expression.  When the slice has exactly one element and that element is a GitHub Actions
// expression (as produced by preprocessStringArrayFieldAsTemplatable or
// ParseStringArrayOrExprFromConfig), the expression string is stored as a plain JSON string
// rather than a JSON array.  This allows GitHub Actions to evaluate the expression at
// runtime when the config.json file is written via heredoc expansion.
//
// For all other non-empty slices the field is stored as a JSON array, matching the
// behaviour of AddStringSlice.
func (b *handlerConfigBuilder) AddTemplatableStringSlice(key string, value []string) *handlerConfigBuilder {
	if len(value) == 0 {
		return b
	}
	// A single-element expression slice is the canonical representation produced by
	// preprocessing – store as a string so GitHub Actions evaluates it at runtime.
	if len(value) == 1 && isExpression(value[0]) {
		b.config[key] = value[0]
		return b
	}
	b.config[key] = value
	return b
}

// AddTemplatableJSONSlice adds a string slice field whose single expression is expected
// to evaluate to a JSON array at runtime. Only the canonical single-element expression
// slice (as produced by ParseStringArrayOrExprFromConfig) is treated as a templatable
// expression; any other non-empty slice, including a mix of literal values and
// expressions, falls back to the plain JSON array behaviour of AddStringSlice (each
// element, including expression text, is emitted as a quoted JSON string element).
func (b *handlerConfigBuilder) AddTemplatableJSONSlice(key string, value []string) *handlerConfigBuilder {
	if len(value) == 0 {
		return b
	}
	if len(value) == 1 && isExpression(value[0]) {
		b.config[key] = newTemplatableJSONExpression(wrapExpressionWithToJSON(value[0]))
		return b
	}
	b.config[key] = value
	return b
}

func marshalSafeOutputsConfig(config map[string]any) ([]byte, error) {
	configJSON, err := jsonutil.MarshalCompactNoHTMLEscape(config)
	if err != nil {
		return nil, err
	}
	result := []byte(configJSON)
	for _, expression := range templatableJSONExpressions(config) {
		placeholderJSON, err := jsonutil.MarshalCompactNoHTMLEscape(expression.placeholder())
		if err != nil {
			return nil, err
		}
		result = bytes.ReplaceAll(result, []byte(placeholderJSON), []byte(expression.expr))
	}
	return result, nil
}

var (
	needsDotReferencePattern     = regexp.MustCompile(`needs\s*\.\s*([^\s.]+)\s*\.`)
	needsBracketReferencePattern = regexp.MustCompile(`needs\s*\[\s*['"]([^'"]+)['"]\s*\]\s*\.`)
)

// sanitizeAgentSafeOutputsConfig walks a safe-outputs config map (as produced for the agent
// job's copy of config.json) and neutralizes any templated field whose expression references
// needs.<job> for a job in unresolvableJobs. These jobs are only ever added as dependencies of
// the safe_outputs handler job, never of the agent job, so referencing their outputs from the
// agent job's config is unresolvable at the GitHub Actions expression level and would trip an
// actionlint "undefined property" error. This only affects the agent job's copy of the config;
// the handler job's own config (built separately) legitimately depends on these jobs.
func sanitizeAgentSafeOutputsConfig(config map[string]any, unresolvableJobs []string) {
	if len(unresolvableJobs) == 0 {
		return
	}
	referencesUnresolvableJob := func(expr string) bool {
		for _, reference := range needsDotReferencePattern.FindAllStringSubmatch(expr, -1) {
			if slices.Contains(unresolvableJobs, reference[1]) {
				return true
			}
		}
		for _, reference := range needsBracketReferencePattern.FindAllStringSubmatch(expr, -1) {
			if slices.Contains(unresolvableJobs, reference[1]) {
				return true
			}
		}
		return false
	}
	fieldReferencesUnresolvableJob := func(value any) bool {
		switch v := value.(type) {
		case templatableJSONExpression:
			return referencesUnresolvableJob(v.expr)
		case string:
			return isExpression(v) && referencesUnresolvableJob(v)
		case []string:
			return slices.ContainsFunc(v, func(item string) bool {
				return isExpression(item) && referencesUnresolvableJob(item)
			})
		case []any:
			return slices.ContainsFunc(v, func(item any) bool {
				switch item := item.(type) {
				case templatableJSONExpression:
					return referencesUnresolvableJob(item.expr)
				case string:
					return isExpression(item) && referencesUnresolvableJob(item)
				default:
					return false
				}
			})
		}
		return false
	}
	for _, handlerConfig := range config {
		fields, ok := handlerConfig.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range fields {
			if !fieldReferencesUnresolvableJob(value) {
				continue
			}
			switch value.(type) {
			case string:
				fields[key] = ""
			default:
				fields[key] = []any{}
			}
		}
	}
}

func templatableJSONExpressions(value any) []templatableJSONExpression {
	var expressions []templatableJSONExpression
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case templatableJSONExpression:
			expressions = append(expressions, value)
		case map[string]any:
			for _, value := range value {
				visit(value)
			}
		case []any:
			for _, value := range value {
				visit(value)
			}
		}
	}
	visit(value)
	return expressions
}

// AddBoolPtr adds a boolean pointer field only if the pointer is not nil
func (b *handlerConfigBuilder) AddBoolPtr(key string, value *bool) *handlerConfigBuilder {
	if value != nil {
		b.config[key] = *value
	}
	return b
}

// AddTemplatableBoolOrInt adds a TemplatableBoolOrInt field to the handler config.
//
// The stored JSON value depends on the content of *value:
//   - "true"  → JSON boolean true
//   - "false" → JSON boolean false
//   - a numeric string (e.g. "1") → JSON number
//   - any other string (GitHub Actions expression) → JSON string evaluated at runtime
//   - nil → field is omitted
func (b *handlerConfigBuilder) AddTemplatableBoolOrInt(key string, value *TemplatableBoolOrInt) *handlerConfigBuilder {
	if value == nil {
		return b
	}
	b.config[key] = value.ToValue()
	return b
}

// AddBoolPtrOrDefault adds a boolean field, using default if pointer is nil
func (b *handlerConfigBuilder) AddBoolPtrOrDefault(key string, value *bool, defaultValue bool) *handlerConfigBuilder {
	if value != nil {
		b.config[key] = *value
	} else {
		b.config[key] = defaultValue
	}
	return b
}

// AddStringPtr adds a string pointer field only if the pointer is not nil
func (b *handlerConfigBuilder) AddStringPtr(key string, value *string) *handlerConfigBuilder {
	if value != nil {
		b.config[key] = *value
	}
	return b
}

// AddDefault adds a field with a default value unconditionally
func (b *handlerConfigBuilder) AddDefault(key string, value any) *handlerConfigBuilder {
	b.config[key] = value
	return b
}

// AddIfTrue adds a boolean field only if the value is true
func (b *handlerConfigBuilder) AddIfTrue(key string, value bool) *handlerConfigBuilder {
	if value {
		b.config[key] = true
	}
	return b
}

// Build returns the built configuration map
func (b *handlerConfigBuilder) Build() map[string]any {
	return b.config
}

// handlerBuilder is a function that builds a handler config from SafeOutputsConfig
type handlerBuilder func(*SafeOutputsConfig) map[string]any

// getEffectiveFooterForTemplatable returns the effective footer as a templatable string.
// If the local string footer is set, use it; otherwise convert the global bool footer.
// Returns nil if neither is set (default to true in JavaScript).
func getEffectiveFooterForTemplatable(localFooter *string, globalFooter *bool) *string {
	if localFooter != nil {
		safeOutputsBuilderLog.Printf("Footer: using local override %q", *localFooter)
		return localFooter
	}
	if globalFooter != nil {
		var s string
		if *globalFooter {
			s = "true"
		} else {
			s = "false"
		}
		safeOutputsBuilderLog.Printf("Footer: derived %q from global bool", s)
		return &s
	}
	safeOutputsBuilderLog.Print("Footer: not configured, deferring to JS default")
	return nil
}

// getEffectiveFooterString returns the effective footer string value for a config.
// If the local string footer is set, use it; otherwise convert the global bool footer.
// Returns nil if neither is set (default to "always" in JavaScript).
func getEffectiveFooterString(localFooter *string, globalFooter *bool) *string {
	if localFooter != nil {
		safeOutputsBuilderLog.Printf("FooterString: using local override %q", *localFooter)
		return localFooter
	}
	if globalFooter != nil {
		var s string
		if *globalFooter {
			s = "always"
		} else {
			s = "none"
		}
		safeOutputsBuilderLog.Printf("FooterString: derived %q from global bool", s)
		return &s
	}
	safeOutputsBuilderLog.Print("FooterString: not configured, deferring to JS default")
	return nil
}
