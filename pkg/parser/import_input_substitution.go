// Package parser provides functions for parsing and processing workflow markdown files.
// import_input_substitution.go implements text-level substitution of import-inputs
// expressions (${{ github.aw.import-inputs.* }}) in raw workflow file content.
package parser

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/importinpututil"
)

// importInputsFallbackExprRegex matches fallback expressions. The replacement
// function folds only chains whose operands are all compile-time safe.
var importInputsFallbackExprRegex = regexp.MustCompile(`\$\{\{\s*([^{}\n]*\|\|[^{}\n]*)\s*\}\}`)

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
		operands := splitFallbackOperands(expression)
		if len(operands) < 2 {
			return match
		}
		fallback := ""
		hasCompileTimeInput := false
		firstTruthy := ""
		foundTruthy := false
		for _, operand := range operands {
			value, formatted, compileTimeInput, ok := resolveFallbackOperand(strings.TrimSpace(operand), inputs)
			if !ok {
				return match
			}
			hasCompileTimeInput = hasCompileTimeInput || compileTimeInput
			fallback = formatted
			if isTruthyImportInput(value) && !foundTruthy {
				firstTruthy = formatted
				foundTruthy = true
			}
		}
		if !hasCompileTimeInput {
			return match
		}
		if foundTruthy {
			return firstTruthy
		}
		return fallback
	}
}

func splitFallbackOperands(expression string) []string {
	operands := make([]string, 0, 2)
	start := 0
	inString := false
	skipNext := false
	for i, r := range expression {
		if skipNext {
			skipNext = false
			continue
		}
		if r == '\'' {
			if inString && strings.HasPrefix(expression[i+1:], "'") {
				skipNext = true
				continue
			}
			inString = !inString
			continue
		}
		if !inString && strings.HasPrefix(expression[i:], "||") {
			operands = append(operands, expression[start:i])
			skipNext = true
			start = i + 2
		}
	}
	operands = append(operands, expression[start:])
	return operands
}

func resolveFallbackOperand(operand string, inputs map[string]any) (any, string, bool, bool) {
	if inputPath, ok := strings.CutPrefix(operand, "github.aw.import-inputs."); ok {
		return resolveFallbackInputOperand(inputPath, inputs)
	}
	if inputPath, ok := strings.CutPrefix(operand, "github.aw.inputs."); ok {
		return resolveFallbackInputOperand(inputPath, inputs)
	}
	return resolveFallbackLiteralOperand(operand)
}

func resolveFallbackInputOperand(inputPath string, inputs map[string]any) (any, string, bool, bool) {
	value, found := resolveImportInputValue(inputs, inputPath)
	if !found {
		return nil, "", true, true
	}
	formatted, ok := importinpututil.FormatResolvedValue(value)
	if !ok {
		return value, "", true, true
	}
	return value, formatted, true, true
}

func resolveFallbackLiteralOperand(operand string) (any, string, bool, bool) {
	switch operand {
	case "true":
		return true, "true", false, true
	case "false":
		return false, "false", false, true
	case "null":
		return nil, "", false, true
	}
	if strings.HasPrefix(operand, "'") && strings.HasSuffix(operand, "'") && len(operand) >= 2 {
		value := strings.ReplaceAll(operand[1:len(operand)-1], "''", "'")
		return value, value, false, true
	}
	if strings.ContainsAny(operand, ".eE") {
		if value, err := strconv.ParseFloat(operand, 64); err == nil {
			return value, operand, false, true
		}
	} else if value, err := strconv.ParseInt(operand, 10, 64); err == nil {
		return value, operand, false, true
	}
	return nil, "", false, false
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
