package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"gopkg.in/yaml.v3"
)

type permissionCommentSource struct {
	key, value *yaml.Node
	inherited  *permissionCommentSource
}

func permissionCommentEntry(node *yaml.Node, name string) permissionCommentSource {
	if node == nil {
		return permissionCommentSource{}
	}
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if node == nil || node.Kind != yaml.MappingNode {
		return permissionCommentSource{}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name { //nolint:uncheckedsliceindex // The loop bounds both members of this mapping entry.
			return permissionCommentSource{key: node.Content[i], value: node.Content[i+1]} //nolint:uncheckedsliceindex // The loop bounds both members of this mapping entry.
		}
	}
	return permissionCommentSource{}
}

func permissionCommentSources(frontmatter string) (map[string]permissionCommentSource, error) {
	result := make(map[string]permissionCommentSource)
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(frontmatter), &document); err != nil {
		return nil, fmt.Errorf("parsing permission source comments: %w", err)
	}
	if len(document.Content) == 0 {
		return result, nil
	}
	root := document.Content[0] //nolint:uncheckedsliceindex // The empty-document guard above ensures a root exists.
	// Workflow permissions are lowered to the agent job, not the workflow token.
	result[string(constants.AgentJobName)] = permissionCommentEntry(root, "permissions")
	on := permissionCommentEntry(root, "on")
	result[string(constants.PreActivationJobName)] = permissionCommentEntry(on.value, "permissions")
	for parentIndex, parent := range []*yaml.Node{root, permissionCommentEntry(root, "safe-outputs").value} {
		jobs := permissionCommentEntry(parent, "jobs").value
		if jobs == nil || jobs.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			source := permissionCommentEntry(jobs.Content[i+1], "permissions") //nolint:uncheckedsliceindex // The loop bounds both members of this mapping entry.
			if source.key != nil {
				id := jobs.Content[i].Value //nolint:uncheckedsliceindex // The loop bounds both members of this mapping entry.
				if parentIndex == 1 {
					id = stringutil.NormalizeSafeOutputIdentifier(id)
				}
				if inherited, ok := result[id]; ok && inherited.key != nil {
					source.inherited = &inherited
				}
				result[id] = source
			}
		}
	}
	return result, nil
}

func hasPermissionScopeComment(source permissionCommentSource) bool {
	for _, node := range []*yaml.Node{source.key, source.value} {
		if node != nil && strings.Trim(node.HeadComment+node.LineComment+node.FootComment, "# \t\r\n") != "" {
			return true
		}
	}
	return false
}

func permissionScopeReason(scope, level string) string {
	if level == "none" {
		return "Deny access to " + scope + "."
	}
	if scope == "id-token" {
		return "Request an OIDC token for authenticated services."
	}
	if scope == "copilot-requests" {
		return "Authenticate requests to the Copilot engine."
	}
	resources := map[string]string{
		"actions":              "workflow runs and artifacts",
		"attestations":         "artifact attestations",
		"checks":               "check runs",
		"code-quality":         "code quality results",
		"contents":             "repository contents",
		"deployments":          "deployments",
		"discussions":          "discussions and comments",
		"drives":               "persistent drive memory",
		"issues":               "issues and comments",
		"models":               "AI models",
		"packages":             "packages",
		"pages":                "GitHub Pages deployments",
		"pull-requests":        "pull requests and review comments",
		"repository-projects":  "repository projects",
		"security-events":      "code scanning results",
		"statuses":             "commit statuses",
		"vulnerability-alerts": "vulnerability alerts",
	}
	resource := resources[scope]
	if resource == "" {
		resource = scope
	}
	if level == "write" {
		return "Create or update " + resource + "."
	}
	return "Read " + resource + " for workflow context."
}

type permissionCommentEdits struct {
	lines         []string
	before, after map[int][]string
}

func (e *permissionCommentEdits) addComment(target map[int][]string, line, column int, comment string) {
	for text := range strings.SplitSeq(comment, "\n") {
		if strings.TrimSpace(text) != "" {
			target[line-1] = append(target[line-1], strings.Repeat(" ", column-1)+text)
		}
	}
}

