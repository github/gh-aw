package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var skillRewriteLog = logger.New("cli:add_skill_rewrite")

// isLocalSkillRef reports whether spec is a local skill reference that should
// be rewritten to a fully-qualified "owner/repo/path@sha" form when the
// workflow is installed with "gh aw add".
//
// A spec is treated as local when it:
//   - is not empty,
//   - does not begin with "${{" (not a GitHub Actions expression), and
//   - contains no "@" separator (and therefore cannot be a fully-pinned
//     remote reference such as "owner/repo/path@<40-char-sha>").
func isLocalSkillRef(spec string) bool {
	spec = strings.TrimSpace(spec)
	return spec != "" && !strings.HasPrefix(spec, "${{") && !strings.Contains(spec, "@")
}

// getLocalHeadSHA returns the current HEAD commit SHA for the git repository
// rooted at gitRoot.
func getLocalHeadSHA(gitRoot string) (string, error) {
	// #nosec G204 -- gitRoot is validated as an absolute path (resolved via FindGitRoot).
	cmd := exec.Command("git", "-C", gitRoot, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve HEAD commit SHA: %w", err)
	}
	sha := strings.TrimSpace(string(output))
	if !gitutil.IsValidFullSHA(sha) {
		return "", fmt.Errorf("unexpected HEAD commit SHA format: %q", sha)
	}
	return sha, nil
}

// buildQualifiedSkillRef builds a fully-qualified skill spec from a local path
// reference, a repository slug (owner/repo), and a commit SHA.
//
// The local path is normalised by stripping a leading "./" before building
// the spec.
func buildQualifiedSkillRef(localPath, repoSlug, headSHA string) string {
	clean := strings.TrimPrefix(localPath, "./")
	return repoSlug + "/" + clean + "@" + headSHA
}

// applyLocalSkillRefRewriting rewrites any local skill path references in the
// workflow frontmatter "skills" array to fully-qualified "owner/repo/path@sha"
// specs. It is a no-op when:
//   - sourceInfo is nil or the workflow is not local,
//   - the current git root or repository slug cannot be determined, or
//   - the HEAD commit SHA cannot be resolved.
//
// Errors from git/repo lookups are treated as best-effort failures: a
// warning is printed (if verbose) and the original content is returned
// unchanged so that downstream compilation can surface a more actionable error.
func applyLocalSkillRefRewriting(content string, sourceInfo *FetchedWorkflow, opts AddOptions) (string, error) {
	if sourceInfo == nil || !sourceInfo.IsLocal {
		return content, nil
	}

	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		skillRewriteLog.Printf("Skipping local skill ref rewriting: not in a git repo: %v", err)
		return content, nil
	}

	repoSlug, err := GetCurrentRepoSlug()
	if err != nil {
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				fmt.Sprintf("Skipping local skill ref rewriting: could not determine current repository: %v", err),
			))
		}
		skillRewriteLog.Printf("Skipping local skill ref rewriting: could not get repo slug: %v", err)
		return content, nil
	}

	headSHA, err := getLocalHeadSHA(gitRoot)
	if err != nil {
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				fmt.Sprintf("Skipping local skill ref rewriting: could not determine HEAD commit: %v", err),
			))
		}
		skillRewriteLog.Printf("Skipping local skill ref rewriting: could not get HEAD SHA: %v", err)
		return content, nil
	}

	updated, err := rewriteLocalSkillRefsInContent(content, repoSlug, headSHA)
	if err != nil {
		skillRewriteLog.Printf("Failed to rewrite local skill refs: %v", err)
		return content, nil
	}
	if updated != content && opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Rewrote local skill references to fully-qualified specs"))
	}
	return updated, nil
}

