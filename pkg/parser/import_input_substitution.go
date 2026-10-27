// Package parser provides functions for parsing and processing workflow markdown files.
// import_input_substitution.go implements text-level substitution of import-inputs
// expressions (${{ github.aw.import-inputs.* }}) in raw workflow file content.
package parser

import (
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/importinpututil"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// substituteImportInputsInContent performs text-level substitution of
// ${{ github.aw.import-inputs.* }} and ${{ github.aw.inputs.* }} expressions
// in raw file content (including YAML frontmatter). This is called before YAML
// parsing so that array/object values serialised as JSON produce valid YAML.
func substituteImportInputsInContent(content string, inputs map[string]any) string {
	importLog.Printf("Substituting import-inputs expressions: inputs=%d, contentBytes=%d", len(inputs), len(content))

	matches, err := actionslanguageservice.ExtractExpressions(content)
	if err != nil {
		importLog.Printf("Cannot scan import-input expressions; preserving original content")
		return content
	}
	var result strings.Builder
	last := 0
	for _, match := range matches {
		result.WriteString(content[last:match.Start])
		replacement := content[match.Start:match.End]
		expression, err := actionslanguageservice.ParseSyntax(match.Content)
		if err != nil || expression == nil {
			importLog.Printf("Cannot parse import-input expression; preserving original expression")
		} else {
			for expression.Kind == "group" {
				expression = expression.Child(0)
			}
			if expression.Kind == "logical" && expression.Token.Kind == "||" {
				resolved := resolveFallbackExpression(expression, inputs)
				if resolved.ok && resolved.isInputReference {
					replacement = resolved.formatted
				}
			} else if path, ok := importInputReference(expression); ok && len(inputs) > 0 {
				// Preserve the supported depth of standalone import references.
				reference, _ := actionslanguageservice.Reference(expression)
				if len(reference) <= 5 && (reference[2] != "inputs" || len(reference) == 4) {
					if value, found := resolveImportInputPath(inputs, path); found {
						replacement = value
					}
				}
			}
		}
		result.WriteString(replacement)
		last = match.End
	}
	result.WriteString(content[last:])
	return result.String()
}

func splitFallbackOperands(expression string) []string {
	root, err := actionslanguageservice.ParseSyntax(expression)
	if err != nil || root == nil {
		importLog.Printf("Cannot parse fallback expression")
		return nil
	}
	var operands []string
	var split func(*actionslanguageservice.Expression)
	split = func(node *actionslanguageservice.Expression) {
		if node.Kind == "logical" && node.Token.Kind == "||" {
			for _, child := range node.Children {
				split(child)
			}
		} else {
			operands = append(operands, expression[node.Start:node.End])
		}
	}
	split(root)
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
	expression, err := actionslanguageservice.ParseSyntax(operand)
	if err != nil || expression == nil {
		importLog.Printf("Cannot parse fallback operand")
		return fallbackOperandResult{}
	}
	return resolveFallbackExpression(expression, inputs)
}

func importInputReference(expression *actionslanguageservice.Expression) (string, bool) {
	path, ok := actionslanguageservice.Reference(expression)
	if !ok || len(path) < 4 || path[0] != "github" || path[1] != "aw" || (path[2] != "import-inputs" && path[2] != "inputs") {
		return "", false
	}
	for _, segment := range path[3:] {
		if segment == "" {
			return "", false
		}
		for _, ch := range segment {
			if ch != '_' && ch != '-' && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
				return "", false
			}
		}
	}
	return strings.Join(path[3:], "."), true
}

func resolveFallbackExpression(expression *actionslanguageservice.Expression, inputs map[string]any) fallbackOperandResult {
	if expression.Kind == "group" {
		return resolveFallbackExpression(expression.Child(0), inputs)
	}
	if expression.Kind == "logical" && expression.Token.Kind == "||" {
		var selected fallbackOperandResult
		hasInputReference := false
		for _, child := range expression.Children {
			resolved := resolveFallbackExpression(child, inputs)
			if !resolved.ok {
				return fallbackOperandResult{}
			}
			hasInputReference = hasInputReference || resolved.isInputReference
			if !isTruthyImportInput(selected.value) {
				selected = resolved
			}
		}
		selected.isInputReference = hasInputReference
		return selected
	}
	if inputPath, ok := importInputReference(expression); ok {
		return resolveFallbackInputOperand(inputPath, inputs)
	}
	return resolveFallbackLiteralExpression(expression)
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

func resolveFallbackLiteralExpression(expression *actionslanguageservice.Expression) fallbackOperandResult {
	if expression.Kind != "literal" {
		return fallbackOperandResult{}
	}
	switch expression.Token.Kind {
	case "true":
		return fallbackOperandResult{value: true, formatted: "true", ok: true}
	case "false":
		return fallbackOperandResult{value: false, formatted: "false", ok: true}
	case "null":
		return fallbackOperandResult{ok: true}
	case "string":
		// Decode every escaped quote for substitution without changing the
		// native lexer's upstream-compatible single-replacement Token.Value.
		text := expression.Token.Text
		value := strings.ReplaceAll(text[1:len(text)-1], "''", "'")
		return fallbackOperandResult{value: value, formatted: value, ok: true}
	case "number":
		value := expression.Token.Value
		if integer, err := strconv.ParseInt(expression.Token.Text, 10, 64); err == nil {
			value = integer
		}
		return fallbackOperandResult{value: value, formatted: expression.Token.Text, ok: true}
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
		return reflected.Float() != 0 && !math.IsNaN(reflected.Float())
	default:
		return true
	}
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
