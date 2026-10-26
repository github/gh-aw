package agentdrain

import (
	"fmt"
	"regexp"

	"github.com/github/gh-aw/pkg/logger"
)

var maskLog = logger.New("agentdrain:mask")

// Masker applies a sequence of regex substitution rules to normalize log lines.
type Masker struct {
	rules []compiledRule
}

type compiledRule struct {
	name        string
	re          *regexp.Regexp
	replacement string
}

// NewMasker compiles the given MaskRules into a Masker ready for use.
// Returns an error if any pattern fails to compile.
func NewMasker(rules []MaskRule) (*Masker, error) {
	maskLog.Printf("Compiling %d mask rules", len(rules))
	compiled := make([]compiledRule, 0, len(rules))
	for _, r := range rules {
		//nolint:regexpdynamicpattern // Mask rules are configuration input and compilation errors are returned to the caller.
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			maskLog.Printf("Failed to compile mask rule %q: %v", r.Name, err)
			return nil, fmt.Errorf("agentdrain: mask rule %q: %w", r.Name, err)
		}
		compiled = append(compiled, compiledRule{
			name:        r.Name,
			re:          re,
			replacement: r.Replacement,
		})
	}
	maskLog.Printf("Masker ready with %d compiled rules", len(compiled))
	return &Masker{rules: compiled}, nil
}

// Mask applies all mask rules in order and returns the transformed line.
func (m *Masker) Mask(line string) string {
	if maskLog.Enabled() {
		maskLog.Printf("Masking line: length=%d, rules=%d", len(line), len(m.rules))
	}
	for _, r := range m.rules {
		line = r.re.ReplaceAllString(line, r.replacement)
	}
	return line
}