// rewriteLocalSkillRefsInContent rewrites local skill path references inside
// the frontmatter "skills" array of content. Only entries that have no "@"
// separator are considered local; all others are left unchanged.
// If no local refs are present the original content is returned as-is.
func rewriteLocalSkillRefsInContent(content, repoSlug, headSHA string) (string, error) {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil || len(result.FrontmatterLines) == 0 {
		return content, nil
	}

	rawSkills, hasSkills := result.Frontmatter["skills"]
	if !hasSkills {
		return content, nil
	}
	skillsArr, ok := rawSkills.([]any)
	if !ok || len(skillsArr) == 0 {
		return content, nil
	}

	// Quick scan: skip expensive line editing when there are no local refs.
	anyLocal := false
	for _, rawSkill := range skillsArr {
		switch v := rawSkill.(type) {
		case string:
			if isLocalSkillRef(v) {
				anyLocal = true
			}
		case map[string]any:
			if skillVal, ok := v["skill"].(string); ok && isLocalSkillRef(skillVal) {
				anyLocal = true
			}
		}
	}
	if !anyLocal {
		return content, nil
	}

	newFrontmatterLines := rewriteSkillsInFrontmatterLines(result.FrontmatterLines, repoSlug, headSHA)

	// Reconstruct the file using the same convention as other frontmatter
	// editors in this package.
	var parts []string
	parts = append(parts, "---")
	parts = append(parts, newFrontmatterLines...)
	parts = append(parts, "---")
	if result.Markdown != "" {
		parts = append(parts, "")
		parts = append(parts, result.Markdown)
	}
	return strings.Join(parts, "\n"), nil
}

// rewriteSkillsInFrontmatterLines performs a line-by-line rewrite of skill
// spec values inside the "skills:" block of the frontmatter. It handles:
//   - string list items:  "  - .github/skills/my-skill"
//   - object list items:  "  - skill: .github/skills/my-skill"
//   - object block keys:  "    skill: .github/skills/my-skill" (rare form)
func rewriteSkillsInFrontmatterLines(lines []string, repoSlug, headSHA string) []string {
	newLines := make([]string, 0, len(lines))
	inSkills := false
	skillsBaseIndent := -1

	for _, line := range lines {
		if line == "" {
			newLines = append(newLines, line)
			continue
		}

		trimmed := strings.TrimSpace(line)
		indent := countLeadingSpacesSkill(line)

		if !inSkills {
			if isSkillsKeyLine(trimmed) {
				inSkills = true
				skillsBaseIndent = indent
			} else if strings.HasPrefix(trimmed, "skills:") {
				// Flow-sequence form: skills: [item1, item2, ...]
				line = rewriteFlowSkillsLine(line, trimmed, indent, repoSlug, headSHA)
			}
			newLines = append(newLines, line)
			continue
		}

		// A non-list, non-empty line at or below the "skills:" indent level
		// means we have left the skills block.
		if indent <= skillsBaseIndent && !strings.HasPrefix(trimmed, "-") {
			inSkills = false
			newLines = append(newLines, line)
			continue
		}

		// Handle list items: "  - <value>" or "  - skill: <value>"
		if strings.HasPrefix(trimmed, "- ") {
			itemContent := trimmed[2:] // content after "- "
			leadingSpace := strings.Repeat(" ", indent)

			if rest, ok := strings.CutPrefix(itemContent, "skill:"); ok {
				// Object form: "- skill: <value>"
				rawVal := strings.TrimSpace(rest)
				valPart, comment := splitYAMLValueAndComment(rawVal)
				unquoted := trimYAMLQuotesSkill(valPart)
				if isLocalSkillRef(unquoted) {
					qualified := buildQualifiedSkillRef(unquoted, repoSlug, headSHA)
					suffix := ""
					if comment != "" {
						suffix = " " + comment
					}
					line = leadingSpace + "- skill: " + qualified + suffix
					skillRewriteLog.Printf("Rewrote local skill ref (object form): %q -> %q", unquoted, qualified)
				}
			} else {
				// String form: "- <value>"
				valPart, comment := splitYAMLValueAndComment(itemContent)
				unquoted := trimYAMLQuotesSkill(valPart)
				if isLocalSkillRef(unquoted) {
					qualified := buildQualifiedSkillRef(unquoted, repoSlug, headSHA)
					suffix := ""
					if comment != "" {
						suffix = " " + comment
					}
					line = leadingSpace + "- " + qualified + suffix
					skillRewriteLog.Printf("Rewrote local skill ref (string form): %q -> %q", unquoted, qualified)
				}
			}
		} else if indent > skillsBaseIndent && strings.HasPrefix(trimmed, "skill:") {
			// Object block key on its own line (rare YAML form where the list
			// item marker was on the previous line and skill: is indented):
			//   -
			//     skill: .github/skills/my-skill
			rawVal := strings.TrimSpace(strings.TrimPrefix(trimmed, "skill:"))
			valPart, comment := splitYAMLValueAndComment(rawVal)
			unquoted := trimYAMLQuotesSkill(valPart)
			if isLocalSkillRef(unquoted) {
				qualified := buildQualifiedSkillRef(unquoted, repoSlug, headSHA)
				leadingSpace := strings.Repeat(" ", indent)
				suffix := ""
				if comment != "" {
					suffix = " " + comment
				}
				line = leadingSpace + "skill: " + qualified + suffix
				skillRewriteLog.Printf("Rewrote local skill ref (block object form): %q -> %q", unquoted, qualified)
			}
		}

		newLines = append(newLines, line)
	}

	return newLines
}

