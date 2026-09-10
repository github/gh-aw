//go:build !integration

package cli

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/parser"
)

const fuzzSkillRepo = "githubnext/skills/review/security"

// FuzzUpdateSkillRefsPreservesSyntheticWorkflows generates workflows whose frontmatter
// combines quoted keys, block and flow lists, object entries, duplicate and overlapping
// references, comments and arbitrary comment text, then checks that a skill upgrade only
// rewrites the targeted reference values and leaves every other byte untouched.
func FuzzUpdateSkillRefsPreservesSyntheticWorkflows(f *testing.F) {
	for shape := range uint32(64) {
		f.Add(shape, "keep this comment")
	}
	f.Add(uint32(7), "value: # not a comment boundary")
	f.Add(uint32(23), `quoted "text" and 'text'`)
	f.Add(uint32(41), "")

	f.Fuzz(func(t *testing.T, shape uint32, comment string) {
		comment = sanitizeFuzzComment(comment)
		content, expected := buildSyntheticSkillWorkflow(shape, comment)

		if _, err := parser.ExtractFrontmatterFromContent(content); err != nil {
			t.Fatalf("generated workflow does not parse: %v\n%s", err, content)
		}

		resolver := func(_ context.Context, repo, currentRef string, _, _ bool, _ time.Duration) (string, error) {
			if repo == "" {
				t.Fatalf("resolver received empty repo for ref %q", currentRef)
			}
			return bumpFuzzVersion(currentRef), nil
		}
		changed, got, err := updateSkillRefsInContentWithResolver(context.Background(), content, true, false, 0, resolver)
		if err != nil {
			t.Fatalf("skill update failed: %v\n%s", err, content)
		}
		if !changed {
			t.Fatalf("skill update was not applied\n%s", content)
		}
		if got != expected {
			t.Fatalf("upgrade changed bytes outside targeted references\n--- got ---\n%s\n--- want ---\n%s", got, expected)
		}
		if _, err := parser.ExtractFrontmatterFromContent(got); err != nil {
			t.Fatalf("upgraded workflow no longer parses: %v\n%s", err, got)
		}
	})
}

// FuzzYAMLValueEnd checks that the comment boundary detection used by reference
// replacement is stable: it never reports an offset outside the line and never splits
// a line so that the value part still contains an unquoted comment.
func FuzzYAMLValueEnd(f *testing.F) {
	f.Add("skills:")
	f.Add("  - owner/repo@v1 # comment")
	f.Add(`  - "owner/repo@v1 # not a comment"`)
	f.Add(`  - 'it''s # quoted' # real comment`)
	f.Add(`  - "escaped \" quote" # comment`)
	f.Add("#")

	f.Fuzz(func(t *testing.T, line string) {
		if len(line) > 4096 || strings.ContainsAny(line, "\n\r") {
			return
		}
		end := yamlValueEnd(line)
		if end < 0 || end > len(line) {
			t.Fatalf("yamlValueEnd returned out-of-range offset %d for %q", end, line)
		}
		value := line[:end]
		if again := yamlValueEnd(value); again != len(value) {
			t.Fatalf("yamlValueEnd is not idempotent for %q: %d != %d", line, again, len(value))
		}
		if end < len(line) && line[end] != '#' {
			t.Fatalf("yamlValueEnd for %q pointed at %q instead of a comment", line, line[end])
		}
	})
}

func sanitizeFuzzComment(comment string) string {
	var builder strings.Builder
	for _, r := range comment {
		// Drop non-printable runes and "@" so generated comments can never contain a
		// reference that the upgrade is expected to rewrite.
		if r < ' ' || r > '~' || r == '@' {
			continue
		}
		builder.WriteRune(r)
		if builder.Len() >= 40 {
			break
		}
	}
	return builder.String()
}

func bumpFuzzVersion(ref string) string {
	number, err := strconv.Atoi(strings.TrimPrefix(ref, "v"))
	if err != nil {
		return ref
	}
	return "v" + strconv.Itoa(number+1)
}

// buildSyntheticSkillWorkflow returns a workflow and the exact expected result of a skill
// upgrade for it, so that the fuzz target can assert byte-for-byte preservation.
func buildSyntheticSkillWorkflow(shape uint32, comment string) (content, expected string) {
	versions := []string{"v1", "v10", "v9", "v1"}
	versions = versions[:1+int(shape%4)]

	key := "skills"
	switch (shape >> 2) % 3 {
	case 1:
		key = `"skills"`
	case 2:
		key = `'skills'`
	}
	quote := ""
	switch (shape >> 4) % 3 {
	case 1:
		quote = "'"
	case 2:
		quote = `"`
	}
	indent := "  "
	if shape&(1<<6) != 0 {
		indent = "    "
	}
	inlineComment := ""
	if shape&(1<<7) != 0 {
		inlineComment = " # " + comment + " " + fuzzSkillRefValue(versions[0])
	}
	useFlowList := shape&(1<<8) != 0
	// 0: plain scalar entries, 1: block map entries, 2: flow map entries
	entryStyle := (shape >> 9) % 3

	var original, updated strings.Builder
	header := "---\n# " + comment + "\ntimeout-minutes: 5\n"
	original.WriteString(header)
	updated.WriteString(header)

	if useFlowList {
		var oldItems, newItems []string
		for _, version := range versions {
			oldItems = append(oldItems, fuzzSkillEntry(entryStyle, quote, version))
			newItems = append(newItems, fuzzSkillEntry(entryStyle, quote, bumpFuzzVersion(version)))
		}
		original.WriteString(key + ": [" + strings.Join(oldItems, ", ") + "]" + inlineComment + "\n")
		updated.WriteString(key + ": [" + strings.Join(newItems, ", ") + "]" + inlineComment + "\n")
	} else {
		original.WriteString(key + ":" + inlineComment + "\n")
		updated.WriteString(key + ":" + inlineComment + "\n")
		for _, version := range versions {
			suffix := inlineComment + "\n"
			original.WriteString(indent + "- " + fuzzSkillEntry(entryStyle, quote, version) + suffix)
			updated.WriteString(indent + "- " + fuzzSkillEntry(entryStyle, quote, bumpFuzzVersion(version)) + suffix)
			original.WriteString(indent + "# untouched: " + fuzzSkillRefValue(version) + "\n")
			updated.WriteString(indent + "# untouched: " + fuzzSkillRefValue(version) + "\n")
		}
	}

	footer := "engine: copilot\n---\n\n# Body\n\nMentions " + fuzzSkillRefValue(versions[0]) + " in markdown.\n"
	original.WriteString(footer)
	updated.WriteString(footer)
	return original.String(), updated.String()
}

func fuzzSkillRefValue(version string) string {
	return fuzzSkillRepo + "@" + version
}

// fuzzSkillEntry renders one list entry as a plain scalar, a block map value or a flow map.
func fuzzSkillEntry(style uint32, quote, version string) string {
	value := quote + fuzzSkillRefValue(version) + quote
	switch style {
	case 1:
		return "skill: " + value
	case 2:
		return "{skill: " + value + "}"
	default:
		return value
	}
}
