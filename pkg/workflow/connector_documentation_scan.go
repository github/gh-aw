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
	// Omitted documentation is never prompt input, but retain the other source
	// checks (including Unicode, HTML abuse and hidden spans) within it as well.
	for _, finding := range ScanMarkdownSecurity(content) {
		if finding.Category == CategoryHiddenContent &&
			finding.Description == "HTML comment contains suspicious content (code, URLs, or executable instructions)" {
			continue
		}
		if !slices.Contains(findings, finding) {
			findings = append(findings, finding)
		}
	}
	return findings
}