func (e *permissionCommentEdits) appendInlineComment(line int, comment string) bool {
	index := line - 1
	if index >= 0 && index < len(e.lines) {
		if !strings.Contains(e.lines[index], " #") {
			e.lines[index] = strings.Join([]string{e.lines[index], comment}, " ")
			return true
		}
	}
	return false
}

func (e *permissionCommentEdits) copyComments(destination, source permissionCommentSource) {
	if destination.key == nil || source.key == nil {
		return
	}
	for _, pair := range [][2]*yaml.Node{{destination.key, source.key}, {destination.value, source.value}} {
		dst, src := pair[0], pair[1]
		e.addComment(e.before, dst.Line, destination.key.Column, src.HeadComment)
		e.addComment(e.after, dst.Line, destination.key.Column, src.FootComment)
		if src.LineComment != "" {
			// Flow mapping comments can share a line; retain each without
			// putting a second inline comment into an existing comment.
			if !e.appendInlineComment(dst.Line, src.LineComment) {
				e.addComment(e.before, dst.Line, destination.key.Column, src.LineComment)
			}
		}
	}
}

func (e *permissionCommentEdits) documentJob(perms, source permissionCommentSource) {
	if perms.key == nil {
		return
	}
	for current := &source; current != nil; current = current.inherited {
		e.copyComments(perms, *current)
	}
	if perms.value.Kind != yaml.MappingNode {
		return
	}
	for j := 0; j+1 < len(perms.value.Content); j += 2 {
		scope := permissionCommentSource{key: perms.value.Content[j], value: perms.value.Content[j+1]} //nolint:uncheckedsliceindex // The loop bounds both members of this mapping entry.
		documented := hasPermissionScopeComment(scope)
		for current := &source; current != nil; current = current.inherited {
			authorScope := permissionCommentEntry(current.value, scope.key.Value)
			e.copyComments(scope, authorScope)
			documented = documented || hasPermissionScopeComment(authorScope)
		}
		if scope.key.Value == "contents" && scope.value.Value == "read" {
			continue
		}
		// A scope-specific reason is still needed when the author only
		// documented the block as a whole.
		if !documented {
			comment := "# " + permissionScopeReason(scope.key.Value, scope.value.Value)
			if !e.appendInlineComment(scope.value.Line, comment) {
				e.addComment(e.before, scope.key.Line, scope.key.Column, comment)
			}
		}
	}
}

func (e *permissionCommentEdits) render() string {
	var result strings.Builder
	for i, line := range e.lines {
		for _, comment := range e.before[i] {
			result.WriteString(comment + "\n")
		}
		result.WriteString(line)
		for _, comment := range e.after[i] {
			result.WriteString("\n" + comment)
		}
		if i < len(e.lines)-1 {
			result.WriteByte('\n')
		}
	}
	return result.String()
}

// documentPermissionComments edits only structural permission lines. In particular,
// text resembling permissions inside a run/script block must remain untouched.
func documentPermissionComments(content, frontmatter string) (string, error) {
	sources, err := permissionCommentSources(frontmatter)
	if err != nil {
		return "", err
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return "", fmt.Errorf("parsing generated permission comments: %w", err)
	}
	if len(document.Content) == 0 {
		return content, nil
	}
	edits := permissionCommentEdits{
		lines: strings.Split(content, "\n"), before: make(map[int][]string), after: make(map[int][]string),
	}
	edits.documentJob(permissionCommentEntry(document.Content[0], "permissions"), sources[string(constants.AgentJobName)]) //nolint:uncheckedsliceindex // The empty-document guard above ensures a root exists.
	jobs := permissionCommentEntry(document.Content[0], "jobs").value                                                      //nolint:uncheckedsliceindex // The empty-document guard above ensures a root exists.
	if jobs != nil && jobs.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			perms := permissionCommentEntry(jobs.Content[i+1], "permissions") //nolint:uncheckedsliceindex // The loop bounds both members of this mapping entry.
			edits.documentJob(perms, sources[jobs.Content[i].Value])          //nolint:uncheckedsliceindex // The loop bounds both members of this mapping entry.
		}
	}
	return edits.render(), nil
}
