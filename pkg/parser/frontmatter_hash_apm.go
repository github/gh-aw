package parser

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var apmManifestPathTextPattern = regexp.MustCompile(`(?m)^(?:apm|"apm"|'apm'):[ \t]*(?:"([A-Za-z0-9_./-]+)"|'([A-Za-z0-9_./-]+)'|([A-Za-z0-9_./-]+))[ \t]*(?:#.*)?$`)

// ExtractAPMManifestPathText matches the compiler's supported scalar spelling.
// It is shared with the text-based JavaScript stale-check hash contract.
func ExtractAPMManifestPathText(frontmatter string) string {
	matches := apmManifestPathTextPattern.FindStringSubmatch(strings.ReplaceAll(frontmatter, "\r\n", "\n"))
	if len(matches) == 0 {
		return ""
	}
	for _, value := range matches[1:] {
		if value != "" {
			return value
		}
	}
	return ""
}

func readAPMHashDocuments(frontmatter, baseDir string, reader FileReader) ([]string, error) {
	manifest := ExtractAPMManifestPathText(frontmatter)
	if manifest == "" {
		return nil, nil
	}

	path := filepath.Join(baseDir, manifest)
	paths := []string{path, filepath.Join(filepath.Dir(path), "apm.lock.yaml")}
	documents := make([]string, 0, len(paths))
	for _, path := range paths {
		content, err := reader(path)
		if err != nil {
			return nil, fmt.Errorf("cannot hash APM document %s: %w", path, err)
		}
		documents = append(documents, normalizeFrontmatterText(string(content)))
	}
	return documents, nil
}

func addAPMHashDocuments(canonical map[string]any, frontmatter, normalizedFrontmatter string, imports []string, baseDir string, reader FileReader) error {
	documents, err := readAPMHashDocuments(frontmatter, baseDir, reader)
	if err != nil {
		return err
	}
	inputs := append(append([]string(nil), imports...), documents...)
	if err := validateFrontmatterHashInputSize(normalizedFrontmatter, inputs); err != nil {
		return err
	}
	if len(documents) > 0 {
		canonical["apm-documents"] = documents
	}
	return nil
}
