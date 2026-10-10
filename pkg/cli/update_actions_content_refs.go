package cli

// This file rewrites "uses:" action refs and skill refs found inside Markdown
// workflow content. It is invoked by update_actions_workflow_files.go while
// walking workflow files.

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/parser"
)

type skillRefUpdateResolver func(ctx context.Context, repo, currentRef string, allowMajor, verbose bool, coolDown time.Duration) (string, error)

// noObjectKey signals to updateFrontmatterRepoRefsInContentWithResolver that the field
// being updated (e.g. "plugins") does not support the map[string]any object form with a
// nested ref key, so object-form entries are left untouched.
const noObjectKey = ""

type frontmatterRefUpdate struct {
	old         string
	replacement string
}

func updateSkillRefsInContent(ctx context.Context, content string, allowMajor, verbose bool, coolDown time.Duration) (bool, string, error) {
	return updateSkillRefsInContentWithResolver(ctx, content, allowMajor, verbose, coolDown, resolveLatestRef)
}

func updatePluginRefsInContent(ctx context.Context, content string, allowMajor, verbose bool, coolDown time.Duration) (bool, string, error) {
	return updatePluginRefsInContentWithResolver(ctx, content, allowMajor, verbose, coolDown, resolveLatestRef)
}

func updateSkillRefsInContentWithResolver(
	ctx context.Context,
	content string,
	allowMajor, verbose bool,
	coolDown time.Duration,
	resolver skillRefUpdateResolver,
) (bool, string, error) {
	return updateFrontmatterRepoRefsInContentWithResolver(ctx, content, "skills", "skill", allowMajor, verbose, coolDown, resolver)
}

func updatePluginRefsInContentWithResolver(
	ctx context.Context,
	content string,
	allowMajor, verbose bool,
	coolDown time.Duration,
	resolver skillRefUpdateResolver,
) (bool, string, error) {
	return updateFrontmatterRepoRefsInContentWithResolver(ctx, content, "plugins", noObjectKey, allowMajor, verbose, coolDown, resolver)
}

func updateFrontmatterRepoRefsInContentWithResolver(
	ctx context.Context,
	content string,
	fieldName string,
	objectKey string,
	allowMajor, verbose bool,
	coolDown time.Duration,
	resolver skillRefUpdateResolver,
) (bool, string, error) {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		if verbose {
			updateLog.Printf("Skipping %s update for content without parseable frontmatter: %v", fieldName, err)
		}
		return false, content, nil
	}
	if result == nil || result.Frontmatter == nil {
		return false, content, nil
	}

	rawRefs, ok := result.Frontmatter[fieldName].([]any)
	if !ok || len(rawRefs) == 0 {
		return false, content, nil
	}

	changed := false
	var updates []frontmatterRefUpdate
	for _, rawRef := range rawRefs {
		switch typed := rawRef.(type) {
		case string:
			updated, updatedRef, err := updateSkillRefValue(ctx, fieldName, typed, allowMajor, verbose, coolDown, resolver)
			if err != nil {
				return false, content, err
			}
			if updated {
				updates = append(updates, frontmatterRefUpdate{old: typed, replacement: updatedRef})
				changed = true
			}
		case map[string]any:
			if objectKey == noObjectKey {
				continue
			}
			skillRef, ok := typed[objectKey].(string)
			if !ok {
				continue
			}
			updated, updatedRef, err := updateSkillRefValue(ctx, fieldName, skillRef, allowMajor, verbose, coolDown, resolver)
			if err != nil {
				return false, content, err
			}
			if updated {
				updates = append(updates, frontmatterRefUpdate{old: skillRef, replacement: updatedRef})
				changed = true
			}
		}
	}
	if !changed {
		return false, content, nil
	}

	updatedContent, applied := applyFrontmatterRefUpdates(content, result.FrontmatterLines, fieldName, objectKey, updates)
	if !applied {
		return false, content, fmt.Errorf("unable to locate parsed %s references in frontmatter", fieldName)
	}
	return true, updatedContent, nil
}

