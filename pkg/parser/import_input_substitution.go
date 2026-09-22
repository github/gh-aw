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
var importInputsFallbackExprRegex = regexp.MustCompile(`\$\{\{\s*([^{}\n]*(?:github\.aw\.import-inputs\.|github\.aw\.inputs\.)[^{}\n]*\|\|[^{}\n]*|[^{}\n]*\|\|[^{}\n]*(?:github\.aw\.import-inputs\.|github\.aw\.inputs\.)[^{}\n]*)\s*\}\}`)

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
		if !strings.Contains(expression, "github.aw.import-inputs.") && !strings.Contains(expression, "github.aw.inputs.") {
			return match
		}
		operands := splitFallbackOperands(expression)
		if len(operands) < 2 {
			return match
		}
		fallback := ""
		hasInputReference := false
		firstTruthy := ""
		foundTruthy := false
		for _, operand := range operands {
			resolved := resolveFallbackOperand(strings.TrimSpace(operand), inputs)
			if !resolved.ok {
				return match
			}
			hasInputReference = hasInputReference || resolved.isInputReference
			fallback = resolved.formatted
			if isTruthyImportInput(resolved.value) && !foundTruthy {
				firstTruthy = resolved.formatted
				foundTruthy = true
			}
		}
		if !hasInputReference {
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

type fallbackOperandResult struct {
	// value is the operand's raw value for truthiness checks.
	value any
	// formatted is the text substituted when the operand wins or is the final fallback.
	formatted string
	// isInputReference is true for github.aw.import-inputs.* and github.aw.inputs.* operands.
	isInputReference bool
	// ok is false when the operand is unsupported or cannot be formatted, so folding must abort.
	ok bool
}

func resolveFallbackOperand(operand string, inputs map[string]any) fallbackOperandResult {
	if inputPath, ok := strings.CutPrefix(operand, "github.aw.import-inputs."); ok {
		return resolveFallbackInputOperand(inputPath, inputs)
	}
	if inputPath, ok := strings.CutPrefix(operand, "github.aw.inputs."); ok {
		return resolveFallbackInputOperand(inputPath, inputs)
	}
	return resolveFallbackLiteralOperand(operand)
}

func resolveFallbackInputOperand(inputPath string, inputs map[string]any) fallbackOperandResult {
	value, found := resolveImportInputValue(inputs, inputPath)
	if !found || value == nil {
		return fallbackOperandResult{isInputReference: true, ok: true}
	}
	formatted, ok := importinpututil.FormatResolvedValue(value)
	if !ok {
		return fallbackOperandResult{value: value, isInputReference: true}
	}
	return fallbackOperandResult{value: value, formatted: formatted, isInputReference: true, ok: true}
}

func resolveFallbackLiteralOperand(operand string) fallbackOperandResult {
	switch operand {
	case "true":
		return fallbackOperandResult{value: true, formatted: "true", ok: true}
	case "false":
		return fallbackOperandResult{value: false, formatted: "false", ok: true}
	case "null":
		return fallbackOperandResult{ok: true}
	}
	if strings.HasPrefix(operand, "'") && strings.HasSuffix(operand, "'") && len(operand) >= 2 {
		value := strings.ReplaceAll(operand[1:len(operand)-1], "''", "'")
		return fallbackOperandResult{value: value, formatted: value, ok: true}
	}
	if strings.ContainsAny(operand, ".eE") {
		if value, err := strconv.ParseFloat(operand, 64); err == nil {
			return fallbackOperandResult{value: value, formatted: operand, ok: true}
		}
	} else if value, err := strconv.ParseInt(operand, 10, 64); err == nil {
		return fallbackOperandResult{value: value, formatted: operand, ok: true}
	}
	return fallbackOperandResult{}
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
