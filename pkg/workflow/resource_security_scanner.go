package workflow

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// ScanResourceSecurity checks non-Markdown resources for Unicode obfuscation
// without interpreting their source code or data as rendered Markdown.
func ScanResourceSecurity(content []byte) []SecurityFinding {
	if !utf8.Valid(content) {
		return nil
	}
	return scanUnicodeAbuse(string(content))
}

// FormatResourceSecurityFindings formats resource findings without describing them as Markdown.
func FormatResourceSecurityFindings(findings []SecurityFinding, filePath string) string {
	if len(findings) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Security scan found " + strconv.Itoa(len(findings)) + " issue(s) in resource content:\n\n")
	for _, finding := range findings {
		compilerErr := finding.ToFinding(filePath).CompilerError()
		findingErr := formatCompilerErrorWithPosition(
			compilerErr.Position.File,
			compilerErr.Position.Line,
			compilerErr.Position.Column,
			compilerErr.Type,
			compilerErr.Message,
			nil,
		)
		sb.WriteString(findingErr.Error())
		sb.WriteString("\n")
	}
	sb.WriteString("\nThis resource contains potentially malicious content and cannot be added.")
	return sb.String()
}
