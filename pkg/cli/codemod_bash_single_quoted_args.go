package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var bashSingleQuotedArgsCodemodLog = logger.New("cli:codemod_bash_single_quoted_args")

// getBashSingleQuotedArgsCodemod rewrites tools.bash entries that contain
// single-quoted shell arguments into equivalent double-quoted forms so Copilot
// shell allow-tool generation does not truncate them to a prefix.
func getBashSingleQuotedArgsCodemod() Codemod {
	return Codemod{
		ID:           "bash-single-quoted-args-rewrite",
		Name:         "Rewrite single-quoted bash tool args",
		Description:  "Rewrites tools.bash entries like grep -n 'foo' to grep -n \"foo\" when safe, reducing Copilot shell() truncation warnings.",
		IntroducedIn: "0.39.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			toolsValue, hasTools := frontmatter["tools"]
			if !hasTools {
				return content, false, nil
			}

			toolsMap, ok := toolsValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			bashValue, hasBash := toolsMap["bash"]
			if !hasBash {
				return content, false, nil
			}

			bashCommands, ok := bashValue.([]any)
			if !ok {
				return content, false, nil
			}

			updated := make([]any, len(bashCommands))
			copy(updated, bashCommands)

			changed := false
			var unsafeCommands []string
			for i, cmd := range bashCommands {
				cmdStr, ok := cmd.(string)
				if !ok {
					continue
				}

				rewritten, safe, rewrittenChanged := rewriteSingleQuotedBashArgs(cmdStr)
				if !safe {
					unsafeCommands = append(unsafeCommands, cmdStr)
					continue
				}
				if rewrittenChanged {
					updated[i] = rewritten
					changed = true
				}
			}

			for _, cmd := range unsafeCommands {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
					fmt.Sprintf("tools.bash entry %q contains an unclosed single-quoted segment and could not be safely rewritten; left unchanged", cmd)))
			}

			if !changed {
				return content, false, nil
			}

			toolsMap["bash"] = updated
			frontmatter["tools"] = toolsMap

			result, err := parser.ExtractFrontmatterFromContent(content)
			if err != nil {
				return content, false, fmt.Errorf("failed to parse frontmatter for rewrite: %w", err)
			}

			bashSingleQuotedArgsCodemodLog.Print("Rewrote single-quoted tools.bash arguments to safe double-quoted forms")
			updatedContent, err := reconstructWorkflowFileFromMap(frontmatter, result.Markdown)
			if err != nil {
				return content, false, fmt.Errorf("failed to reconstruct workflow content after rewrite: %w", err)
			}
			return updatedContent, true, nil
		},
	}
}

// rewriteSingleQuotedBashArgs rewrites single-quoted shell segments to
// double-quoted segments with escaping that preserves literal semantics.
// Returns rewritten command, whether rewrite was safe, and whether it changed.
func rewriteSingleQuotedBashArgs(cmd string) (string, bool, bool) {
	if !strings.Contains(cmd, "'") {
		return cmd, true, false
	}

	var b strings.Builder
	b.Grow(len(cmd) + 8)
	changed := false
	inDoubleQuotes := false
	escaped := false

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		if inDoubleQuotes {
			b.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inDoubleQuotes = false
			}
			continue
		}

		if escaped {
			b.WriteByte(ch)
			escaped = false
			continue
		}

		switch ch {
		case '\\':
			b.WriteByte(ch)
			escaped = true
			continue
		case '"':
			b.WriteByte(ch)
			inDoubleQuotes = true
			continue
		case '\'':
			j := i + 1
			for j < len(cmd) && cmd[j] != '\'' {
				j++
			}
			if j >= len(cmd) {
				return cmd, false, false
			}

			content := cmd[i+1 : j]
			b.WriteByte('"')
			for k := range len(content) {
				contentCh := content[k]
				switch contentCh {
				case '\\', '"', '$', '`':
					b.WriteByte('\\')
				}
				b.WriteByte(contentCh)
			}
			b.WriteByte('"')

			changed = true
			i = j
			continue
		default:
			b.WriteByte(ch)
		}
	}

	rewritten := b.String()
	if !changed || rewritten == cmd {
		return cmd, true, false
	}
	return rewritten, true, true
}
