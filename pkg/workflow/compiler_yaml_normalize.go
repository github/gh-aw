package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var compilerYAMLNormalizeLog = logger.New("workflow:compiler_yaml_normalize")

// maxConsecutiveBlankLines is the largest run of blank lines allowed in the
// normalized YAML. yamllint's empty-lines rule flags more than two consecutive
// blank lines (max: 2), so runs are capped here to keep generated lock files clean.
const maxConsecutiveBlankLines = 2

// normalizeBlankLines rewrites the assembled workflow YAML so that:
//   - whitespace-only lines are emitted as truly empty lines,
//   - trailing whitespace is trimmed from non-block-scalar content lines,
//   - no more than maxConsecutiveBlankLines blank lines appear in a row outside
//     block scalars, and
//   - the file ends with exactly one trailing newline (no trailing blank line).
//
// YAML literal/folded block scalars carry raw payload content, so their non-blank
// lines and blank-line runs must be preserved exactly. This pass therefore only
// trims/caps generator-owned structural YAML, while still clearing indentation-only
// blank lines everywhere to remove yamllint trailing-spaces and empty-lines noise.
//
// This implementation avoids strings.Split/strings.Join to reduce allocations: it scans
// the input byte-by-byte and builds the result with a single pre-allocated strings.Builder.
func normalizeBlankLines(yamlContent string) string {
	compilerYAMLNormalizeLog.Printf("Normalizing blank lines in %d bytes of YAML", len(yamlContent))
	var b strings.Builder
	b.Grow(len(yamlContent))

	// lastNonBlankEnd tracks the builder length immediately after writing the last
	// non-blank line (including its trailing newline). It starts at 0 and is only
	// advanced when a substantive line is written, so it stays 0 when all lines
	// are whitespace-only or the input is empty. Every line — blank or not — still
	// gets a '\n' written to b, so b.Len() and lastNonBlankEnd may diverge when
	// there are trailing blank lines.
	lastNonBlankEnd := 0
	// blankRun counts consecutive blank lines emitted since the last non-blank
	// structural line, so runs longer than maxConsecutiveBlankLines can be
	// collapsed outside block scalars.
	blankRun := 0
	var scalar yamlBlankLineScalarState
	pos := 0
	for pos < len(yamlContent) {
		// Find the end of the current line.
		end := strings.IndexByte(yamlContent[pos:], '\n')
		var line string
		if end == -1 {
			line = yamlContent[pos:]
		} else {
			line = yamlContent[pos : pos+end]
		}

		processStructuralLine := true
		trimmed := strings.TrimRight(line, " \t")
		if scalar.writeLine(&b, line, trimmed) {
			processStructuralLine = false
			if trimmed != "" {
				lastNonBlankEnd = b.Len()
			}
		}

		if processStructuralLine {
			if writeStructuralYAMLLine(&b, trimmed, &blankRun, &scalar) {
				lastNonBlankEnd = b.Len()
			}
		}

		if end == -1 {
			break
		}
		pos += end + 1
	}
	return finishNormalizedYAML(&b, lastNonBlankEnd)
}

func finishNormalizedYAML(b *strings.Builder, lastNonBlankEnd int) string {
	if lastNonBlankEnd == 0 {
		compilerYAMLNormalizeLog.Print("Input contained no non-blank lines, returning single newline")
		return "\n"
	}
	compilerYAMLNormalizeLog.Printf("Normalized YAML to %d bytes", lastNonBlankEnd)
	return b.String()[:lastNonBlankEnd]
}

func writeStructuralYAMLLine(b *strings.Builder, trimmed string, blankRun *int, scalar *yamlBlankLineScalarState) bool {
	if trimmed == "" {
		if *blankRun < maxConsecutiveBlankLines {
			b.WriteByte('\n')
			*blankRun++
		}
		return false
	}
	b.WriteString(trimmed)
	b.WriteByte('\n')
	*blankRun = 0
	if headerIndent, ok := blockScalarHeaderIndentForLine(trimmed); ok {
		scalar.pending = true
		scalar.headerIndent = headerIndent
	}
	return true
}

type yamlBlankLineScalarState struct {
	pending, active      bool
	headerIndent, indent int
}

func (s *yamlBlankLineScalarState) writeLine(b *strings.Builder, line, trimmed string) bool {
	if !s.pending && !s.active {
		return false
	}
	if trimmed == "" {
		// Clear whitespace-only scalar lines without capping their run.
		b.WriteByte('\n')
		return true
	}
	lineIndent := countLeadingSpaces(line)
	if s.pending {
		if lineIndent <= s.headerIndent {
			s.pending = false
		} else {
			s.indent = lineIndent
			s.active = true
			s.pending = false
		}
	}
	if s.active {
		if lineIndent < s.indent {
			s.active = false
		} else {
			b.WriteString(line)
			b.WriteByte('\n')
			return true
		}
	}
	return false
}

func countLeadingSpaces(line string) int {
	count := 0
	for count < len(line) && line[count] == ' ' {
		count++
	}
	return count
}

func blockScalarHeaderIndentForLine(line string) (int, bool) {
	colon := strings.LastIndexByte(line, ':')
	if colon == -1 {
		return 0, false
	}

	rest := strings.TrimSpace(line[colon+1:])
	if rest == "" || (rest[0] != '|' && rest[0] != '>') {
		return 0, false
	}

	rest = rest[1:]
	for rest != "" {
		switch rest[0] {
		case '+', '-':
			rest = rest[1:]
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			rest = rest[1:]
		default:
			goto indicatorsDone
		}
	}

indicatorsDone:
	rest = strings.TrimSpace(rest)
	if rest != "" && rest[0] != '#' {
		return 0, false
	}

	return countLeadingSpaces(line), true
}
