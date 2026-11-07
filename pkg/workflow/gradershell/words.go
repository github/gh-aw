package gradershell

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// LiteralWord performs quote removal only, never parameter or command
// expansion. Unknown values are not guessed from assignments or environment.
func LiteralWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	return literalParts(word.Parts, false)
}

func literalParts(parts []syntax.WordPart, quoted bool) (string, bool) {
	var result strings.Builder
	for _, part := range parts {
		switch part := part.(type) {
		case *syntax.Lit:
			value := part.Value
			for i := 0; i < len(value); i++ {
				current := value[i] //nolint:uncheckedsliceindex // The nonnegative cursor is rechecked by the loop after escaped-byte skips.
				if current == '\\' && i+1 < len(value) {
					next := value[i+1] //nolint:uncheckedsliceindex // The enclosing guard checks this lookahead; i starts at zero.
					if !quoted || strings.ContainsRune("$`\"\\\n", rune(next)) {
						i++
						if next != '\n' {
							result.WriteByte(next)
						}
						continue
					}
				}
				if !quoted && strings.ContainsRune("*?[~", rune(current)) {
					return "", false
				}
				result.WriteByte(current)
			}
		case *syntax.SglQuoted:
			if part.Dollar {
				return "", false // ANSI-C escapes require expansion, not quote removal.
			}
			result.WriteString(part.Value)
		case *syntax.DblQuoted:
			if part.Dollar {
				return "", false
			}
			value, static := literalParts(part.Parts, true)
			if !static {
				return "", false
			}
			result.WriteString(value)
		default:
			return "", false
		}
	}
	return result.String(), true
}
