// This file provides security scanning for markdown workflow content.
//
// # Markdown Security Scanner
//
// This file detects dangerous or malicious content in markdown workflow files
// that are being added from untrusted sources via `gh aw add` or `gh aw trial`.
// It provides hard errors with no override for the following categories:
//
//   - Unicode abuse: zero-width characters, bidi overrides, control characters
//   - Hidden content: HTML comments with suspicious payloads, hidden spans, CSS hiding
//   - Obfuscated links: data URIs, mismatched link text/URLs, encoded URLs
//   - HTML abuse: script/iframe/object/embed tags, event handlers
//   - Embedded files: SVG with scripts, data-URI payloads in images
//   - Social engineering: misleading formatting patterns
//
// # Usage
//
// Call ScanMarkdownSecurity(content) before writing any externally-sourced
// workflow file to disk. Returns a list of SecurityFinding values, each
// describing a specific issue found. If the list is non-empty, the workflow
// should be rejected.

package workflow

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/stringutil"
	"golang.org/x/net/html"
)

var markdownSecurityLog = logger.New("workflow:markdown_security_scanner")

// SecurityFindingCategory represents a category of security finding
type SecurityFindingCategory string

const (
	// CategoryUnicodeAbuse covers zero-width characters, bidi overrides, and control characters
	CategoryUnicodeAbuse SecurityFindingCategory = "unicode-abuse"
	// CategoryHiddenContent covers HTML comments with payloads, hidden spans, CSS hiding
	CategoryHiddenContent SecurityFindingCategory = "hidden-content"
	// CategoryObfuscatedLinks covers data URIs, mismatched links, encoded URLs
	CategoryObfuscatedLinks SecurityFindingCategory = "obfuscated-links"
	// CategoryHTMLAbuse covers script/iframe/object/embed tags and event handlers
	CategoryHTMLAbuse SecurityFindingCategory = "html-abuse"
	// CategoryEmbeddedFiles covers SVG scripts, data-URI image payloads
	CategoryEmbeddedFiles SecurityFindingCategory = "embedded-files"
	// CategorySocialEngineering covers misleading formatting and disguised commands
	CategorySocialEngineering SecurityFindingCategory = "social-engineering"
)

// SecurityFinding represents a single security issue found in markdown content
type SecurityFinding struct {
	Category    SecurityFindingCategory
	Description string
	Line        int    // 1-based line number where the issue was found, 0 if unknown
	Snippet     string // Short excerpt of the problematic content
}

// ToFinding converts the security finding to the shared finding representation
// used by the scanner integrations. Markdown security findings are hard
// rejections, so they are always reported at high severity.
func (f SecurityFinding) ToFinding(filePath string) scanfindings.Finding {
	return scanfindings.Finding{
		RuleID:   string(f.Category),
		Severity: scanfindings.SeverityHigh,
		Message:  fmt.Sprintf("[%s] %s", f.Category, f.Description),
		File:     filePath,
		Line:     f.Line,
		Column:   1,
	}
}

// countCategories counts unique security finding categories
func countCategories(findings []SecurityFinding) int {
	categories := make(map[SecurityFindingCategory]bool)
	for _, f := range findings {
		categories[f.Category] = true
	}
	return len(categories)
}

