package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
)

var skillsFrontmatterLog = logger.New("workflow:skills_frontmatter")

// skillRepoPathRegexp matches the repository (and optional skill sub-path) portion
// of a remote skill spec, e.g. "owner/repo" or "owner/repo/skill/path".
var skillRepoPathRegexp = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*$`)

// skillRefCharsRegexp restricts non-SHA refs (branch/tag names) to a safe character
// set. This is deliberately more permissive than a SHA (branch names may contain "/"
// for hierarchical names such as "release/1.0") while still preventing shell/argument
// injection when the ref is later passed to "gh" subprocesses.
var skillRefCharsRegexp = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]*$`)

var localSkillPathRegexp = regexp.MustCompile(`^(?:\./)?(?:\.[A-Za-z0-9_-][A-Za-z0-9_.-]*|[A-Za-z0-9_-][A-Za-z0-9_.-]*)(?:/(?:\.[A-Za-z0-9_-][A-Za-z0-9_.-]*|[A-Za-z0-9_-][A-Za-z0-9_.-]*))*$`)
var skillsGitHubTokenExpressionRegexp = regexp.MustCompile(`^\$\{\{\s*(secrets\.[A-Za-z_][A-Za-z0-9_]*(\s*\|\|\s*secrets\.[A-Za-z_][A-Za-z0-9_]*)*|needs\.[A-Za-z_][A-Za-z0-9_]*\.outputs\.[A-Za-z_][A-Za-z0-9_]*)\s*\}\}$`)

// looksLikeAmbiguousSHA reports whether ref is composed entirely of hex characters
// with a length between 7 and 40 (inclusive) but is not itself a valid, canonical
// (lowercase, 40-char) full SHA. Such values are rejected as skill refs because they
// could be mistaken for (or silently truncate to) a real commit SHA, which is a
// well-known ref-confusion/collision risk; authors should use the full 40-character
// lowercase SHA or a clearly non-SHA branch/tag name instead.
func looksLikeAmbiguousSHA(ref string) bool {
	return len(ref) >= 7 && len(ref) <= 40 && gitutil.IsHexString(ref) && !gitutil.IsValidFullSHA(ref)
}

// parsedSkillRefSpec classifies a skill reference for validation and resolution.
type parsedSkillRefSpec struct {
	trimmed      string
	repoPath     string
	ref          string
	isLocal      bool
	isExpression bool
	isRemote     bool
	isFullSHA    bool
}

// parseSkillRefSpec classifies local paths, expressions, and valid remote skill
// references. It does not validate local paths or non-SHA remote refs; callers
// apply their respective validation or resolution behavior to the result.
func parseSkillRefSpec(spec string) parsedSkillRefSpec {
	parsed := parsedSkillRefSpec{trimmed: strings.TrimSpace(spec)}
	if parsed.trimmed == "" {
		return parsed
	}
	if strings.Contains(parsed.trimmed, "${{") {
		parsed.isExpression = true
		return parsed
	}
	if !strings.Contains(parsed.trimmed, "@") {
		parsed.isLocal = true
		return parsed
	}

	repoPath, ref, hasAt := strings.Cut(parsed.trimmed, "@")
	if hasAt && skillRepoPathRegexp.MatchString(repoPath) {
		parsed.repoPath = repoPath
		parsed.ref = ref
		parsed.isRemote = true
		parsed.isFullSHA = gitutil.IsValidFullSHA(ref)
	}
	return parsed
}

// SkillReference describes a single skills[] entry in workflow frontmatter.
// It supports both legacy string-only entries and object entries with per-skill auth.
type SkillReference struct {
	Skill       string           `json:"skill,omitempty"`
	GitHubToken string           `json:"github-token,omitempty"`
	GitHubApp   *GitHubAppConfig `json:"github-app,omitempty"`
}

