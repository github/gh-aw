package workflow

// ScanResourceSecurity checks non-Markdown resources for Unicode obfuscation
// without interpreting their source code or data as rendered Markdown.
func ScanResourceSecurity(content string) []SecurityFinding {
	return scanUnicodeAbuse(content)
}
