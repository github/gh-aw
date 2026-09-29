package cli

import (
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
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
	indent int
	key    string
}

func normalizeSandboxFieldLines(lines []string) ([]string, bool) {
	result := make([]string, 0, len(lines))
	stack := make([]sandboxYAMLPathEntry, 0)
	modified := false

	for _, line := range lines {
		key, ok := frontmatterMappingKey(line)
		if !ok {
			result = append(result, line)
			continue
		}

		indent := len(getIndentation(line))
		for {
			last, ok := lastSandboxYAMLPathEntry(stack)
			if !ok || last.indent < indent {
				break
			}
			stack = stack[:len(stack)-1]
		}

		parent := make([]string, 0, len(stack))
		for _, entry := range stack {
			parent = append(parent, entry.key)
		}
		if replacement := sandboxFieldReplacement(parent, key); replacement != "" {
			line, _ = findAndReplaceInLine(line, key, replacement)
			key = replacement
			modified = true
		}
		result = append(result, line)
		stack = append(stack, sandboxYAMLPathEntry{indent: indent, key: key})
	}

	return result, modified
}

func lastSandboxYAMLPathEntry(stack []sandboxYAMLPathEntry) (sandboxYAMLPathEntry, bool) {
	var last sandboxYAMLPathEntry
	found := false
	for _, entry := range stack {
		last = entry
		found = true
	}
	return last, found
}

func frontmatterMappingKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
		return "", false
	}
	key, _, ok := strings.Cut(trimmed, ":")
	if !ok || key == "" || strings.ContainsAny(key, " \t{}[]") {
		return "", false
	}
	return key, true
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
