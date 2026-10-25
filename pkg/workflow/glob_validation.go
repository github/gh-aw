// This file contains glob pattern validation logic for GitHub Actions workflow filters.
//
// The validation code is adapted from github.com/rhysd/actionlint (MIT License).
// Source: https://github.com/rhysd/actionlint/blob/v1.7.12/glob.go
//
// It is maintained here as a self-contained copy to avoid importing the full actionlint
// package (which has transitive dependency compatibility constraints). The glob validator
// depends only on standard library packages.
//
// # Functions
//
//   - validateRefGlob()  - Validates a glob pattern for Git ref names (branches/tags)
//   - validatePathGlob() - Validates a glob pattern for file paths
//   - validateGlobPatternList() - Iterates and validates a list of glob patterns
//
// Both return a slice of invalidGlobPattern describing any violations found.

package workflow

import (
	"fmt"
	"strings"
	"text/scanner"
	"unicode"

	"github.com/github/gh-aw/pkg/logger"
)

var globValidationLog = logger.New("workflow:glob_validation")

// invalidGlobPattern describes a single validation error within a glob pattern.
type invalidGlobPattern struct {
	// Message is a human-readable description of the problem.
	Message string
	// Column is the 1-based column of the error within the pattern (0 when unknown).
	Column int
}

// globValidator holds the state for iterative glob scanning.
type globValidator struct {
	isRef bool
	prec  bool
	errs  []invalidGlobPattern
	scan  scanner.Scanner
}

func (v *globValidator) error(msg string) {
	p := v.scan.Pos()
	c := p.Column - 1
	if p.Line > 1 {
		c = 0
	}
	v.errs = append(v.errs, invalidGlobPattern{msg, c})
}

func (v *globValidator) unexpected(char rune, what, why string) {
	unexpected := "unexpected EOF"
	if char != scanner.EOF {
		unexpected = fmt.Sprintf("unexpected character %q", char)
	}
	while := ""
	if what != "" {
		while = " while checking " + what
	}
	v.error(fmt.Sprintf("invalid glob pattern. %s%s. %s", unexpected, while, why))
}

func (v *globValidator) invalidRefChar(c rune, why string) {
	cfmt := "%q"
	if unicode.IsPrint(c) {
		cfmt = "'%c'"
	}
	format := "character " + cfmt + " is invalid for branch and tag names. %s. see `man git-check-ref-format` for more details. note that regular expression is unavailable"
	v.error(fmt.Sprintf(format, c, why))
}

func (v *globValidator) init(pat string) {
	v.errs = []invalidGlobPattern{}
	v.prec = false
	v.scan.Init(strings.NewReader(pat))
	v.scan.Error = func(s *scanner.Scanner, m string) {
		v.error(fmt.Sprintf("error while scanning glob pattern %q: %s", pat, m))
	}
}

//nolint:cyclop,gocognit // complexity mirrors the upstream actionlint implementation
func (v *globValidator) validateNext() bool {
	c := v.scan.Next()
	prec := true

	switch c {
	case '\\':
		switch v.scan.Peek() {
		case '[', '?', '*':
			c = v.scan.Next()
			if v.isRef {
				v.invalidRefChar(c, "ref name cannot contain spaces, ~, ^, :, [, ?, *")
			}
		case '+', '\\', '!':
			c = v.scan.Next()
		default:
			if v.isRef {
				v.invalidRefChar('\\', "only special characters [, ?, +, *, \\, ! can be escaped with \\")
				c = v.scan.Next()
			}
		}
	case '?':
		if !v.prec {
			v.unexpected('?', "special character ? (zero or one)", "the preceding character must not be special character")
		}
		prec = false
	case '+':
		if !v.prec {
			v.unexpected('+', "special character + (one or more)", "the preceding character must not be special character")
		}
		prec = false
	case '*':
		prec = false
	case '[':
		var complete bool
		c, complete = v.validateCharacterMatch()
		if !complete {
			return false
		}
	case '\r':
		if v.scan.Peek() == '\n' {
			c = v.scan.Next()
		}
		v.unexpected(c, "", "newline cannot be contained")
	case '\n':
		v.unexpected('\n', "", "newline cannot be contained")
	case ' ', '\t', '~', '^', ':':
		if v.isRef {
			v.invalidRefChar(c, "ref name cannot contain spaces, ~, ^, :, [, ?, *")
		}
	default:
	}
	v.prec = prec

	if v.scan.Peek() == scanner.EOF {
		if v.isRef && (c == '/' || c == '.') {
			v.invalidRefChar(c, "ref name must not end with / and .")
		}
		return false
	}
	return true
}

