package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// wireCreateTokenJobs runs after step insertion so token references stay local to
// jobs that actually mint them, and only compiler-owned consumers after the mint are rewritten.
func (c *Compiler) wireCreateTokenJobs(data *WorkflowData) error {
	for _, job := range c.jobManager.GetAllJobs() {
		content := strings.Join(job.Steps, "")
		if !strings.Contains(content, "create-token") {
			continue
		}
		var document yaml.Node
		if err := yaml.Unmarshal([]byte(content), &document); err != nil {
			return fmt.Errorf("cannot wire create-token in job %q: %w", job.Name, err)
		}
		if len(document.Content) != 1 {
			continue
		}
		root := document.Content[0] //nolint:uncheckedsliceindex // Exactly one document is required above.
		if root.Kind != yaml.SequenceNode {
			continue
		}
		lines := strings.SplitAfter(content, "\n")
		minted := false
		for _, step := range root.Content {
			if isCreateTokenMintStep(step) {
				minted = true
				continue
			}
			if minted {
				wireCreateTokenEnv(job.Name, step, lines, data)
			}
		}
		if !minted {
			continue
		}
		if shouldGrantCreateTokenOIDC(job, data) {
			perms := NewPermissionsParser(job.Permissions).ToPermissions()
			if level, exists := perms.Get(PermissionIdToken); !exists || level != PermissionWrite {
				perms.Set(PermissionIdToken, PermissionWrite)
				job.Permissions = perms.RenderToYAML()
			}
		}
		// Keep the original fragment boundaries used by job validation.
		line := 0
		for i, fragment := range job.Steps {
			count := strings.Count(fragment, "\n")
			if !strings.HasSuffix(fragment, "\n") && fragment != "" {
				count++
			}
			job.Steps[i] = strings.Join(lines[line:line+count], "")
			line += count
		}
	}
	return nil
}

func isCreateTokenMintStep(step *yaml.Node) bool {
	id := artifactMappingValue(step, "id")
	uses := artifactMappingValue(step, "uses")
	if id == nil || id.Value != "create-token" || uses == nil {
		return false
	}
	return strings.HasPrefix(uses.Value, "actions/create-github-app-token@")
}

func shouldGrantCreateTokenOIDC(job *Job, data *WorkflowData) bool {
	jobPermissions := NewPermissionsParser(job.Permissions).ToPermissions()
	if level, exists := jobPermissions.Get(PermissionIdToken); exists && level == PermissionNone {
		return false
	}
	if data == nil {
		return true
	}
	if data.SafeOutputs != nil && data.SafeOutputs.IDToken != nil && *data.SafeOutputs.IDToken == string(PermissionNone) {
		return false
	}
	workflowPermissions := NewPermissionsParser(data.Permissions).ToPermissions()
	if level, exists := workflowPermissions.Get(PermissionIdToken); exists && level == PermissionNone {
		return false
	}
	return true
}

func wireCreateTokenEnv(jobName string, step *yaml.Node, lines []string, data *WorkflowData) {
	if isCreateTokenCheckoutManifestStep(step) {
		wireCreateTokenConsumerEnv(jobName, artifactMappingValue(step, "env"), lines, data)
		return
	}
	id := artifactMappingValue(step, "id")
	if id == nil {
		return
	}
	switch jobName {
	case "activation", "agent", "safe_outputs", "conclusion":
	default:
		return
	}
	switch id.Value {
	case "start-mcp-gateway", "build-checkout-manifest":
		wireCreateTokenConsumerEnv(jobName, artifactMappingValue(step, "env"), lines, data)
	case "redact_secrets":
		addCreateTokenToSecretRedaction(artifactMappingValue(step, "env"), lines)
	}
}

func isCreateTokenCheckoutManifestStep(step *yaml.Node) bool {
	name := artifactMappingValue(step, "name")
	uses := artifactMappingValue(step, "uses")
	return name != nil && name.Value == "Build checkout manifest for safe-outputs handlers" &&
		uses != nil && strings.HasPrefix(uses.Value, "actions/github-script@")
}