// ScanMarkdownSecurity scans markdown content for dangerous or malicious patterns.
// It automatically strips YAML frontmatter (delimited by ---) so that only the
// markdown body is scanned. Line numbers in returned findings are adjusted to
// match the original file. Returns a list of findings. If non-empty, the content
// should be rejected.
func ScanMarkdownSecurity(content string) []SecurityFinding {
	markdownSecurityLog.Printf("Scanning markdown content (%d bytes) for security issues", len(content))

	// Strip frontmatter and get the line offset for correct line number reporting
	markdownBody, lineOffset := stripFrontmatter(content)
	markdownSecurityLog.Printf("Stripped frontmatter: %d line(s) removed, scanning %d bytes of markdown", lineOffset, len(markdownBody))

	var findings []SecurityFinding

	markdownSecurityLog.Print("Running unicode abuse detection")
	findings = append(findings, scanUnicodeAbuse(markdownBody)...)
	markdownSecurityLog.Print("Running hidden content detection")
	findings = append(findings, scanHiddenContent(markdownBody)...)
	markdownSecurityLog.Print("Running obfuscated links detection")
	findings = append(findings, scanObfuscatedLinks(markdownBody)...)
	markdownSecurityLog.Print("Running HTML abuse detection")
	findings = append(findings, scanHTMLAbuse(markdownBody)...)
	markdownSecurityLog.Print("Running embedded files detection")
	findings = append(findings, scanEmbeddedFiles(markdownBody)...)
	markdownSecurityLog.Print("Running social engineering detection")
	findings = append(findings, scanSocialEngineering(markdownBody)...)

	// Adjust line numbers to account for stripped frontmatter
	if lineOffset > 0 {
		for i := range findings {
			if findings[i].Line > 0 {
				findings[i].Line += lineOffset
			}
		}
	}

	if len(findings) > 0 {
		markdownSecurityLog.Printf("Security scan complete: found %d issue(s) across %d categor(ies)", len(findings), countCategories(findings))
	} else {
		markdownSecurityLog.Print("Security scan complete: no issues found")
	}

	return findings
}

// stripFrontmatter removes YAML frontmatter (--- delimited) from content.
// Returns the markdown body and the number of lines consumed by frontmatter
// (including the closing --- delimiter) so callers can adjust line numbers.
func stripFrontmatter(content string) (string, int) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content, 0
	}

	// Find closing ---
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			// Return everything after the closing ---
			remaining := strings.Join(lines[i+1:], "\n")
			return remaining, i + 1 // i+1 lines consumed (0-indexed i, plus the closing ---)
		}
	}

	// No closing --- found; treat as frontmatter-only with no markdown body to scan
	return "", 0
}

// FormatSecurityFindings formats a list of findings into a human-readable error message
// filePath: the workflow file path to include in error messages
func FormatSecurityFindings(findings []SecurityFinding, filePath string) string {
	if len(findings) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Security scan found %d issue(s) in workflow markdown:\n\n", len(findings))

	// Format each finding using formatCompilerErrorWithPosition for consistency
	for _, f := range findings {
		// Render through the shared finding representation so markdown security
		// findings use the same severity vocabulary as the scanner integrations.
		compilerErr := f.ToFinding(filePath).CompilerError()

		// Create a formatted error for this finding
		findingErr := formatCompilerErrorWithPosition(
			compilerErr.Position.File,
			compilerErr.Position.Line,
			compilerErr.Position.Column,
			compilerErr.Type,
			compilerErr.Message,
			nil,
		)

		// Append the formatted error to our output
		sb.WriteString(findingErr.Error())
		sb.WriteString("\n")
	}

	sb.WriteString("\nThis workflow contains potentially malicious content and cannot be added.")
	return sb.String()
}

// --- Unicode Abuse Detection ---

// Zero-width and invisible Unicode characters that should never appear in workflows
var dangerousUnicodeRunes = map[rune]string{
	'\u200B': "zero-width space (U+200B)",
	'\u200C': "zero-width non-joiner (U+200C)",
	'\u200D': "zero-width joiner (U+200D)",
	'\uFEFF': "zero-width no-break space / BOM (U+FEFF)",
	'\u00AD': "soft hyphen (U+00AD)",
	'\u200E': "left-to-right mark (U+200E)",
	'\u200F': "right-to-left mark (U+200F)",
	'\u2060': "word joiner (U+2060)",
	'\u2061': "function application (U+2061)",
	'\u2062': "invisible times (U+2062)",
	'\u2063': "invisible separator (U+2063)",
	'\u2064': "invisible plus (U+2064)",
	'\u180E': "Mongolian vowel separator (U+180E)",
}

// Bidi override characters used in Trojan Source attacks
var bidiOverrideRunes = map[rune]string{
	'\u202A': "left-to-right embedding (U+202A)",
	'\u202B': "right-to-left embedding (U+202B)",
	'\u202C': "pop directional formatting (U+202C)",
	'\u202D': "left-to-right override (U+202D)",
	'\u202E': "right-to-left override (U+202E)",
	'\u2066': "left-to-right isolate (U+2066)",
	'\u2067': "right-to-left isolate (U+2067)",
	'\u2068': "first strong isolate (U+2068)",
	'\u2069': "pop directional isolate (U+2069)",
}

