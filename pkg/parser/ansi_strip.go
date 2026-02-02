package parser

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var ansiStripLog = logger.New("parser:ansi_strip")

// StripANSI removes ANSI escape codes from a string.
func StripANSI(s string) string {
	if ansiStripLog.Enabled() {
		ansiStripLog.Printf("Stripping ANSI codes from string: length=%d", len(s))
	}

	if s == "" {
		return s
	}

	var result strings.Builder
	result.Grow(len(s)) // Pre-allocate capacity for efficiency

	i := 0
	escapeSequences := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			escapeSequences++
			if i+1 >= len(s) {
				// ESC at end of string, skip it
				i++
				continue
			}
			// Found ESC character, determine sequence type
			switch s[i+1] {
			case '[':
				// CSI sequence: \x1b[...final_char
				// Parameters are in range 0x30-0x3F (0-?), intermediate chars 0x20-0x2F (space-/)
				// Final characters are in range 0x40-0x7E (@-~)
				i += 2 // Skip ESC and [
				for i < len(s) {
					if isFinalCSIChar(s[i]) {
						i++ // Skip the final character
						break
					} else if isCSIParameterChar(s[i]) {
						i++ // Skip parameter/intermediate character
					} else {
						// Invalid character in CSI sequence, stop processing this escape
						break
					}
				}
			case ']':
				// OSC sequence: \x1b]...terminator
				// Terminators: \x07 (BEL) or \x1b\\ (ST)
				i += 2 // Skip ESC and ]
				for i < len(s) {
					if s[i] == '\x07' {
						i++ // Skip BEL
						break
					} else if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
						i += 2 // Skip ESC and \
						break
					}
					i++
				}
			case '(':
				// G0 character set selection: \x1b(char
				i += 2 // Skip ESC and (
				if i < len(s) {
					i++ // Skip the character
				}
			case ')':
				// G1 character set selection: \x1b)char
				i += 2 // Skip ESC and )
				if i < len(s) {
					i++ // Skip the character
				}
			case '=':
				// Application keypad mode: \x1b=
				i += 2
			case '>':
				// Normal keypad mode: \x1b>
				i += 2
			case 'c':
				// Reset: \x1bc
				i += 2
			default:
				// Other escape sequences (2-character)
				// Handle common ones like \x1b7, \x1b8, \x1bD, \x1bE, \x1bH, \x1bM
				if i+1 < len(s) && (s[i+1] >= '0' && s[i+1] <= '~') {
					i += 2
				} else {
					// Invalid or incomplete escape sequence, just skip ESC
					i++
				}
			}
		} else {
			// Regular character, keep it
			result.WriteByte(s[i])
			i++
		}
	}

	if ansiStripLog.Enabled() {
		ansiStripLog.Printf("ANSI stripping complete: input_length=%d, output_length=%d, escape_sequences=%d", len(s), result.Len(), escapeSequences)
	}

	return result.String()
}

// isFinalCSIChar checks if a character is a valid CSI final character
// Final characters are in range 0x40-0x7E (@-~)
func isFinalCSIChar(b byte) bool {
	return b >= 0x40 && b <= 0x7E
}

// isCSIParameterChar checks if a character is a valid CSI parameter or intermediate character
// Parameter characters are in range 0x30-0x3F (0-?)
// Intermediate characters are in range 0x20-0x2F (space-/)
func isCSIParameterChar(b byte) bool {
	return (b >= 0x20 && b <= 0x2F) || (b >= 0x30 && b <= 0x3F)
}
