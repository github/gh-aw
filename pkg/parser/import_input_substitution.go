// Package parser provides functions for parsing and processing workflow markdown files.
// import_input_substitution.go implements text-level substitution of import-inputs
// expressions (${{ github.aw.import-inputs.* }}) in raw workflow file content.
package parser

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/importinpututil"
)

// importInputsFallbackExprRegex matches fallback expressions containing only
// import-input references joined by ||.
var importInputsFallbackExprRegex = regexp.MustCompile(`\$\{\{\s*(github\.aw\.import-inputs\.[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)?(?:\s*\|\|\s*github\.aw\.import-inputs\.[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)?)+)\s*\}\}`)

// importInputsExprRegex matches ${{ github.aw.import-inputs.<key> }} and
// ${{ github.aw.import-inputs.<key>.<subkey> }} expressions in raw content.
var importInputsExprRegex = regexp.MustCompile(`\$\{\{\s*github\.aw\.import-inputs\.([a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)?)\s*\}\}`)

// legacyInputsExprRegex matches ${{ github.aw.inputs.<key> }} (legacy form) in raw content.
var legacyInputsExprRegex = regexp.MustCompile(`\$\{\{\s*github\.aw\.inputs\.([a-zA-Z0-9_-]+)\s*\}\}`)

// substituteImportInputsInContent performs text-level substitution of
// ${{ github.aw.import-inputs.* }} and ${{ github.aw.inputs.* }} expressions
// in raw file content (including YAML frontmatter). This is called before YAML
// parsing so that array/object values serialised as JSON produce valid YAML.
func substituteImportInputsInContent(content string, inputs map[string]any) string {
	importLog.Printf("Substituting import-inputs expressions: inputs=%d, contentBytes=%d", len(inputs), len(content))

	result := content
	if len(inputs) > 0 {
		result = legacyInputsExprRegex.ReplaceAllStringFunc(result, buildImportInputReplaceFunc(legacyInputsExprRegex, inputs))
	}
	result = importInputsFallbackExprRegex.ReplaceAllStringFunc(result, buildImportInputFallbackReplaceFunc(inputs))
	if len(inputs) > 0 {
		result = importInputsExprRegex.ReplaceAllStringFunc(result, buildImportInputReplaceFunc(importInputsExprRegex, inputs))
	}
	return result
}

func buildImportInputFallbackReplaceFunc(inputs map[string]any) func(string) string {
	return func(match string) string {
		expression, found := firstRegexCapture(importInputsFallbackExprRegex, match)
		if !found {
			return match
		}
		fallback := ""
		for operand := range strings.SplitSeq(expression, "||") {
			inputPath := strings.TrimPrefix(strings.TrimSpace(operand), "github.aw.import-inputs.")
			value, found := resolveImportInputValue(inputs, inputPath)
			if !found {
				fallback = ""
				continue
			}
			formatted, ok := importinpututil.FormatResolvedValue(value)
			if !ok {
				fallback = ""
				continue
			}
			fallback = formatted
			if isTruthyImportInput(value) {
				return formatted
			}
		}
		return fallback
	}
}

func isTruthyImportInput(value any) bool {
	if value == nil {
		return false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Bool:
		return reflected.Bool()
	case reflect.String:
		return reflected.String() != ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflected.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflected.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return reflected.Float() != 0
	default:
		return true
	}
}

func buildImportInputReplaceFunc(regex *regexp.Regexp, inputs map[string]any) func(string) string {
	return func(match string) string {
		inputPath, found := firstRegexCapture(regex, match)
		if !found {
			return match
		}
		strVal, found := resolveImportInputPath(inputs, inputPath)
		if found {
			return strVal
		}
		return match
	}
}

func firstRegexCapture(regex *regexp.Regexp, value string) (string, bool) {
	for index, capture := range regex.FindStringSubmatch(value) {
		if index == 1 {
			return capture, true
		}
	}
	return "", false
}

func resolveImportInputPath(inputs map[string]any, inputPath string) (string, bool) {
	value, ok := resolveImportInputValue(inputs, inputPath)
	if !ok {
		return "", false
	}
	return importinpututil.FormatResolvedValue(value)
}

func resolveImportInputValue(inputs map[string]any, inputPath string) (any, bool) {
	return importinpututil.ResolvePathValue(inputs, inputPath)
}