func applyFrontmatterRefUpdates(content string, frontmatterLines []string, fieldName, objectKey string, updates []frontmatterRefUpdate) (string, bool) {
	originalFrontmatter := strings.Join(frontmatterLines, "\n")
	if originalFrontmatter == "" {
		return content, false
	}

	lines := slices.Clone(frontmatterLines)
	candidates := frontmatterRefCandidates(updates)
	inField := false
	applied := make([]bool, len(updates))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if getIndentation(line) == "" && isFrontmatterFieldLine(trimmed, fieldName) {
			inField = true
			lines[i] = replaceFrontmatterRefValues(line, candidates, applied)
			continue
		}
		if inField && isTopLevelKey(line) {
			break
		}
		if !inField || !isFrontmatterRefValueLine(trimmed, objectKey) {
			continue
		}
		lines[i] = replaceFrontmatterRefValues(line, candidates, applied)
	}

	if slices.Contains(applied, false) {
		return content, false
	}
	updatedFrontmatter := strings.Join(lines, "\n")
	// Anchor the rewrite to the frontmatter block so identical text in the Markdown body
	// (for example a documented frontmatter snippet) can never be rewritten instead.
	firstNewline := strings.IndexByte(content, '\n')
	if firstNewline < 0 {
		return content, false
	}
	frontmatterStart := firstNewline + 1
	if !strings.HasPrefix(content[frontmatterStart:], originalFrontmatter) {
		return content, false
	}
	return content[:frontmatterStart] + updatedFrontmatter + content[frontmatterStart+len(originalFrontmatter):], true
}

func isFrontmatterFieldLine(trimmed, fieldName string) bool {
	key, ok := parseFrontmatterKey(trimmed)
	return ok && key == fieldName
}

// parseFrontmatterKey extracts the mapping key from a trimmed frontmatter line,
// unquoting single- or double-quoted keys such as "skills" or 'plugins'.
func parseFrontmatterKey(trimmed string) (string, bool) {
	if trimmed == "" {
		return "", false
	}
	if quote := trimmed[0]; quote == '\'' || quote == '"' {
		key, rest, ok := cutQuotedYAMLScalar(trimmed)
		if !ok || !strings.HasPrefix(strings.TrimLeft(rest, " \t"), ":") {
			return "", false
		}
		return key, true
	}
	key, _, found := strings.Cut(trimmed, ":")
	if !found {
		return "", false
	}
	return strings.TrimSpace(key), true
}

// cutQuotedYAMLScalar decodes the quoted scalar starting at the beginning of value and
// returns its unescaped content together with the remaining text after the closing quote.
// Only literal-character escapes (for example \" and \\) are decoded; control-character
// and unicode escapes such as \n or \uXXXX are not, since frontmatter keys and repository
// references never contain them.
func cutQuotedYAMLScalar(value string) (string, string, bool) {
	quote := value[0]
	var body strings.Builder
	for i := 1; i < len(value); i++ {
		switch {
		case quote == '\'' && value[i] == '\'':
			if i+1 < len(value) && value[i+1] == '\'' {
				body.WriteByte('\'')
				i++
				continue
			}
			return body.String(), value[i+1:], true
		case quote == '"' && value[i] == '\\' && i+1 < len(value):
			body.WriteByte(value[i+1])
			i++
			continue
		case quote == '"' && value[i] == '"':
			return body.String(), value[i+1:], true
		}
		body.WriteByte(value[i])
	}
	return "", "", false
}

// isFrontmatterRefValueLine reports whether a line inside the field block may carry a
// reference value. Comments never do. Fields without an object form (for example
// "plugins") additionally leave mapping entries, including flow maps, untouched.
func isFrontmatterRefValueLine(trimmed, objectKey string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	if objectKey != noObjectKey {
		return true
	}
	return !frontmatterLineHasMapping(trimmed)
}

func frontmatterLineHasMapping(trimmed string) bool {
	value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	value = strings.TrimPrefix(value, "{")
	return strings.Contains(value[:yamlValueEnd(value)], ":")
}

// frontmatterRefCandidate is one raw YAML encoding of a parsed reference value, paired
// with the matching encoding of its replacement. Parsed values are unescaped, so quoted
// scalars are matched through their single- and double-quoted encodings as well.
type frontmatterRefCandidate struct {
	update      int
	search      string
	replacement string
}

