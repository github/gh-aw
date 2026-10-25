// This file implements the Model Alias Format (MAF) identifier parser as defined in
// the Model Alias Format Specification (docs/src/content/docs/specs/model-alias-specification.md).
//
// # Model Identifier Syntax (Section 4)
//
// A model identifier string takes one of the following forms:
//
//	bare-name                         e.g. "sonnet", "agent"
//	provider/model-token              e.g. "copilot/gpt-5"
//	provider/model-glob-token         e.g. "copilot/*sonnet*"
//	any of the above + "?" params     e.g. "opus?effort=high"
//
// # ABNF Grammar (Section 4.1)
//
//	model-identifier  = base-identifier [ "?" query-string ]
//
//	base-identifier   = bare-name
//	                  / provider-scoped
//	                  / glob-pattern
//
//	bare-name         = 1*( ALPHA / DIGIT / "-" / "_" / "." )
//	                    ; MUST NOT start with "-" or "."
//
//	provider-scoped   = provider-token "/" model-token
//
//	provider-token    = ALPHA 0*( ALPHA / DIGIT / "-" )
//	                    ; starts with a letter; hyphens allowed but not at end
//
//	model-token       = model-char 0*( model-char / "." model-char )
//
//	model-char        = ALPHA / DIGIT / "-" / "_"
//
//	glob-pattern      = provider-token "/" model-glob-token
//
//	model-glob-token  = 1*( model-char / "." / "*" )
//
//	query-string      = param *( "&" param )
//
//	param             = param-key "=" param-value
//
//	param-key         = ALPHA 0*( ALPHA / DIGIT / "-" )
//
//	param-value       = 1*( ALPHA / DIGIT / "-" / "_" / "." )

package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var modelIdentifierLog = logger.New("workflow:model_identifier")

// ParsedModelIdentifier holds the components of a parsed model identifier.
type ParsedModelIdentifier struct {
	// Raw is the original unparsed string.
	Raw string
	// Base is the base identifier (before "?").
	Base string
	// Provider is the provider token (empty for bare identifiers).
	Provider string
	// ModelToken is the model-token portion after the "/" (empty for bare identifiers).
	ModelToken string
	// IsGlob reports whether the model token contains a "*" wildcard.
	IsGlob bool
	// Params holds the URL-style query parameters (key → value).
	Params map[string]string
}

// Defined parameter keys recognised by the spec (Section 6).
const (
	modelParamEffort      = "effort"
	modelParamTemperature = "temperature"
)

// knownModelParams is the set of parameter keys defined in Section 6.
var knownModelParams = map[string]struct{}{
	modelParamEffort:      {},
	modelParamTemperature: {},
}

// ─── Character-class helpers ─────────────────────────────────────────────────

