package parser

import (
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strings"
)

var connectorDocumentationPattern = regexp.MustCompile(`(?s)<!--.*?(?:-->|$)|<connector-documentation(?:\s[^>]*)?>.*?</connector-documentation>`)

// RemoveConnectorDocumentation omits well-formed authoring documentation that
// matches a declared connector. Call before input substitution, not afterwards:
// an input must not be able to close a documentation comment or its CDATA.
// Newlines are retained so source positions and line selections stay stable.
// Standalone XML and legacy comments are supported. Invalid documentation and
// unrelated comments remain subject to security scans.
func RemoveConnectorDocumentation(content string) string {
	if !strings.Contains(content, "connector-documentation") {
		return content
	}
	document, err := ExtractFrontmatterFromContent(content)
	if err != nil {
		return content
	}
	declarations, ok := document.Frontmatter["connectors"].(map[string]any)
	if !ok {
		return content
	}
	firstNewline, _ := splitFirstLine(content)
	_, markdownStart, err := findFrontmatterDelimiters(content, computeFrontmatterSearchStart(content, firstNewline))
	if err != nil {
		return content
	}
	body := connectorDocumentationPattern.ReplaceAllStringFunc(content[markdownStart:], func(fragment string) string {
		document := fragment
		if strings.HasPrefix(fragment, "<!--") {
			if !strings.HasSuffix(fragment, "-->") {
				return fragment
			}
			document = fragment[4 : len(fragment)-3]
		}
		if !validConnectorDocumentation(document, declarations) {
			return fragment
		}
		return strings.Repeat("\n", strings.Count(fragment, "\n"))
	})
	return content[:markdownStart] + body
}

func validConnectorDocumentation(body string, declarations map[string]any) bool {
	if strings.Contains(body, "--") {
		return false
	}
	decoder := xml.NewDecoder(strings.NewReader(strings.TrimSpace(body)))
	root, depth := false, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return root && depth == 0
		}
		if err != nil {
			return false
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if root || value.Name.Space != "" || value.Name.Local != "connector-documentation" {
					return false
				}
				var id, version string
				seen := map[string]struct{}{}
				for _, attribute := range value.Attr {
					_, duplicate := seen[attribute.Name.Local]
					if attribute.Name.Space != "" || duplicate {
						return false
					}
					seen[attribute.Name.Local] = struct{}{}
					switch attribute.Name.Local {
					case "id":
						id = attribute.Value
					case "version":
						version = attribute.Value
					default:
						return false
					}
				}
				config, exists := declarations[id].(map[string]any)
				if !exists || config["endpoints"] == nil || config["credentials"] == nil || version != "1" {
					return false
				}
				root = true
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.Directive, xml.ProcInst:
			return false
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(value)) != "" {
				return false
			}
		}
	}
}
