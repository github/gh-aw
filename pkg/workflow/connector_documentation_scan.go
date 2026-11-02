package workflow

import (
	"encoding/xml"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
)

func scanImportedConnectorMarkdown(content, path string) []SecurityFinding {
	findings := ScanMarkdownSecurity(content)
	canonical := "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")
	if !strings.Contains(canonical, "/.github/aw/connectors/") || !strings.HasSuffix(canonical, ".md") {
		return findings
	}
	document, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return findings
	}
	declarations, ok := document.Frontmatter["connectors"].(map[string]any)
	if !ok {
		return findings
	}
	approved := map[int]struct{}{}
	for _, match := range htmlCommentPattern.FindAllStringSubmatchIndex(content, -1) {
		if len(match) < 4 {
			continue
		}
		bounds := [4]int(match)
		body := content[bounds[2]:bounds[3]]
		if validConnectorDocumentation(body, declarations) {
			approved[lineNumberAt(content, bounds[0])] = struct{}{}
		}
	}
	filtered := findings[:0]
	for _, finding := range findings {
		_, isApproved := approved[finding.Line]
		if finding.Category == CategoryHiddenContent &&
			finding.Description == "HTML comment contains suspicious content (code, URLs, or executable instructions)" &&
			isApproved {
			continue
		}
		filtered = append(filtered, finding)
	}
	return filtered
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