// isEmojiLike reports whether r is in a Unicode range commonly used for emoji.
// This is used to distinguish legitimate emoji ZWJ sequences (e.g., 🧑‍🤝‍🧑)
// from abusive uses of U+200D to hide or obfuscate ASCII text.
//
// Ranges covered:
//   - U+2194–U+27BF: arrows, misc symbols, and dingbats (includes ❤ U+2764, ♀ U+2640)
//   - U+FE00–U+FE0F: variation selectors (e.g., U+FE0F forces emoji presentation)
//   - U+1F000+:      supplementary multilingual plane where most modern emoji live
func isEmojiLike(r rune) bool {
	return (r >= 0x2194 && r <= 0x27BF) || // arrows, misc symbols, dingbats
		(r >= 0xFE00 && r <= 0xFE0F) || // variation selectors
		r >= 0x1F000 // supplementary multilingual plane (most modern emoji)
}

func scanUnicodeAbuse(content string) []SecurityFinding {
	var findings []SecurityFinding
	lines := strings.Split(content, "\n")

	markdownSecurityLog.Printf("Scanning %d line(s) for unicode abuse", len(lines))

	for lineNum, line := range lines {
		findings = append(findings, scanUnicodeAbuseLine(line, lineNum+1)...)
	}

	return findings
}

func scanUnicodeAbuseLine(line string, lineNo int) []SecurityFinding {
	var findings []SecurityFinding
	var prevRune rune
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		if r == utf8.RuneError && size <= 1 {
			i++
			continue
		}
		if name, ok := dangerousUnicodeRunes[r]; ok {
			// U+200D (ZWJ) is a standard component of emoji sequences such as
			// 🧑‍🤝‍🧑 (people holding hands) or 👨‍👩‍👧 (family). Only flag it when
			// it is NOT flanked by emoji-range codepoints on both sides.
			// prevRune is 0 (null) at the start of each line, so a ZWJ at
			// the beginning of a line is always flagged (isEmojiLike(0)==false).
			if r == '\u200D' {
				var nextRune rune
				if i+size < len(line) {
					nextRune, _ = utf8.DecodeRuneInString(line[i+size:])
				}
				if isEmojiLike(prevRune) && isEmojiLike(nextRune) {
					prevRune = r
					i += size
					continue
				}
			}
			findings = append(findings, SecurityFinding{
				Category:    CategoryUnicodeAbuse,
				Description: "contains invisible character: " + name,
				Line:        lineNo,
				Snippet:     stringutil.Truncate(strings.TrimSpace(line), 80),
			})
		}

		if name, ok := bidiOverrideRunes[r]; ok {
			findings = append(findings, SecurityFinding{
				Category:    CategoryUnicodeAbuse,
				Description: "contains bidirectional override character: " + name,
				Line:        lineNo,
				Snippet:     stringutil.Truncate(strings.TrimSpace(line), 80),
			})
		}

		// Check for C0/C1 control characters (except common whitespace)
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			// Skip BOM which is already handled above
			if r != '\uFEFF' {
				findings = append(findings, SecurityFinding{
					Category:    CategoryUnicodeAbuse,
					Description: fmt.Sprintf("contains control character U+%04X", r),
					Line:        lineNo,
					Snippet:     stringutil.Truncate(strings.TrimSpace(line), 80),
				})
			}
		}

		prevRune = r
		i += size
	}

	return findings
}

// --- Hidden Content Detection ---

// Patterns for hidden content in HTML
var (
	// HTML comments that contain suspicious content (not simple TODO/NOTE comments)
	htmlCommentPattern = regexp.MustCompile(`(?s)<!--(.*?)-->`)

	// Hidden spans and divs using CSS
	cssHiddenPattern = regexp.MustCompile(`(?i)<(span|div|p|section|article)[^>]*style\s*=\s*["'][^"']*(?:display\s*:\s*none|visibility\s*:\s*hidden|opacity\s*:\s*0|font-size\s*:\s*0|height\s*:\s*0|width\s*:\s*0|overflow\s*:\s*hidden)`)

	// HTML entity obfuscation (sequences of HTML entities that could hide text)
	htmlEntitySequencePattern = regexp.MustCompile(`(?:&#x?[0-9a-fA-F]+;){4,}`)
)