func validateSkillSpecValue(skillSpec string, idx int) error {
	parsed := parseSkillRefSpec(skillSpec)
	if parsed.trimmed == "" {
		return fmt.Errorf("skills[%d] must be a non-empty string. Example: skills[%d]: \"owner/repo@abc1234...\"", idx, idx)
	}
	// Local path references (no "@" and not an expression) are allowed; they
	// are installed with --from-local at runtime and rewritten to a remote
	// repospec by "gh aw add".
	if parsed.isLocal {
		if !localSkillPathRegexp.MatchString(parsed.trimmed) {
			return fmt.Errorf(
				"skills[%d] local paths must be repository-relative without '..' traversal segments (got %q). Example: skills[%d]: \"./skills/my-skill\"",
				idx,
				skillSpec,
				idx,
			)
		}
		return nil
	}

	// GitHub Actions expressions are not supported as skill refs: they cannot be
	// syntax-validated or resolved to a SHA at compile time.
	if parsed.isExpression {
		return fmt.Errorf(
			"skills[%d] must use owner/repo@<ref> or owner/repo/skill/path@<ref> and does not support expressions (got %q). Example: skills[%d]: \"owner/repo@main\" or skills[%d]: \"owner/repo@abcdef1234567890abcdef1234567890abcdef12\"",
			idx,
			skillSpec,
			idx,
			idx,
		)
	}

	if !parsed.isRemote {
		return fmt.Errorf(
			"skills[%d] must use owner/repo@<ref> or owner/repo/skill/path@<ref> (got %q). Example: skills[%d]: \"owner/repo@main\" or skills[%d]: \"owner/repo@abcdef1234567890abcdef1234567890abcdef12\"",
			idx,
			skillSpec,
			idx,
			idx,
		)
	}
	return validateSkillRemoteRef(parsed, skillSpec, idx)
}

func validateSkillRemoteRef(parsed parsedSkillRefSpec, skillSpec string, idx int) error {
	// An empty ref ("owner/repo@") explicitly opts out of pinning: the skill is
	// installed from the repository's default branch. This is allowed, but
	// triggers a compile-time warning recommending an explicit ref (see
	// emitSkillPinningWarnings).
	if parsed.ref == "" {
		return nil
	}

	if parsed.isFullSHA {
		return nil
	}

	if looksLikeAmbiguousSHA(parsed.ref) {
		return fmt.Errorf(
			"skills[%d] ref %q looks like a truncated or malformed commit SHA (got %q); use the full 40-character lowercase SHA or a branch/tag name",
			idx,
			parsed.ref,
			skillSpec,
		)
	}

	if !skillRefCharsRegexp.MatchString(parsed.ref) || strings.Contains(parsed.ref, "..") {
		return fmt.Errorf(
			"skills[%d] ref %q contains unsupported characters; refs may only contain letters, digits, '.', '_', '-', and '/', must start with a letter or digit, and must not contain '..' (got %q)",
			idx,
			parsed.ref,
			skillSpec,
		)
	}

	return nil
}

func validateFrontmatterSkills(frontmatter map[string]any) error {
	rawSkills, hasSkills := frontmatter["skills"]
	if !hasSkills {
		return nil
	}

	skills, ok := rawSkills.([]any)
	if !ok {
		return errors.New("skills must be an array of skill references. Example: skills: [\"owner/repo@sha\"]")
	}

	skillsFrontmatterLog.Printf("validateFrontmatterSkills: validating %d skill entr(ies)", len(skills))

	for i, rawSkill := range skills {
		switch typed := rawSkill.(type) {
		case string:
			if err := validateSkillSpecValue(typed, i); err != nil {
				return err
			}
		case map[string]any:
			if err := validateFrontmatterSkillObject(typed, i); err != nil {
				return err
			}
		default:
			return fmt.Errorf("skills[%d] must be a string or object. Example: skills[%d]: \"owner/repo@sha\" or {skill: \"owner/repo@sha\"}", i, i)
		}
	}

	return nil
}