func (v *globValidator) validateCharacterMatch() (rune, bool) {
	c := rune('[')
	if v.scan.Peek() == ']' {
		c = v.scan.Next()
		v.unexpected(']', "content of character match []", "character match must not be empty")
		return c, true
	}
	chars := 0
Loop:
	for {
		c = v.scan.Next()
		switch c {
		case ']':
			break Loop
		case scanner.EOF:
			v.unexpected(c, "end of character match []", "missing ]")
			return c, false
		default:
			if v.scan.Peek() != '-' {
				chars++
				continue Loop
			}
			chars += 2
			s := c
			_ = v.scan.Next() // eat '-'; return value not needed
			switch v.scan.Peek() {
			case ']':
				c = v.scan.Next()
				v.unexpected(c, "character range in []", "end of range is missing")
				break Loop
			case scanner.EOF:
				// do nothing
			default:
				c = v.scan.Next()
				if s > c {
					why := fmt.Sprintf("start of range %q (%d) is larger than end of range %q (%d)", s, s, c, c)
					v.unexpected(c, "character range in []", why)
				}
			}
		}
	}
	if chars == 1 {
		v.unexpected(c, "character match []", "character match with single character is useless. simply use x instead of [x]")
	}
	return c, true
}

func (v *globValidator) validate(pat string) {
	v.init(pat)
	if pat == "" {
		v.error("glob pattern cannot be empty")
		return
	}
	switch v.scan.Peek() {
	case '/':
		if v.isRef {
			v.scan.Next()
			v.invalidRefChar('/', "ref name must not start with /")
			v.prec = true
		}
	case '!':
		v.scan.Next()
		if v.scan.Peek() == scanner.EOF {
			v.unexpected('!', "! at first character (negate pattern)", "at least one character must follow !")
			return
		}
		v.prec = false
	}
	for v.validateNext() {
	}
}

func runGlobValidation(pat string, isRef bool) []invalidGlobPattern {
	v := globValidator{}
	v.isRef = isRef
	v.validate(pat)
	if len(v.errs) > 0 {
		globValidationLog.Printf("Glob validation found %d error(s) for pattern %q (isRef=%t)", len(v.errs), pat, isRef)
	}
	return v.errs
}

// validateGlobPatternList validates patterns in order and stops at the first
// invalid entry.
//
// For that first invalid pattern, onInvalid receives the list index, the pattern
// value, and the collected validation messages. The return value from onInvalid
// is returned directly to the caller, and onInvalid may include side effects
// such as logging.
//
// Contract: onInvalid must return a non-nil error when called. Returning nil is
// treated as success.
func validateGlobPatternList(
	patterns []string,
	validate func(string) []invalidGlobPattern,
	onInvalid func(index int, pattern string, messages []string) error,
) error {
	for i, pat := range patterns {
		errs := validate(pat)
		if len(errs) == 0 {
			continue
		}

		msgs := make([]string, 0, len(errs))
		for _, err := range errs {
			msgs = append(msgs, err.Message)
		}

		return onInvalid(i, pat, msgs)
	}

	return nil
}

// validateRefGlob validates a GitHub Actions ref filter glob (branch or tag pattern).
// It returns a non-empty slice of invalidGlobPattern when the pattern is invalid.
func validateRefGlob(pat string) []invalidGlobPattern {
	globValidationLog.Printf("Validating ref glob pattern: %s", pat)
	errs := runGlobValidation(pat, true)
	if len(errs) > 0 {
		globValidationLog.Printf("Ref glob pattern invalid: %d error(s) found", len(errs))
	}
	return errs
}

// validatePathGlob validates a GitHub Actions path filter glob.
// It returns a non-empty slice of invalidGlobPattern when the pattern is invalid.
// Path patterns starting with "./" or "../" are explicitly rejected.
func validatePathGlob(pat string) []invalidGlobPattern {
	globValidationLog.Printf("Validating path glob pattern: %s", pat)
	p := strings.TrimSpace(pat)

	var errs []invalidGlobPattern
	if pat != p {
		errs = append(errs, invalidGlobPattern{"leading and trailing spaces are not allowed in glob path", 0})
	}

	// Reject '.', '..', './<path>', and '../<path>' (#521 in actionlint)
	stripped := strings.TrimPrefix(p, "!")
	if stripped == "." || stripped == ".." || strings.HasPrefix(stripped, "./") || strings.HasPrefix(stripped, "../") {
		globValidationLog.Printf("Path glob rejected due to invalid prefix: %s", stripped)
		errs = append(errs, invalidGlobPattern{"'.', '..', and paths starting with './' or '../' are not allowed in glob path", 0})
	}

	if len(errs) > 0 {
		return errs
	}

	return runGlobValidation(pat, false)
}