func wireCreateTokenConsumerEnv(jobName string, env *yaml.Node, lines []string, data *WorkflowData) {
	if env == nil || env.Kind != yaml.MappingNode || env.Style&yaml.FlowStyle != 0 {
		return
	}
	for i := 0; i+1 < len(env.Content); i += 2 {
		key, value := env.Content[i], env.Content[i+1] //nolint:uncheckedsliceindex // The mapping value index is checked by the loop condition.
		switch key.Value {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_AW_GITHUB_TOKEN", "GH_AW_GITHUB_MCP_SERVER_TOKEN", "GITHUB_MCP_SERVER_TOKEN":
		default:
			continue
		}
		if value.Kind != yaml.ScalarNode || key.Line != value.Line || value.Style == yaml.LiteralStyle || value.Style == yaml.FoldedStyle ||
			!isDefaultCreateTokenFallback(value.Value) || isExplicitCreateTokenBinding(jobName, data, key.Value, value.Value) {
			continue
		}
		line := key.Line - 1
		if line < 0 || line >= len(lines) {
			continue
		}
		lines[line] = strings.Replace(lines[line], value.Value,
			combineTokenExpressions("${{ steps.create-token.outputs.token }}", value.Value), 1)
	}
}

func isExplicitCreateTokenBinding(jobName string, data *WorkflowData, key, value string) bool {
	if data == nil {
		return false
	}
	if jobName == "agent" {
		if override, exists := getEngineEnvOverrides(data)[key]; exists && override != "" {
			return true
		}
	}
	if jobName == "agent" && key == "GITHUB_MCP_SERVER_TOKEN" {
		if githubTool, ok := data.Tools["github"].(map[string]any); ok {
			if token, ok := githubTool["github-token"].(string); ok && token != "" {
				return true
			}
		}
	}
	return jobName == "safe_outputs" && containsExplicitGitHubToken(data.RawFrontmatter["safe-outputs"], value)
}

func containsExplicitGitHubToken(value any, token string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "github-token" {
				if configured, ok := child.(string); ok && configured == token {
					return true
				}
			}
			if containsExplicitGitHubToken(child, token) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsExplicitGitHubToken(child, token) {
				return true
			}
		}
	}
	return false
}

func addCreateTokenToSecretRedaction(env *yaml.Node, lines []string) {
	if env == nil || env.Kind != yaml.MappingNode || env.Style&yaml.FlowStyle != 0 {
		return
	}
	names := artifactMappingValue(env, "GH_AW_SECRET_NAMES")
	if names == nil || names.Kind != yaml.ScalarNode || names.Value == "" ||
		names.Style == yaml.LiteralStyle || names.Style == yaml.FoldedStyle {
		return
	}
	for i := 0; i+1 < len(env.Content); i += 2 {
		key, value := env.Content[i], env.Content[i+1] //nolint:uncheckedsliceindex // The mapping value index is checked by the loop condition.
		secretName, isSecret := strings.CutPrefix(key.Value, "SECRET_")
		if isSecret && value.Value == "${{ steps.create-token.outputs.token }}" &&
			createTokenSecretNameInUse(nil, names.Value, secretName) {
			return
		}
	}
	namesLine := names.Line - 1
	if namesLine < 0 || namesLine >= len(lines) {
		return
	}
	const baseName = "GH_AW_GITHUB_TOKEN_MINTED"
	name := baseName
	for suffix := 1; createTokenSecretNameInUse(env, names.Value, name); suffix++ {
		name = fmt.Sprintf("%s_%d", baseName, suffix)
	}
	lines[namesLine] = strings.Replace(lines[namesLine], names.Value, names.Value+","+name, 1)
	indent := strings.Repeat(" ", len(lines[namesLine])-len(strings.TrimLeft(lines[namesLine], " \t")))
	lines[namesLine] = fmt.Sprintf("%s%sSECRET_%s: ${{ steps.create-token.outputs.token }}\n", lines[namesLine], indent, name)
}

func createTokenSecretNameInUse(env *yaml.Node, names, name string) bool {
	for registeredName := range strings.SplitSeq(names, ",") {
		if strings.TrimSpace(registeredName) == name {
			return true
		}
	}
	return env != nil && artifactMappingValue(env, "SECRET_"+name) != nil
}

func isDefaultCreateTokenFallback(value string) bool {
	if !strings.HasPrefix(value, "${{") || !strings.HasSuffix(value, "}}") {
		return false
	}
	hasGitHubAppTokenSecret := false
	for part := range strings.SplitSeq(stripExpressionWrapper(value), "||") {
		switch strings.TrimSpace(part) {
		case "secrets.GH_AW_GITHUB_TOKEN", "secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN":
			hasGitHubAppTokenSecret = true
		case "secrets.GITHUB_TOKEN":
		default:
			return false
		}
	}
	return hasGitHubAppTokenSecret
}
