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
	inField := false
	applied := make([]bool, len(updates))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if getIndentation(line) == "" && isFrontmatterFieldLine(trimmed, fieldName) {
			inField = true
			lines[i] = replaceFrontmatterRefValues(line, updates, applied)
			continue
		}
		if inField && isTopLevelKey(line) {
			break
		}
		if !inField || !isFrontmatterRefValueLine(trimmed, objectKey) {
			continue
		}
		lines[i] = replaceFrontmatterRefValues(line, updates, applied)
	}

	if slices.Contains(applied, false) {
		return content, false
	}
	updatedFrontmatter := strings.Join(lines, "\n")
	return strings.Replace(content, originalFrontmatter, updatedFrontmatter, 1), true
}

func isFrontmatterFieldLine(trimmed, fieldName string) bool {
	key, _, found := strings.Cut(trimmed, ":")
	if !found {
		return false
	}
	return unquoteYAMLKey(strings.TrimSpace(key)) == fieldName
}

// unquoteYAMLKey strips matching surrounding quotes so quoted keys such as "skills"
// or 'plugins' are recognized alongside their plain form.
func unquoteYAMLKey(key string) string {
	if len(key) < 2 {
		return key
	}
	quote := key[0]
	if (quote == '\'' || quote == '"') && key[len(key)-1] == quote {
		return key[1 : len(key)-1]
	}
	return key
}

func isFrontmatterRefValueLine(trimmed, objectKey string) bool {
	if !strings.HasPrefix(trimmed, "- ") {
		return objectKey != noObjectKey && strings.HasPrefix(trimmed, objectKey+":")
	}
	value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	if objectKey != noObjectKey && strings.HasPrefix(value, objectKey+":") {
		return true
	}
	return !strings.Contains(value[:yamlValueEnd(value)], ":")
}

// replaceFrontmatterRefValues rewrites reference values in a single frontmatter line.
// Matches are located against the immutable original text and applied left to right,
// preferring the longest match at each position so overlapping references (for example
// "owner/repo@v1" inside "owner/repo@v10") are never corrupted by earlier replacements.
func replaceFrontmatterRefValues(line string, updates []frontmatterRefUpdate, applied []bool) string {
	valueEnd := yamlValueEnd(line)
	prefix := line[:valueEnd]
	var builder strings.Builder
	for i := 0; i < len(prefix); {
		best := -1
		bestLen := 0
		for j, update := range updates {
			if applied[j] || update.old == "" || len(update.old) <= bestLen {
				continue
			}
			if strings.HasPrefix(prefix[i:], update.old) {
				best = j
				bestLen = len(update.old)
			}
		}
		if best < 0 {
			builder.WriteByte(prefix[i])
			i++
			continue
		}
		builder.WriteString(updates[best].replacement)
		applied[best] = true
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