func scanHiddenContent(content string) []SecurityFinding {
	var findings []SecurityFinding
	lines := strings.Split(content, "\n")

	// Check for HTML comments containing suspicious content
	matches := htmlCommentPattern.FindAllStringSubmatchIndex(content, -1)
	for _, match := range matches {
		commentBody := content[match[2]:match[3]]
		commentLine := lineNumberAt(content, match[0])

		// Flag comments that contain code-like content, URLs, or suspicious keywords
		lowerComment := strings.ToLower(commentBody)
		if containsSuspiciousCommentContent(lowerComment) {
			findings = append(findings, SecurityFinding{
				Category:    CategoryHiddenContent,
				Description: "HTML comment contains suspicious content (code, URLs, or executable instructions)",
				Line:        commentLine,
				Snippet:     stringutil.Truncate(strings.TrimSpace(commentBody), 80),
			})
		}
	}

	// Check for CSS-hidden elements
	for lineNum, line := range lines {
		lineNo := lineNum + 1

		if cssHiddenPattern.MatchString(line) {
			findings = append(findings, SecurityFinding{
				Category:    CategoryHiddenContent,
				Description: "HTML element uses CSS to hide content (display:none, visibility:hidden, opacity:0, etc.)",
				Line:        lineNo,
				Snippet:     stringutil.Truncate(strings.TrimSpace(line), 80),
			})
		}

		// Check for HTML entity obfuscation
		if htmlEntitySequencePattern.MatchString(line) {
			findings = append(findings, SecurityFinding{
				Category:    CategoryHiddenContent,
				Description: "contains sequence of HTML entities that may be obfuscating text",
				Line:        lineNo,
				Snippet:     stringutil.Truncate(strings.TrimSpace(line), 80),
			})
		}
	}

	return findings
}

// suspiciousCommentWordPatterns matches short shell keywords as whole words
// to avoid false positives (e.g. "sh " matching inside "fresh data")
var suspiciousCommentWordPatterns = regexp.MustCompile(`(?i)(?:^|\s)(?:sh|bash)\s`)

// containsSuspiciousCommentContent checks if an HTML comment body contains suspicious content
func containsSuspiciousCommentContent(lowerComment string) bool {
	// Check short keywords that need word-boundary matching
	if suspiciousCommentWordPatterns.MatchString(lowerComment) {
		return true
	}

	// Exact substring patterns (specific enough to avoid false positives)
	suspiciousPatterns := []string{
		"curl ", "wget ", "eval(",
		"base64", "exec(", "system(",
		"<script", "<iframe", "<object",
		"javascript:", "vbscript:",
		// Data URI patterns with MIME types (avoids matching "metadata:", "PR data:", etc.)
		"data:text/", "data:application/", "data:image/svg",
		"require(",
		"document.", "window.",
		"fetch(", "xmlhttprequest",
		"prompt injection", "ignore previous", "ignore above",
		"override instructions", "new instructions",
		"you are now", "act as",
	}

	for _, pattern := range suspiciousPatterns {
		if strings.Contains(lowerComment, pattern) {
			return true
		}
	}

	return false
}

// --- Obfuscated Links Detection ---

