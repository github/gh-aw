package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"gopkg.in/yaml.v3"
)

var sandboxFieldsCodemodLog = logger.New("cli:codemod_sandbox_fields")

type sandboxFieldRename struct {
	parent []string
	oldKey string
	newKey string
}

var sandboxFieldRenames = []sandboxFieldRename{
	{parent: []string{"sandbox", "agent", "images"}, oldKey: "apiProxy", newKey: "api-proxy"},
	{parent: []string{"sandbox", "agent", "images"}, oldKey: "cliProxy", newKey: "cli-proxy"},
	{parent: []string{"sandbox", "agent", "images"}, oldKey: "buildTools", newKey: "build-tools"},
	{parent: []string{"sandbox", "agent", "images"}, oldKey: "dohProxy", newKey: "doh-proxy"},
	{parent: []string{"sandbox", "agent", "images"}, oldKey: "enclaveScript", newKey: "enclave-script"},
	{parent: []string{"sandbox", "agent", "images"}, oldKey: "enclaveAgent", newKey: "enclave-agent"},
	{parent: []string{"sandbox", "agent", "images"}, oldKey: "enclaveMcpServer", newKey: "enclave-mcp-server"},
	{parent: []string{"sandbox", "agent", "images"}, oldKey: "dindStaging", newKey: "dind-staging"},
	{parent: []string{"sandbox", "agent", "config", "filesystem"}, oldKey: "denyRead", newKey: "deny-read"},
	{parent: []string{"sandbox", "agent", "config", "filesystem"}, oldKey: "allowWrite", newKey: "allow-write"},
	{parent: []string{"sandbox", "agent", "config", "filesystem"}, oldKey: "denyWrite", newKey: "deny-write"},
	{parent: []string{"sandbox", "agent", "config"}, oldKey: "ignoreViolations", newKey: "ignore-violations"},
	{parent: []string{"sandbox", "agent", "config"}, oldKey: "enableWeakerNestedSandbox", newKey: "enable-weaker-nested-sandbox"},
	{parent: []string{"sandbox", "agent", "config", "network"}, oldKey: "allowedDomains", newKey: "allowed-domains"},
	{parent: []string{"sandbox", "agent", "config", "network"}, oldKey: "blockedDomains", newKey: "blocked-domains"},
	{parent: []string{"sandbox", "agent", "config", "network"}, oldKey: "allowUnixSockets", newKey: "allow-unix-sockets"},
	{parent: []string{"sandbox", "agent", "config", "network"}, oldKey: "allowLocalBinding", newKey: "allow-local-binding"},
	{parent: []string{"sandbox", "agent", "config", "network"}, oldKey: "allowAllUnixSockets", newKey: "allow-all-unix-sockets"},
	{parent: []string{"sandbox", "agent", "targets", "*"}, oldKey: "authHeader", newKey: "auth-header"},
	{parent: []string{"sandbox", "agent", "targets", "*"}, oldKey: "extraHeaders", newKey: "extra-headers"},
	{parent: []string{"sandbox", "agent", "targets", "*"}, oldKey: "extraBodyFields", newKey: "extra-body-fields"},
	{parent: []string{"sandbox", "agent", "targets", "*"}, oldKey: "sessionId", newKey: "session-id"},
	{parent: []string{"sandbox", "config", "filesystem"}, oldKey: "denyRead", newKey: "deny-read"},
	{parent: []string{"sandbox", "config", "filesystem"}, oldKey: "allowWrite", newKey: "allow-write"},
	{parent: []string{"sandbox", "config", "filesystem"}, oldKey: "denyWrite", newKey: "deny-write"},
	{parent: []string{"sandbox", "config"}, oldKey: "ignoreViolations", newKey: "ignore-violations"},
	{parent: []string{"sandbox", "config"}, oldKey: "enableWeakerNestedSandbox", newKey: "enable-weaker-nested-sandbox"},
	{parent: []string{"sandbox", "config", "network"}, oldKey: "allowedDomains", newKey: "allowed-domains"},
	{parent: []string{"sandbox", "config", "network"}, oldKey: "blockedDomains", newKey: "blocked-domains"},
	{parent: []string{"sandbox", "config", "network"}, oldKey: "allowUnixSockets", newKey: "allow-unix-sockets"},
	{parent: []string{"sandbox", "config", "network"}, oldKey: "allowLocalBinding", newKey: "allow-local-binding"},
	{parent: []string{"sandbox", "config", "network"}, oldKey: "allowAllUnixSockets", newKey: "allow-all-unix-sockets"},
	{parent: []string{"sandbox", "mcp"}, oldKey: "entrypointArgs", newKey: "entrypoint-args"},
}

func getSandboxFieldsCodemod() Codemod {
	return Codemod{
		ID:           "sandbox-fields-kebab-case",
		Name:         "Normalize sandbox fields to kebab-case",
		Description:  "Renames camel-cased sandbox frontmatter fields to their kebab-case equivalents",
		IntroducedIn: "0.86.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			if !hasCamelizedSandboxField(frontmatter) {
				return content, false, nil
			}

			lines, _, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}
			if _, _, err := normalizeSandboxFieldLinesWithError(lines); err != nil {
				return content, false, err
			}

			updated, applied, err := applyFrontmatterLineTransform(content, normalizeSandboxFieldLines)
			if applied {
				sandboxFieldsCodemodLog.Print("Normalized camel-cased sandbox frontmatter fields")
			}
			return updated, applied, err
		},
	}
}

