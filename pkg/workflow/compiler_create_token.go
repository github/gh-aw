package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// wireCreateTokenJobs runs after step insertion so token references stay local to
// jobs that actually mint them, and only consumers after the mint are rewritten.
func (c *Compiler) wireCreateTokenJobs() error {
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
			if id := artifactMappingValue(step, "id"); id != nil && id.Value == "create-token" {
				minted = true
				continue
			}
			if minted {
				wireCreateTokenEnv(artifactMappingValue(step, "env"), lines)
			}
		}
		if !minted {
			continue
		}
		perms := NewPermissionsParser(job.Permissions).ToPermissions()
		if level, exists := perms.Get(PermissionIdToken); !exists || level != PermissionWrite {
			perms.Set(PermissionIdToken, PermissionWrite)
			job.Permissions = perms.RenderToYAML()
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

func wireCreateTokenEnv(env *yaml.Node, lines []string) {
	if env == nil || env.Kind != yaml.MappingNode || env.Style&yaml.FlowStyle != 0 {
		return
	}
	for i := 0; i+1 < len(env.Content); i += 2 {
		key, value := env.Content[i], env.Content[i+1] //nolint:uncheckedsliceindex // The mapping value index is checked by the loop condition.
		switch key.Value {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_AW_GITHUB_TOKEN", "GH_AW_GITHUB_MCP_SERVER_TOKEN", "SECRET_GH_AW_GITHUB_TOKEN":
		default:
			continue
		}
		if value.Kind != yaml.ScalarNode || key.Line != value.Line || value.Style == yaml.LiteralStyle || value.Style == yaml.FoldedStyle ||
			!isDefaultCreateTokenFallback(value.Value) {
			continue
		}
		names := artifactMappingValue(env, "GH_AW_SECRET_NAMES")
		if key.Value == "SECRET_GH_AW_GITHUB_TOKEN" &&
			(names == nil || names.Kind != yaml.ScalarNode || names.Value == "" || names.Style == yaml.LiteralStyle || names.Style == yaml.FoldedStyle) {
			continue
		}
		line := key.Line - 1
		if line < 0 || line >= len(lines) {
			continue
		}
		lines[line] = strings.Replace(lines[line], value.Value,
			combineTokenExpressions("${{ steps.create-token.outputs.token }}", value.Value), 1)
		if key.Value == "SECRET_GH_AW_GITHUB_TOKEN" && names != nil {
			// Redaction must still receive the original secret even when the minted
			// token wins. Register both values with the existing redaction contract.
			namesLine := names.Line - 1
			if namesLine < 0 || namesLine >= len(lines) {
				continue
			}
			var fallback strings.Builder
			fallback.WriteString("GH_AW_GITHUB_TOKEN_FALLBACK")
			for artifactMappingValue(env, "SECRET_"+fallback.String()) != nil {
				fallback.WriteByte('_') //nolint:bufferresetbeforereuse // Collision suffixes intentionally accumulate on the same name.
			}
			fallbackName := fallback.String()
			lines[namesLine] = strings.Replace(lines[namesLine], names.Value, names.Value+","+fallbackName, 1)
			indent := strings.Repeat(" ", key.Column-1)
			lines[line] = fmt.Sprintf("%s%sSECRET_%s: %s\n", lines[line], indent, fallbackName, value.Value)
		}
	}
}

func isDefaultCreateTokenFallback(value string) bool {
	if !strings.HasPrefix(value, "${{") || !strings.HasSuffix(value, "}}") {
		return false
	}
	for part := range strings.SplitSeq(stripExpressionWrapper(value), "||") {
		switch strings.TrimSpace(part) {
		case "secrets.GH_AW_GITHUB_TOKEN", "secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN", "secrets.GITHUB_TOKEN":
		default:
			return false
		}
	}
	return true
}