var (
	// Markdown links: [text](url), with support for escaped characters inside text and URL
	markdownLinkPattern = regexp.MustCompile(`\[((?:\\.|[^\]\\])*)\]\(((?:\\.|[^\\)])+)\)`)

	// Markdown images: ![alt](url), with support for escaped characters inside alt text and URL
	markdownImagePattern = regexp.MustCompile(`!\[((?:\\.|[^\]\\])*)\]\(((?:\\.|[^\\)])+)\)`)

	// Data URI pattern
	dataURIPattern = regexp.MustCompile(`(?i)\bdata:[ \t]*[a-zA-Z]+/[a-zA-Z0-9.+\-]+[ \t]*[;,]`)

	// Multiple URL encoding (percent-encoded percent signs)
	multipleEncodingPattern = regexp.MustCompile(`%25[0-9a-fA-F]{2}`)

	// IP address URLs (not domain names)
	ipAddressURLPattern = regexp.MustCompile(`(?i)https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)

	// URL shortener domains
	urlShortenerPattern = regexp.MustCompile(`(?i)https?://(?:bit\.ly|t\.co|tinyurl\.com|goo\.gl|ow\.ly|is\.gd|buff\.ly|rebrand\.ly|shorturl\.at|tiny\.cc)/`)

	// Suspicious query parameters
	suspiciousQueryParamPattern = regexp.MustCompile(`(?i)[?&](?:token|key|auth|secret|password|credential|api_key|apikey|access_token)=`)
)

func scanObfuscatedLinks(content string) []SecurityFinding {
	var findings []SecurityFinding
	lines := strings.Split(content, "\n")

	for lineNum, line := range lines {
		lineNo := lineNum + 1

		linkMatches := markdownLinkPattern.FindAllStringSubmatch(line, -1)
		for _, m := range linkMatches {
			findings = append(findings, scanMarkdownLinkSecurity(m, lineNo)...)
		}

		imageMatches := markdownImagePattern.FindAllStringSubmatch(line, -1)
		for _, m := range imageMatches {
			imageURL := m[2]

			if dataURIPattern.MatchString(imageURL) {
				findings = append(findings, SecurityFinding{
					Category:    CategoryObfuscatedLinks,
					Description: "markdown image uses a data: URI which can embed executable content",
					Line:        lineNo,
					Snippet:     stringutil.Truncate(strings.TrimSpace(m[0]), 80),
				})
			}
		}
	}

	return findings
}

func scanMarkdownLinkSecurity(match []string, lineNo int) []SecurityFinding {
	var findings []SecurityFinding
	linkURL := match[2]
	addFinding := func(description string) {
		findings = append(findings, SecurityFinding{
			Category:    CategoryObfuscatedLinks,
			Description: description,
			Line:        lineNo,
			Snippet:     stringutil.Truncate(strings.TrimSpace(match[0]), 80),
		})
	}

	if dataURIPattern.MatchString(linkURL) {
		addFinding("markdown link uses a data: URI which can embed executable content")
	}
	if multipleEncodingPattern.MatchString(linkURL) {
		addFinding("markdown link URL is multiply-encoded (possible obfuscation)")
	}
	if ipAddressURLPattern.MatchString(linkURL) {
		addFinding("markdown link points to an IP address instead of a domain name")
	}
	if urlShortenerPattern.MatchString(linkURL) {
		addFinding("markdown link uses a URL shortener which hides the true destination")
	}
	if suspiciousQueryParamPattern.MatchString(linkURL) {
		addFinding("markdown link URL contains suspicious authentication parameters (token, key, secret)")
	}

	lowerURL := strings.ToLower(strings.TrimSpace(linkURL))
	if strings.HasPrefix(lowerURL, "javascript:") || strings.HasPrefix(lowerURL, "vbscript:") || strings.HasPrefix(lowerURL, "data:") {
		addFinding("markdown link uses dangerous protocol: " + strings.SplitN(lowerURL, ":", 2)[0])
	}

	return findings
}

// --- HTML Abuse Detection ---

var (
	// Dangerous HTML elements
	scriptTagPattern           = regexp.MustCompile(`(?i)<\s*script(?:[\s>/]|$)`)
	scriptElementPattern       = regexp.MustCompile(`(?is)<\s*script([^>]*)>(.*?)<\s*/\s*script\s*>`)
	scriptSrcOccurrencePattern = regexp.MustCompile(`(?i)(?:^|\s)src\s*=`)
	baseTagPattern             = regexp.MustCompile(`(?i)<\s*base(?:[\s>/]|$)`)
	iframeTagPattern           = regexp.MustCompile(`(?i)<\s*iframe[\s>]`)
	objectTagPattern           = regexp.MustCompile(`(?i)<\s*object[\s>]`)
	embedTagPattern            = regexp.MustCompile(`(?i)<\s*embed[\s>]`)
	linkTagPattern             = regexp.MustCompile(`(?i)<\s*link\s[^>]*rel\s*=\s*["']stylesheet`)
	metaRefreshPattern         = regexp.MustCompile(`(?i)<\s*meta\s[^>]*http-equiv\s*=\s*["']refresh`)
	styleTagPattern            = regexp.MustCompile(`(?i)<\s*style[\s>]`)
	formTagPattern             = regexp.MustCompile(`(?i)<\s*form[\s>]`)

	// Event handlers in HTML attributes
	eventHandlerPattern = regexp.MustCompile(`(?i)\s+on(?:load|click|error|mouseover|mouseout|focus|blur|submit|change|input|keyup|keydown|keypress|dblclick|contextmenu|drag|drop|copy|paste)\s*=`)

	dangerousHTMLChecks = [...]struct {
		pattern *regexp.Regexp
		desc    string
	}{
		{iframeTagPattern, "<iframe> tag can embed external content"},
		{objectTagPattern, "<object> tag can embed executable content"},
		{embedTagPattern, "<embed> tag can embed executable content"},
		{linkTagPattern, "<link rel=\"stylesheet\"> can load external resources"},
		{metaRefreshPattern, "<meta http-equiv=\"refresh\"> can redirect to malicious URLs"},
		{formTagPattern, "<form> tag can submit data to external servers"},
	}
)

func scanHTMLAbuse(content string) []SecurityFinding {
	var findings []SecurityFinding
	lines := strings.Split(content, "\n")
	scriptLines := strings.Split(maskSafeLocalScriptElements(content), "\n")

	// Track if we're inside a code block (fenced or indented)
	inCodeBlock := false
	codeBlockDelimiter := ""

	for lineNum, line := range lines {
		lineNo := lineNum + 1
		trimmed, isFence := markdownCodeFence(scriptLines[lineNum])

		// Track code blocks to avoid false positives
		if isFence {
			if !inCodeBlock {
				inCodeBlock = true
				codeBlockDelimiter = trimmed[:3]
			} else if isClosingCodeFence(trimmed, codeBlockDelimiter) {
				inCodeBlock = false
				codeBlockDelimiter = ""
			}
			continue
		}

		// Skip content inside code blocks
		if inCodeBlock {
			continue
		}

		findings = append(findings, scanHTMLLine(line, scriptLines[lineNum], lineNo)...)
	}

	return findings
}

func scanHTMLLine(line, scriptLine string, lineNo int) []SecurityFinding {
	var findings []SecurityFinding
	addFinding := func(description string) {
		findings = append(findings, SecurityFinding{
			Category:    CategoryHTMLAbuse,
			Description: description,
			Line:        lineNo,
			Snippet:     stringutil.Truncate(strings.TrimSpace(line), 80),
		})
	}

	if scriptTagPattern.MatchString(scriptLine) {
		addFinding("<script> tag can execute arbitrary JavaScript")
	}
	for _, check := range dangerousHTMLChecks {
		if check.pattern.MatchString(line) {
			addFinding(check.desc)
		}
	}
	if styleTagPattern.MatchString(line) {
		addFinding("<style> tag can be used to hide content or mislead users")
	}
	if eventHandlerPattern.MatchString(line) {
		addFinding("HTML element contains event handler attribute (onclick, onload, onerror, etc.)")
	}

	return findings
}

func maskSafeLocalScriptElements(content string) string {
	if baseTagPattern.MatchString(content) {
		return content
	}

	return scriptElementPattern.ReplaceAllStringFunc(content, func(element string) string {
		if !isSafeLocalScriptElement(element) {
			return element
		}

		return strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' {
				return r
			}
			return ' '
		}, element)
	})
}