func frontmatterRefCandidates(updates []frontmatterRefUpdate) []frontmatterRefCandidate {
	encoders := []func(string) string{
		func(value string) string { return value },
		encodeSingleQuotedYAMLBody,
		encodeDoubleQuotedYAMLBody,
	}
	var candidates []frontmatterRefCandidate
	for i, update := range updates {
		if update.old == "" {
			continue
		}
		seen := make(map[string]struct{}, len(encoders))
		for _, encode := range encoders {
			search := encode(update.old)
			if _, duplicate := seen[search]; duplicate {
				continue
			}
			seen[search] = struct{}{}
			candidates = append(candidates, frontmatterRefCandidate{
				update:      i,
				search:      search,
				replacement: encode(update.replacement),
			})
		}
	}
	return candidates
}

func encodeSingleQuotedYAMLBody(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func encodeDoubleQuotedYAMLBody(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(escaped, `"`, `\"`)
}

// replaceFrontmatterRefValues rewrites reference values in a single frontmatter line.
// Matches are located against the immutable original text and applied left to right,
// preferring the longest match at each position so overlapping references (for example
// "owner/repo@v1" inside "owner/repo@v10") are never corrupted by earlier replacements.
func replaceFrontmatterRefValues(line string, candidates []frontmatterRefCandidate, applied []bool) string {
	valueEnd := yamlValueEnd(line)
	prefix := line[:valueEnd]
	var builder strings.Builder
	for i := 0; i < len(prefix); {
		best := -1
		bestLen := 0
		for j, candidate := range candidates {
			if applied[candidate.update] || len(candidate.search) <= bestLen {
				continue
			}
			if strings.HasPrefix(prefix[i:], candidate.search) {
				best = j
				bestLen = len(candidate.search)
			}
		}
		if best < 0 {
			builder.WriteByte(prefix[i])
			i++
			continue
		}
		builder.WriteString(candidates[best].replacement)
		applied[candidates[best].update] = true
		i += bestLen
	}
	return builder.String() + line[valueEnd:]
}

func yamlValueEnd(line string) int {
	var quote byte
	for i := 0; i < len(line); {
		current := line[i]
		if quote == 0 {
			switch current {
			case '\'', '"':
				quote = current
			case '#':
				if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
					return i
				}
			}
			i++
			continue
		}
		if quote == '\'' && current == '\'' {
			if i+1 < len(line) && line[i+1] == '\'' {
				i += 2
				continue
			}
			quote = 0
		} else if quote == '"' && current == '"' && !isBackslashEscaped(line, i) {
			quote = 0
		}
		i++
	}
	return len(line)
}