func validateFrontmatterSkillObject(typed map[string]any, i int) error {
	if len(typed) == 0 {
		return fmt.Errorf("skills[%d] must include a non-empty skill field. Example: skills[%d]: {skill: \"owner/repo@sha\"}", i, i)
	}
	skillValue, hasSkill := typed["skill"]
	if !hasSkill {
		return fmt.Errorf("skills[%d].skill is required. Example: skills[%d].skill: \"owner/repo@sha\"", i, i)
	}
	skillSpec, ok := skillValue.(string)
	if !ok {
		return fmt.Errorf("skills[%d].skill must be a string. Example: skills[%d].skill: \"owner/repo@sha\"", i, i)
	}
	if strings.TrimSpace(skillSpec) == "" {
		return fmt.Errorf("skills[%d].skill must be a non-empty string. Example: skills[%d].skill: \"owner/repo@sha\"", i, i)
	}
	if err := validateSkillSpecValue(skillSpec, i); err != nil {
		return err
	}
	for key := range typed {
		switch key {
		case "skill", "github-token", "github-app":
			// allowed
		default:
			return fmt.Errorf("skills[%d].%s is not supported; allowed fields are skill, github-token, github-app", i, key)
		}
	}
	return validateSkillCredentials(typed, i)
}

func validateSkillCredentials(typed map[string]any, i int) error {
	_, hasToken := typed["github-token"]
	_, hasApp := typed["github-app"]
	if hasToken && hasApp {
		return fmt.Errorf("skills[%d]: github-token and github-app are mutually exclusive; use one or the other", i)
	}
	if tokenValue, hasToken := typed["github-token"]; hasToken {
		token, ok := tokenValue.(string)
		if !ok {
			return fmt.Errorf("skills[%d].github-token must be a string. Example: skills[%d].github-token: \"${{ secrets.MY_TOKEN }}\"", i, i)
		}
		if !skillsGitHubTokenExpressionRegexp.MatchString(token) {
			return fmt.Errorf(
				"skills[%d].github-token must be a valid GitHub token expression. Example: skills[%d].github-token: \"${{ secrets.NAME }}\" or \"${{ needs.auth.outputs.token }}\"",
				i,
				i,
			)
		}
	}
	if app, hasApp := typed["github-app"]; hasApp {
		appMap, ok := app.(map[string]any)
		if !ok {
			return fmt.Errorf("skills[%d].github-app must be an object. Example: skills[%d].github-app: {client-id: \"Iv1.abc\", private-key: \"...\"}", i, i)
		}
		parsed := parseAppConfig(appMap)
		if !parsed.hasRequiredCredentials() {
			return fmt.Errorf("skills[%d].github-app must include non-empty client-id/app-id and private-key. Example: skills[%d].github-app: {client-id: \"Iv1.abc\", private-key: \"...\"}", i, i)
		}
	}
	return nil
}

func parseRawSkillReferences(rawSkills []any) []SkillReference {
	skillsFrontmatterLog.Printf("parseRawSkillReferences: parsing %d raw skill entr(ies)", len(rawSkills))
	refs := make([]SkillReference, 0, len(rawSkills))
	for _, rawSkill := range rawSkills {
		switch typed := rawSkill.(type) {
		case string:
			skillSpec := strings.TrimSpace(typed)
			if skillSpec == "" {
				continue
			}
			refs = append(refs, SkillReference{Skill: skillSpec})
		case map[string]any:
			skillSpec, _ := typed["skill"].(string)
			if strings.TrimSpace(skillSpec) == "" {
				continue
			}
			ref := SkillReference{Skill: strings.TrimSpace(skillSpec)}
			if token, ok := typed["github-token"].(string); ok {
				ref.GitHubToken = token
			}
			if appMap, ok := typed["github-app"].(map[string]any); ok {
				ref.GitHubApp = parseAppConfig(appMap)
			}
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	skillsFrontmatterLog.Printf("parseRawSkillReferences: parsed %d skill reference(s)", len(refs))
	return refs
}
