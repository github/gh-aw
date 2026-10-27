// Package actionslanguageservice implements the actions/languageservices parser
// grammar in Go. It does not evaluate expressions or execute workflow code.
package actionslanguageservice

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxExpressionLength = 21000
	MaxExpressionDepth  = 50
)

// Token retains the original spelling and byte offset for diagnostics.
type Token struct {
	Kind   string
	Text   string
	Value  any
	Offset int
}

// Tokenize exposes the official lexer for source-preserving formatting.
func Tokenize(source string) ([]Token, error) {
	return lex(source)
}

func lex(source string) ([]Token, error) {
	if exceedsExpressionLength(source) {
		return nil, fmt.Errorf("exceeded max expression length %d", MaxExpressionLength)
	}
	var tokens []Token
	previous := ""
	for i := 0; i >= 0 && i < len(source); {
		if strings.ContainsRune(" \r\t\n", rune(source[i])) {
			i++
			continue
		}
		start := i
		c := source[i]
		i++
		token := Token{Kind: string(c), Offset: start}
		switch c {
		case '(', ')', '[', ']', ',', '*':
		case '!', '=', '>', '<', '&', '|':
			if i < len(source) && ((source[i] == '=' && c != '&' && c != '|') ||
				(source[i] == c && (c == '&' || c == '|'))) {
				i++
				token.Kind = source[start:i]
			} else if c == '=' || c == '&' || c == '|' {
				return nil, unexpected(source, start, string(c))
			}
		case '\'':
			closed := false
			for i < len(source) {
				if source[i] == '\'' {
					i++
					if i == len(source) || source[i] != '\'' {
						closed = true
						break
					}
				}
				i++
			}
			if !closed {
				return nil, unexpected(source, start, source[start:])
			}
			// Match the pinned upstream lexer's single replacement, including
			// its behavior when a literal contains multiple doubled quotes.
			token.Kind, token.Value = "string", strings.Replace(source[start+1:i-1], "''", "'", 1)
		default:
			if c == '.' && (previous == "identifier" || previous == "]" || previous == ")" || previous == "*") {
				break
			}
			var err error
			token, i, err = lexValue(source, start, c, previous)
			if err != nil {
				return nil, err
			}
		}
		token.Text = source[start:i]
		tokens = append(tokens, token)
		previous = token.Kind
	}
	return append(tokens, Token{Kind: "eof", Offset: len(source)}), nil
}

func exceedsExpressionLength(source string) bool {
	length := 0
	for _, r := range source {
		length++
		if r > 0xffff {
			length++
		}
		if length > MaxExpressionLength {
			return true
		}
	}
	return false
}

func lexValue(source string, start int, first byte, previous string) (Token, int, error) {
	number := first >= '0' && first <= '9' || first == '.' || first == '-' || first == '+'
	end := start + 1
	for end >= 0 && end < len(source) && (!isBoundary(source[end:]) || number && source[end] == '.') {
		end++
	}
	text := source[start:end]
	token := Token{Kind: "identifier", Offset: start}
	if number {
		value, err := parseNumber(text)
		if err != nil {
			return Token{}, end, unexpected(source, start, text)
		}
		token.Kind, token.Value = "number", value
		return token, end, nil
	}
	if !isIdentifier(text) {
		return Token{}, end, unexpected(source, start, text)
	}
	if previous != "." {
		switch text {
		case "true", "false", "null":
			token.Kind = text
		case "NaN":
			token.Kind, token.Value = "number", math.NaN()
		case "Infinity":
			token.Kind, token.Value = "number", math.Inf(1)
		}
	}
	return token, end, nil
}

func isBoundary(source string) bool {
	if strings.ContainsRune("([)],.!><=&|", rune(source[0])) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(source)
	return unicode.IsSpace(r)
}

func isIdentifier(text string) bool {
	for i, r := range text {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' ||
			i > 0 && (r >= '0' && r <= '9' || r == '-') {
			continue
		}
		return false
	}
	return text != ""
}

func parseNumber(text string) (float64, error) {
	if text == "" || !strings.ContainsRune("0123456789.+-", rune(text[0])) {
		return 0, fmt.Errorf("invalid number %q", text)
	}
	// JavaScript Number(), used by the upstream lexer, accepts hexadecimal,
	// binary and octal integers as well as decimal/exponent notation.
	if len(text) > 2 && text[0] == '0' && strings.ContainsRune("xXbBoO", rune(text[1])) {
		base := 16
		switch text[1] {
		case 'b', 'B':
			base = 2
		case 'o', 'O':
			base = 8
		}
		value := 0.0
		for _, r := range strings.ToLower(text[2:]) {
			digit := strings.IndexRune("0123456789abcdef", r)
			if digit < 0 || digit >= base {
				return 0, fmt.Errorf("invalid number %q", text)
			}
			value = value*float64(base) + float64(digit)
		}
		return value, nil
	}
	// strconv accepts Go-only syntax (underscores, Inf, hexadecimal floats).
	if strings.ContainsAny(text, "_pPxX") || strings.Contains(text, "Inf") && text != "+Infinity" && text != "-Infinity" {
		return 0, fmt.Errorf("invalid number %q", text)
	}
	if text == "+Infinity" || text == "-Infinity" {
		sign := 1
		if text[0] == '-' {
			sign = -1
		}
		return math.Inf(sign), nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if math.IsInf(value, 0) {
		return value, nil
	}
	return value, err
}

func unexpected(source string, offset int, text string) error {
	return fmt.Errorf("unexpected symbol %q at position %d in expression: %s", text, offset+1, source)
}