func isBackslashEscaped(value string, index int) bool {
	backslashes := 0
	for index--; index >= 0 && value[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func updateSkillRefValue(
	ctx context.Context,
	fieldName string,
	skillRef string,
	allowMajor, verbose bool,
	coolDown time.Duration,
	resolver skillRefUpdateResolver,
) (bool, string, error) {
	trimmedSkillRef := strings.TrimSpace(skillRef)
	if trimmedSkillRef == "" || strings.Contains(trimmedSkillRef, "${{") {
		return false, skillRef, nil
	}
	spec, currentRef, ok := strings.Cut(trimmedSkillRef, "@")
	spec = strings.TrimSpace(spec)
	currentRef = strings.TrimSpace(currentRef)
	if !ok || spec == "" || currentRef == "" {
		return false, skillRef, nil
	}

	repo := gitutil.ExtractBaseRepo(spec)
	if repo == "" {
		return false, skillRef, nil
	}
	latestRef, err := resolver(ctx, repo, currentRef, allowMajor, verbose, coolDown)
	if err != nil {
		if verbose {
			updateLog.Printf("Skipping %s update for %s@%s: %v", fieldName, spec, currentRef, err)
		}
		return false, skillRef, nil
	}
	if latestRef == "" || latestRef == currentRef {
		return false, skillRef, nil
	}
	return true, spec + "@" + latestRef, nil
}

//nolint:largefunc
func updateActionRefsInContentWithDeps(ctx context.Context, deps actionUpdateDeps, content string, cache map[string]latestReleaseResult, coolDownCache map[string]coolDownCheckResult, allowMajor, verbose bool, coolDown time.Duration) (bool, string, error) {
	changed := false
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		match := actionRefPattern.FindStringSubmatchIndex(line)
		if match == nil {
			continue
		}

		// Extract matched groups
		prefix := line[match[2]:match[3]] // "uses: "
		repo := line[match[4]:match[5]]   // e.g. "actions/checkout"
		ref := line[match[6]:match[7]]    // SHA or version tag
		comment := ""
		if match[8] >= 0 {
			comment = line[match[8]:match[9]] // e.g. " # v6.0.2"
		}
		trailing := ""
		if match[10] >= 0 {
			trailing = line[match[10]:match[11]]
		}

		// When release bumps are disabled, skip non-core (non actions/*) action refs.
		effectiveAllowMajor := allowMajor || isCoreAction(repo)
		if !effectiveAllowMajor {
			continue
		}

		// Determine the "current version" to pass to the latest-release resolver.
		isSHA := IsCommitSHA(ref)
		currentVersion := ref
		if isSHA {
			// Extract version from comment (e.g., " # v6.0.2" -> "v6.0.2")
			if comment != "" {
				commentVersion := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comment), "#"))
				if commentVersion != "" {
					currentVersion = commentVersion
				} else {
					currentVersion = ""
				}
			} else {
				currentVersion = ""
			}
		}

		// Resolve latest version/SHA, using the cache to avoid redundant API calls.
		// Use "|" as separator since GitHub repo names cannot contain "|".
		cacheKey := repo + "|" + currentVersion
		result, cached := cache[cacheKey]
		if !cached {
			latestVersion, latestSHA, err := deps.getLatestRelease(ctx, repo, currentVersion, effectiveAllowMajor, verbose)
			if err != nil {
				updateLog.Printf("Failed to get latest release for %s: %v", repo, err)
				continue
			}
			result = latestReleaseResult{version: latestVersion, sha: latestSHA}
			cache[cacheKey] = result
		}
		latestVersion := result.version
		latestSHA := result.sha

		if isSHA {
			if latestSHA == ref {
				continue // SHA unchanged
			}
		} else {
			if latestVersion == ref {
				continue // Version tag unchanged
			}
			// Prevent downgrades: if the proposed version is older than the current, skip.
			currentVer := parseVersion(ref)
			proposedVer := parseVersion(latestVersion)
			if currentVer != nil && proposedVer != nil && currentVer.IsNewer(proposedVer) {
				updateLog.Printf("Skipping %s in workflow file: proposed version %s is older than current %s (would be a downgrade)", repo, latestVersion, ref)
				continue
			}
		}

		// Apply cooldown: if the repo is not exempt and the release is too recent, try
		// progressively older releases (still newer than current) until finding one that
		// has passed the cooldown period.
		if !isExemptFromCoolDown(repo) {
			coolDownKey := repo + "@" + latestVersion
			coolDownResult, coolDownCached := coolDownCache[coolDownKey]
			if !coolDownCached {
				coolDownResult = deps.checkCoolDown(ctx, repo, latestVersion, coolDown)
				coolDownCache[coolDownKey] = coolDownResult
			}
			if coolDownResult.InCoolDown {
				cooldownLog.Printf("Action ref %s in workflow: %s", repo, coolDownResult.Message)

				// Try to find an older release that has passed the cooldown period.
				olderVersion, olderSHA, findErr := findCooledDownActionVersion(ctx, deps, repo, currentVersion, effectiveAllowMajor, verbose, coolDown, latestVersion)
				if findErr != nil || olderVersion == "" || olderSHA == "" {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping release candidate %s@%s: %s", repo, latestVersion, coolDownResult.Message)))
					}
					continue
				}
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Falling back to %s for %s (latest release candidate is still in cooldown)", olderVersion, repo)))
				}
				// Use the older, cooled-down release and update the per-invocation cache.
				result = latestReleaseResult{version: olderVersion, sha: olderSHA}
				cache[cacheKey] = result
				latestVersion = olderVersion
				latestSHA = olderSHA
			}
		}

		// Build the new uses line
		var newRef string
		if isSHA {
			// SHA-pinned references stay SHA-pinned, updated to latest SHA + version comment
			newRef = fmt.Sprintf("%s%s%s@%s  # %s%s", line[:match[2]], prefix, repo, latestSHA, latestVersion, trailing)
		} else {
			// Version tag references just get the new version tag
			newRef = fmt.Sprintf("%s%s%s@%s%s%s", line[:match[2]], prefix, repo, latestVersion, comment, trailing)
		}

		updateLog.Printf("Updating %s from %s to %s in line %d", repo, ref, latestVersion, i+1)
		lines[i] = newRef
		changed = true
	}

	return changed, strings.Join(lines, "\n"), nil
}