// trimYAMLQuotesSkill strips a single layer of matching single or double
// quotes from a YAML scalar value.
func trimYAMLQuotesSkill(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// splitYAMLValueAndComment splits an inline YAML scalar from its optional
// trailing comment. Per the YAML spec, a comment '#' must be preceded by at
// least one space or tab. '#' characters that appear inside single- or
// double-quoted strings are not treated as comment delimiters.
// Returns (value, comment) where comment, when non-empty, includes the
// leading '#' character. value has trailing whitespace trimmed.
func splitYAMLValueAndComment(s string) (value, comment string) {
	var inSingle, inDouble bool
	for i := range len(s) {
		ch := s[i]
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case ch == '#' && !inSingle && !inDouble && i > 0:
			prev := s[i-1]
			if prev == ' ' || prev == '\t' {
				return strings.TrimRight(s[:i], " \t"), s[i:]
			}
		}
	}
	return s, ""
}

// isSkillsKeyLine reports whether a trimmed YAML line is the "skills:"
// mapping key, optionally followed by whitespace and/or a trailing comment.
// Returns false for flow-sequence values such as "skills: [...]".
func isSkillsKeyLine(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "skills:") {
		return false
	}
	rest := strings.TrimLeft(trimmed[len("skills:"):], " \t")
	return rest == "" || strings.HasPrefix(rest, "#")
}

// rewriteFlowSkillsLine rewrites any local skill refs inside an inline
// flow-sequence on the "skills:" YAML key line, e.g.
//
//	skills: [.github/skills/foo, owner/repo/skill@sha]
//
// Non-local items and overall line structure (indentation, trailing comment)
// are preserved. Only flat string items are rewritten; object items inside
// flow sequences are left unchanged (best-effort).
func rewriteFlowSkillsLine(line, trimmed string, indent int, repoSlug, headSHA string) string {
	rest := strings.TrimLeft(trimmed[len("skills:"):], " \t")
	// Split off any trailing line comment that follows the sequence.
	seqPart, lineComment := splitYAMLValueAndComment(rest)
	seqPart = strings.TrimRight(seqPart, " \t")
	if !strings.HasPrefix(seqPart, "[") || !strings.HasSuffix(seqPart, "]") {
		return line
	}
	inner := seqPart[1 : len(seqPart)-1]
	if strings.TrimSpace(inner) == "" {
		return line // empty sequence, nothing to rewrite
	}
	parts := strings.Split(inner, ",")
	changed := false
	for i, part := range parts {
		trimPart := strings.TrimSpace(part)
		unquoted := trimYAMLQuotesSkill(trimPart)
		if !isLocalSkillRef(unquoted) {
			continue
		}
		qualified := buildQualifiedSkillRef(unquoted, repoSlug, headSHA)
		leadWS := len(part) - len(strings.TrimLeft(part, " \t"))
		parts[i] = part[:leadWS] + qualified
		changed = true
		skillRewriteLog.Printf("Rewrote local skill ref (flow form): %q -> %q", unquoted, qualified)
	}
	if !changed {
		return line
	}
	newSeq := "[" + strings.Join(parts, ",") + "]"
	result := strings.Repeat(" ", indent) + "skills: " + newSeq
	if lineComment != "" {
		result += " " + lineComment
	}
	return result
}

// countLeadingSpacesSkill returns the number of leading space/tab characters
// in s, counting each tab as one unit.
func countLeadingSpacesSkill(s string) int {
	for i, c := range s {
		if c != ' ' && c != '\t' {
			return i
		}
	}
	return len(s)
}