// regex patterns derived directly from the ABNF grammar.
var (
	// provider-token: starts with ALPHA, followed by ALPHA/DIGIT/"-"; must not end with "-"
	reProviderToken = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

	// model-char: ALPHA / DIGIT / "-" / "_"
	// model-token: model-char segments separated by "."; each segment starts with ALPHA or DIGIT
	reModelToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*(\.[A-Za-z0-9][A-Za-z0-9_-]*)*$`)

	// bare-name: starts with ALPHA/DIGIT; contains ALPHA/DIGIT/"-"/"_"/"."
	reBareName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

	// param-key: starts with ALPHA; followed by ALPHA/DIGIT/"-"
	reParamKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

	// param-value: 1*(ALPHA / DIGIT / "-" / "_" / ".")
	reParamValue = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// firstForbiddenCharInProviderOrParam returns the first character in s that is not in
// [A-Za-z0-9-], or 0 if all characters are allowed.
// Used for provider tokens and parameter keys (V-MAF-006).
func firstForbiddenCharInProviderOrParam(s string) rune {
	for _, r := range s {
		if !isAlpha(r) && !isDigit(r) && r != '-' {
			return r
		}
	}
	return 0
}

// firstForbiddenCharInModelToken returns the first character in s that is not in
// [A-Za-z0-9_.-], or 0 if all characters are allowed.
// Used for model tokens (V-MAF-006).
func firstForbiddenCharInModelToken(s string) rune {
	for _, r := range s {
		if !isAlpha(r) && !isDigit(r) && r != '_' && r != '.' && r != '-' {
			return r
		}
	}
	return 0
}

// firstForbiddenCharInBareName returns the first character in s that is not in
// [A-Za-z0-9._-], or 0 if all characters are allowed.
// Used for bare names (V-MAF-006).
func firstForbiddenCharInBareName(s string) rune {
	for _, r := range s {
		if !isAlpha(r) && !isDigit(r) && r != '_' && r != '.' && r != '-' {
			return r
		}
	}
	return 0
}

// firstForbiddenCharInParamValue returns the first character in s that is not in
// [A-Za-z0-9._-], or 0 if all characters are allowed.
// Used for parameter values (V-MAF-006).
func firstForbiddenCharInParamValue(s string) rune {
	for _, r := range s {
		if !isAlpha(r) && !isDigit(r) && r != '_' && r != '.' && r != '-' {
			return r
		}
	}
	return 0
}

func isAlpha(r rune) bool { return isLetter(r) }

// ParseModelIdentifier parses a model identifier string into its components.
//
// The identifier format follows the ABNF grammar in Section 4.1 of the spec.
// Returns a non-nil *ParsedModelIdentifier on success.
// Returns an error (satisfying V-MAF-001 and V-MAF-006) on syntax violations.
func ParseModelIdentifier(s string) (*ParsedModelIdentifier, error) {
	modelIdentifierLog.Printf("Parsing model identifier: %q", s)
	if s == "" {
		return nil, errors.New("model identifier must not be empty. Expected a bare alias, or a provider-scoped name. Example: model: openai/gpt-4o")
	}

	// Split on the first "?" to separate base from query string.
	base, rawQuery, _ := strings.Cut(s, "?")

	p := &ParsedModelIdentifier{
		Raw:    s,
		Base:   base,
		Params: map[string]string{},
	}

	if err := parseModelIdentifierBase(p); err != nil {
		return nil, err
	}
	if rawQuery != "" {
		modelIdentifierLog.Printf("Parsing query string: %q", rawQuery)
		params, err := parseQueryString(rawQuery)
		if err != nil {
			return nil, err
		}
		p.Params = params
		modelIdentifierLog.Printf("Parsed %d query param(s)", len(params))
	}
	modelIdentifierLog.Printf("Successfully parsed model identifier: provider=%q model=%q isGlob=%v params=%d", p.Provider, p.ModelToken, p.IsGlob, len(p.Params))
	return p, nil
}

func parseModelIdentifierBase(p *ParsedModelIdentifier) error {
	base := p.Base
	// ── Validate base identifier ─────────────────────────────────────────────
	if strings.Contains(base, "/") {
		// Provider-scoped or glob pattern.
		provider, modelPart, _ := strings.Cut(base, "/")

		if err := validateProviderToken(provider); err != nil {
			modelIdentifierLog.Printf("Invalid provider token %q: %v", provider, err)
			return err
		}

		p.Provider = provider

		if strings.Contains(modelPart, "*") {
			// Glob pattern.
			modelIdentifierLog.Printf("Parsing as glob pattern: provider=%q model-glob=%q", provider, modelPart)
			if err := validateModelGlobToken(modelPart); err != nil {
				return err
			}
			p.ModelToken = modelPart
			p.IsGlob = true
		} else {
			// Exact provider-scoped name.
			modelIdentifierLog.Printf("Parsing as provider-scoped: provider=%q model=%q", provider, modelPart)
			if err := validateModelToken(modelPart); err != nil {
				return err
			}
			p.ModelToken = modelPart
		}
	} else {
		// Bare name.
		modelIdentifierLog.Printf("Parsing as bare name: %q", base)
		if err := validateBareName(base); err != nil {
			return err
		}
	}

	return nil
}

// ─── Segment validators ───────────────────────────────────────────────────────

// validateProviderToken validates a provider token per the ABNF grammar.
func validateProviderToken(token string) error {
	if token == "" {
		return errors.New("model identifier: provider token must not be empty (segment type: provider). Expected a provider name before '/'. Example: model: openai/gpt-4o")
	}
	if !reProviderToken.MatchString(token) {
		if r := firstForbiddenCharInProviderOrParam(token); r != 0 {
			return fmt.Errorf("model identifier: character %q is not allowed in provider token %q (segment type: provider)", r, token)
		}
		if !isAlpha(rune(token[0])) {
			return fmt.Errorf("model identifier: provider token %q must start with a letter (segment type: provider). Example: model: openai/gpt-4o", token)
		}
	}
	// Provider token must not end with "-".
	if strings.HasSuffix(token, "-") {
		return fmt.Errorf("model identifier: provider token %q must not end with '-' (segment type: provider). Example: model: openai/gpt-4o", token)
	}
	return nil
}

// validateModelToken validates a model token (no wildcards) per the ABNF grammar.
func validateModelToken(token string) error {
	if token == "" {
		return errors.New("model identifier: model token must not be empty (segment type: model). Expected a model name after '/'. Example: model: openai/gpt-4o")
	}
	if !reModelToken.MatchString(token) {
		if r := firstForbiddenCharInModelToken(token); r != 0 {
			return fmt.Errorf("model identifier: character %q is not allowed in model token %q (segment type: model)", r, token)
		}
		return fmt.Errorf("model identifier: model token %q is syntactically invalid (segment type: model). Expected letters, digits, '-', '_', or '.'. Example: model: openai/gpt-4o", token)
	}
	return nil
}

// validateModelGlobToken validates a model-glob-token (may contain "*").
func validateModelGlobToken(token string) error {
	if token == "" {
		return errors.New("model identifier: model glob token must not be empty (segment type: model). Expected a glob pattern after '/'. Example: model: openai/gpt-4*")
	}
	for _, r := range token {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			// ALPHA / DIGIT — always allowed
		case r == '-', r == '_', r == '.', r == '*':
			// allowed in glob tokens
		default:
			return fmt.Errorf("model identifier: character %q is not allowed in glob model token %q (segment type: model)", r, token)
		}
	}
	return nil
}

// validateBareName validates a bare identifier name per the ABNF grammar.
func validateBareName(name string) error {
	if name == "" {
		return errors.New("model identifier: bare name must not be empty (segment type: alias). Expected an alias name. Example: model: gpt-4o")
	}
	// Must not start with "-" or ".".
	if name[0] == '-' || name[0] == '.' {
		return fmt.Errorf("model identifier: bare name %q must not start with '-' or '.' (segment type: alias). Expected a name starting with a letter or digit. Example: model: gpt-4o", name)
	}
	if !reBareName.MatchString(name) {
		if r := firstForbiddenCharInBareName(name); r != 0 {
			return fmt.Errorf("model identifier: character %q is not allowed in bare name %q (segment type: alias)", r, name)
		}
		return fmt.Errorf("model identifier: bare name %q is syntactically invalid (segment type: alias). Expected letters, digits, '-', '_', or '.'. Example: model: gpt-4o", name)
	}
	return nil
}

// ─── Parameter parsing ────────────────────────────────────────────────────────

// parseQueryString parses a query string of the form "key=value&key=value…".
// Returns an error if any key or value violates the grammar (syntax only).
// Known-parameter value validation is performed separately by ValidateKnownParams.
func parseQueryString(raw string) (map[string]string, error) {
	params := map[string]string{}
	for pair := range strings.SplitSeq(raw, "&") {
		if pair == "" {
			return nil, fmt.Errorf("model identifier: empty parameter pair in query string %q", raw)
		}
		k, v, found := strings.Cut(pair, "=")
		if !found {
			return nil, fmt.Errorf("model identifier: parameter %q is missing '=' separator", pair)
		}
		if err := validateParamKey(k); err != nil {
			return nil, err
		}
		if err := validateParamValue(v); err != nil {
			return nil, err
		}
		params[k] = v
	}
	return params, nil
}

// validateParamKey validates a parameter key per the ABNF grammar.
func validateParamKey(key string) error {
	if key == "" {
		return errors.New("model identifier: parameter key must not be empty (segment type: parameter key). Expected a key before '='. Example: model: openai/gpt-4o?effort=high")
	}
	if !reParamKey.MatchString(key) {
		if r := firstForbiddenCharInProviderOrParam(key); r != 0 {
			return fmt.Errorf("model identifier: character %q is not allowed in parameter key %q (segment type: parameter key)", r, key)
		}
		if !isAlpha(rune(key[0])) {
			return fmt.Errorf("model identifier: parameter key %q must start with a letter (segment type: parameter key). Example: model: openai/gpt-4o?effort=high", key)
		}
	}
	return nil
}

// validateParamValue validates a parameter value per the ABNF grammar.
func validateParamValue(value string) error {
	if value == "" {
		return errors.New("model identifier: parameter value must not be empty (segment type: parameter value). Expected a value after '='. Example: model: openai/gpt-4o?effort=high")
	}
	if !reParamValue.MatchString(value) {
		if r := firstForbiddenCharInParamValue(value); r != 0 {
			return fmt.Errorf("model identifier: character %q is not allowed in parameter value %q (segment type: parameter value)", r, value)
		}
	}
	return nil
}

// UnrecognizedParams returns the list of parameter keys in params that are not
// defined in Section 6 (i.e., not effort or temperature).
// Reserved future parameters (Section 6.3) are returned here as well since they
// are not yet defined.
func UnrecognizedParams(params map[string]string) []string {
	var unknown []string
	for k := range params {
		if _, known := knownModelParams[k]; !known {
			unknown = append(unknown, k)
		}
	}
	return unknown
}

// ─── Utility ──────────────────────────────────────────────────────────────────
