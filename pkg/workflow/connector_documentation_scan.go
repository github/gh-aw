package workflow

import (
	"slices"

	"github.com/github/gh-aw/pkg/parser"
)

func scanImportedMarkdownSecurity(content string) []SecurityFinding {
	promptContent := parser.RemoveConnectorDocumentation(content)
	findings := ScanMarkdownSecurity(promptContent)
	if promptContent == content {
		return findings
	}
	// Omitted documentation is never prompt input, but retain all source
	// security checks within it as well.
	for _, finding := range ScanMarkdownSecurity(content) {
		if !slices.Contains(findings, finding) {
			findings = append(findings, finding)
		}
	}
	return findings
}