func hasCamelizedSandboxField(frontmatter map[string]any) bool {
	var visit func(map[string]any, []string) bool
	visit = func(current map[string]any, parent []string) bool {
		for key, value := range current {
			if sandboxFieldReplacement(parent, key) != "" {
				return true
			}
			child, ok := value.(map[string]any)
			if ok && visit(child, append(parent, key)) {
				return true
			}
		}
		return false
	}
	return visit(frontmatter, nil)
}

type sandboxYAMLPathEntry struct {
	line   int
	column int
	oldKey string
	newKey string
}

func normalizeSandboxFieldLines(lines []string) ([]string, bool) {
	result, modified, err := normalizeSandboxFieldLinesWithError(lines)
	if err != nil {
		return lines, false
	}
	return result, modified
}

func normalizeSandboxFieldLinesWithError(lines []string) ([]string, bool, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &document); err != nil {
		return lines, false, fmt.Errorf("failed to parse sandbox frontmatter for field migration: %w", err)
	}

	replacements := make([]sandboxYAMLPathEntry, 0)
	if err := collectSandboxFieldReplacements(&document, nil, &replacements); err != nil {
		return lines, false, err
	}
	if len(replacements) == 0 {
		return lines, false, nil
	}

	result := slices.Clone(lines)
	replacementsByLine := make(map[int][]sandboxYAMLPathEntry)
	for _, replacement := range replacements {
		lineIndex := replacement.line
		if lineIndex < 0 || lineIndex >= len(result) {
			return lines, false, fmt.Errorf("cannot locate sandbox field %q in frontmatter", replacement.oldKey)
		}
		replacementsByLine[lineIndex] = append(replacementsByLine[lineIndex], replacement)
	}
	for lineIndex, line := range result {
		lineReplacements := replacementsByLine[lineIndex]
		if len(lineReplacements) == 0 {
			continue
		}
		slices.SortFunc(lineReplacements, func(a, b sandboxYAMLPathEntry) int {
			return b.column - a.column
		})
		for _, replacement := range lineReplacements {
			var err error
			line, err = replaceSandboxYAMLKey(line, replacement.column, replacement.oldKey, replacement.newKey)
			if err != nil {
				return lines, false, err
			}
		}
		result[lineIndex] = line
	}
	return result, true, nil
}

func collectSandboxFieldReplacements(node *yaml.Node, path []string, replacements *[]sandboxYAMLPathEntry) error {
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			if err := collectSandboxFieldReplacements(child, path, replacements); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		return collectSandboxMappingReplacements(node, path, replacements)
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if err := collectSandboxFieldReplacements(child, append(slices.Clone(path), "*"), replacements); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectSandboxMappingReplacements(node *yaml.Node, path []string, replacements *[]sandboxYAMLPathEntry) error {
	keys := make(map[string]struct{}, len(node.Content)/2)
	for i, key := range node.Content {
		if i%2 != 0 || key.Kind != yaml.ScalarNode {
			continue
		}
		keys[key.Value] = struct{}{}
	}
	var key *yaml.Node
	for _, child := range node.Content {
		if key == nil {
			key = child
			continue
		}
		value := child
		if key.Kind != yaml.ScalarNode {
			key = nil
			continue
		}
		replacement := sandboxFieldReplacement(path, key.Value)
		if replacement != "" {
			if _, exists := keys[replacement]; exists {
				return fmt.Errorf(
					"cannot migrate sandbox field %q to %q at %s: both spellings are present; resolve the duplicate values manually and rerun gh aw fix",
					key.Value,
					replacement,
					strings.Join(append(slices.Clone(path), key.Value), "."),
				)
			}
			*replacements = append(*replacements, sandboxYAMLPathEntry{
				line:   key.Line - 1,
				column: key.Column - 1,
				oldKey: key.Value,
				newKey: replacement,
			})
		}
		childKey := key.Value
		if replacement != "" {
			childKey = replacement
		}
		if err := collectSandboxFieldReplacements(value, append(slices.Clone(path), childKey), replacements); err != nil {
			return err
		}
		key = nil
	}
	return nil
}

func replaceSandboxYAMLKey(line string, column int, oldKey, newKey string) (string, error) {
	if column < 0 || column >= len(line) {
		return line, fmt.Errorf("cannot locate sandbox field %q in frontmatter", oldKey)
	}
	token := line[column:]
	for _, quote := range []string{`"`, `'`} {
		quotedKey := quote + oldKey + quote
		if strings.HasPrefix(token, quotedKey) {
			return line[:column] + quote + newKey + quote + token[len(quotedKey):], nil
		}
	}
	key, suffix, ok := strings.Cut(token, ":")
	if !ok {
		return line, fmt.Errorf("cannot locate sandbox field %q in frontmatter", oldKey)
	}
	plainKey := strings.TrimRight(key, " \t")
	if plainKey != oldKey {
		return line, fmt.Errorf("cannot locate sandbox field %q in frontmatter", oldKey)
	}
	return line[:column] + newKey + key[len(plainKey):] + ":" + suffix, nil
}

func sandboxFieldReplacement(parent []string, key string) string {
	for _, rename := range sandboxFieldRenames {
		if key == rename.oldKey && sandboxFieldPathMatches(parent, rename.parent) {
			return rename.newKey
		}
	}
	return ""
}

func sandboxFieldPathMatches(path, pattern []string) bool {
	return slices.EqualFunc(path, pattern, func(pathElement, patternElement string) bool {
		return patternElement == "*" || pathElement == patternElement
	})
}
