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

			var transformErr error
			updated, applied, err := applyFrontmatterLineTransform(content, func(lines []string) ([]string, bool) {
				result, modified, parseErr := normalizeSandboxFieldLines(lines)
				transformErr = parseErr
				return result, modified
			})
			if transformErr != nil {
				return content, false, transformErr
			}
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

type sandboxKeyEdit struct {
	column int
	oldKey string
	newKey string
}

func normalizeSandboxFieldLines(lines []string) ([]string, bool, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &document); err != nil {
		return lines, false, err
	}
	edits := make(map[int][]sandboxKeyEdit)
	for _, root := range document.Content {
		if err := collectSandboxKeyEdits(root, nil, lines, edits); err != nil {
			return lines, false, err
		}
	}

	result := append([]string(nil), lines...)
	for lineIndex, line := range result {
		lineEdits, ok := edits[lineIndex]
		if !ok {
			continue
		}
		slices.SortFunc(lineEdits, func(a, b sandboxKeyEdit) int { return b.column - a.column })
		for _, edit := range lineEdits {
			runes := []rune(line)
			if edit.column > len(runes) {
				return lines, false, fmt.Errorf("cannot locate sandbox field %q on line %d", edit.oldKey, lineIndex+1)
			}
			offset := len(string(runes[:edit.column]))
			if strings.HasPrefix(line[offset:], "'") || strings.HasPrefix(line[offset:], `"`) {
				offset++
			}
			if !strings.HasPrefix(line[offset:], edit.oldKey) {
				return lines, false, fmt.Errorf("cannot locate sandbox field %q on line %d", edit.oldKey, lineIndex+1)
			}
			line = line[:offset] + edit.newKey + line[offset+len(edit.oldKey):]
		}
		result[lineIndex] = line
	}
	return result, len(edits) > 0, nil
}

func collectSandboxKeyEdits(node *yaml.Node, path []string, lines []string, edits map[int][]sandboxKeyEdit) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	var key *yaml.Node
	for i, value := range node.Content {
		if i%2 == 0 {
			key = value
			continue
		}
		replacement := sandboxFieldReplacement(path, key.Value)
		if replacement != "" {
			for j, sibling := range node.Content {
				if j%2 == 0 && sibling.Value == replacement {
					return fmt.Errorf("cannot migrate sandbox field %s: %q and %q are both present; remove one before running gh aw fix", strings.Join(append(path, key.Value), "."), key.Value, replacement)
				}
			}
			if key.Line < 1 || key.Line > len(lines) || key.Column < 1 {
				return fmt.Errorf("cannot locate sandbox field %q in frontmatter", key.Value)
			}
			edits[key.Line-1] = append(edits[key.Line-1], sandboxKeyEdit{column: key.Column - 1, oldKey: key.Value, newKey: replacement})
		}
		if err := collectSandboxKeyEdits(value, append(path, key.Value), lines, edits); err != nil {
			return err
		}
	}
	return nil
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