func isSafeLocalScriptElement(element string) bool {
	tokenizer := html.NewTokenizer(strings.NewReader(element))
	if tokenizer.Next() != html.StartTagToken {
		return false
	}
	if len(scriptSrcOccurrencePattern.FindAll(tokenizer.Raw(), -1)) != 1 {
		return false
	}
	tagName, hasAttribute := tokenizer.TagName()
	if string(tagName) != "script" {
		return false
	}
	attributes := make([]html.Attribute, 0)
	for hasAttribute {
		key, value, moreAttributes := tokenizer.TagAttr()
		attributes = append(attributes, html.Attribute{Key: string(key), Val: string(value)})
		hasAttribute = moreAttributes
	}
	if !hasLocalJavaScriptSource(attributes) {
		return false
	}

	next := tokenizer.Next()
	if next == html.TextToken {
		if strings.TrimSpace(string(tokenizer.Raw())) != "" {
			return false
		}
		next = tokenizer.Next()
	}
	if next != html.EndTagToken {
		return false
	}
	endTagName, _ := tokenizer.TagName()
	if string(endTagName) != "script" {
		return false
	}

	return tokenizer.Next() == html.ErrorToken && errors.Is(tokenizer.Err(), io.EOF)
}

func hasLocalJavaScriptSource(attributes []html.Attribute) bool {
	var source string
	sourceCount := 0
	for _, attribute := range attributes {
		if strings.HasPrefix(attribute.Key, "on") {
			return false
		}
		if attribute.Key == "src" {
			sourceCount++
			if sourceCount > 1 {
				return false
			}
			source = strings.TrimSpace(attribute.Val)
		}
	}

	if source == "" || strings.HasPrefix(source, "/") || strings.Contains(source, `\`) {
		return false
	}

	parsed, err := url.Parse(source)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Opaque != "" || strings.HasPrefix(parsed.Path, "/") {
		return false
	}

	switch strings.ToLower(path.Ext(path.Clean(parsed.Path))) {
	case ".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

// --- Embedded Files Detection ---

var (
	// SVG elements that can contain scripts
	svgScriptPattern        = regexp.MustCompile(`(?i)<\s*svg[\s>].*<\s*script`)
	svgForeignObjectPattern = regexp.MustCompile(`(?i)<\s*foreignObject[\s>]`)

	// Data URI in image references (with executable MIME types)
	executableDataURIPattern = regexp.MustCompile(`(?i)data\s*:\s*(?:text/html|application/javascript|application/x-javascript|text/javascript|image/svg\+xml)\s*[;,]`)
)

func scanEmbeddedFiles(content string) []SecurityFinding {
	var findings []SecurityFinding
	lines := strings.Split(content, "\n")

	// Track code blocks
	inCodeBlock := false
	codeBlockDelimiter := ""

	for lineNum, line := range lines {
		lineNo := lineNum + 1
		trimmed, isFence := markdownCodeFence(line)

		if isFence {
			if !inCodeBlock {
				inCodeBlock = true
				codeBlockDelimiter = trimmed[:3]
			} else if isClosingCodeFence(trimmed, codeBlockDelimiter) {
				inCodeBlock = false
				codeBlockDelimiter = ""
			}
			continue
		}

		if inCodeBlock {
			continue
		}

		// Check for SVG with script content
		if svgForeignObjectPattern.MatchString(line) {
			findings = append(findings, SecurityFinding{
				Category:    CategoryEmbeddedFiles,
				Description: "SVG <foreignObject> element can embed arbitrary HTML/scripts",
				Line:        lineNo,
				Snippet:     stringutil.Truncate(strings.TrimSpace(line), 80),
			})
		}

		// Check for executable data URIs
		if executableDataURIPattern.MatchString(line) {
			findings = append(findings, SecurityFinding{
				Category:    CategoryEmbeddedFiles,
				Description: "data URI with executable MIME type (text/html, application/javascript, image/svg+xml)",
				Line:        lineNo,
				Snippet:     stringutil.Truncate(strings.TrimSpace(line), 80),
			})
		}
	}

	// Multi-line SVG script check
	if svgScriptPattern.MatchString(content) {
		findings = append(findings, SecurityFinding{
			Category:    CategoryEmbeddedFiles,
			Description: "SVG element contains embedded <script> tag",
			Line:        0,
			Snippet:     "",
		})
	}

	return findings
}

// --- Social Engineering Detection ---

var (
	// Patterns that suggest prompt injection via hidden instructions
	promptInjectionPatterns = regexp.MustCompile(`(?i)(?:ignore\s+(?:previous|above|all)\s+instructions|override\s+instructions|new\s+instructions|you\s+are\s+now|disregard\s+(?:previous|above|all)|forget\s+(?:previous|above|all)|system\s*:\s*you\s+are)`)

	// Base64 encoded payloads (very long base64 strings; threshold tuned to reduce false positives)
	base64PayloadPattern = regexp.MustCompile(`[A-Za-z0-9+/]{200,}={0,2}`)

	// Shell pipe-to-execute patterns
	pipeToShellPattern = regexp.MustCompile(`(?i)(?:curl|wget)\s+[^\n|]*\|\s*(?:sh|bash|zsh|python|node|perl|ruby)`)

	// Base64 decode and execute
	base64ExecPattern = regexp.MustCompile(`(?i)(?:base64\s+(?:-d|--decode)|atob)\s*.*\|\s*(?:sh|bash|eval)`)

	// Hex-encoded strings (potential obfuscation)
	longHexPattern = regexp.MustCompile(`(?:0x[0-9a-fA-F]{2}[\s,]*){20,}|\\x[0-9a-fA-F]{2}(?:\\x[0-9a-fA-F]{2}){19,}`)
)

func scanSocialEngineering(content string) []SecurityFinding {
	var findings []SecurityFinding
	lines := strings.Split(content, "\n")

	// Track code blocks
	inCodeBlock := false
	codeBlockDelimiter := ""

	for lineNum, line := range lines {
		lineNo := lineNum + 1
		trimmed, isFence := markdownCodeFence(line)

		if isFence {
			if !inCodeBlock {
				inCodeBlock = true
				codeBlockDelimiter = trimmed[:3]
			} else if isClosingCodeFence(trimmed, codeBlockDelimiter) {
				inCodeBlock = false
				codeBlockDelimiter = ""
			}
			continue
		}

		// Check all lines (including inside code blocks for some patterns)

		// Prompt injection patterns (check everywhere, including code blocks)
		if promptInjectionPatterns.MatchString(line) {
			findings = append(findings, SecurityFinding{
				Category:    CategorySocialEngineering,
				Description: "contains prompt injection pattern (attempts to override AI agent instructions)",
				Line:        lineNo,
				Snippet:     stringutil.Truncate(strings.TrimSpace(line), 80),
			})
		}

		// Skip code blocks for remaining checks (they may legitimately contain shell code)
		if inCodeBlock {
			continue
		}

		findings = append(findings, scanNonCodeSocialEngineering(line, lineNo)...)
	}

	return findings
}

func scanNonCodeSocialEngineering(line string, lineNo int) []SecurityFinding {
	var findings []SecurityFinding
	addFinding := func(description string, snippetLength int) {
		findings = append(findings, SecurityFinding{
			Category:    CategorySocialEngineering,
			Description: description,
			Line:        lineNo,
			Snippet:     stringutil.Truncate(strings.TrimSpace(line), snippetLength),
		})
	}

	if base64PayloadPattern.MatchString(line) {
		addFinding("contains large base64-encoded payload that may hide malicious content", 60)
	}
	if pipeToShellPattern.MatchString(line) {
		addFinding("contains pipe-to-shell pattern (curl/wget piped to sh/bash) outside code block", 80)
	}
	if base64ExecPattern.MatchString(line) {
		addFinding("contains base64 decode-and-execute pattern", 80)
	}
	if longHexPattern.MatchString(line) {
		addFinding("contains long hex-encoded string that may be obfuscating a payload", 60)
	}

	return findings
}

// --- Helpers ---

func markdownCodeFence(line string) (string, bool) {
	line = strings.TrimSuffix(line, "\r")
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || (indent < len(line) && line[indent] == '\t') {
		return "", false
	}

	trimmed := strings.TrimSpace(line[indent:])
	return trimmed, strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// isClosingCodeFence checks if a trimmed line is a valid closing code fence.
// In CommonMark, a closing fence must consist only of the fence characters
// (backticks or tildes) with no info string. Lines like "```bash" are opening
// fences, not closing fences.
func isClosingCodeFence(trimmed, codeBlockDelimiter string) bool {
	if !strings.HasPrefix(trimmed, codeBlockDelimiter) {
		return false
	}
	// Strip all fence characters, then check only whitespace remains (no info string)
	fenceChar := codeBlockDelimiter[0]
	stripped := strings.TrimLeft(trimmed, string(fenceChar))
	return strings.TrimSpace(stripped) == ""
}

// lineNumberAt returns the 1-based line number for byte offset pos in content
func lineNumberAt(content string, pos int) int {
	if pos < 0 || pos >= len(content) {
		return 0
	}
	return strings.Count(content[:pos], "\n") + 1
}
