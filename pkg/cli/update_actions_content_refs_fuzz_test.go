//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/parser"
)

const (
	fuzzOldRef = "1111111111111111111111111111111111111111"
	fuzzNewRef = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func FuzzUpgradePreservesMutatedExistingWorkflows(f *testing.F) {
	addExistingWorkflowFuzzSeeds(f)

	f.Fuzz(func(t *testing.T, content string, mutation uint8) {
		if len(content) > 64*1024 || !strings.HasPrefix(content, "---\n") {
			return
		}
		parsed, err := parser.ExtractFrontmatterFromContent(content)
		if err != nil || parsed.Frontmatter == nil {
			return
		}
		for _, key := range []string{"skills", "plugins", "timeout_minutes"} {
			if _, exists := parsed.Frontmatter[key]; exists {
				return
			}
		}

		mutated, expected, fieldName, objectKey := mutateExistingWorkflowForUpgrade(content, mutation)
		mutatedFrontmatter, err := parser.ExtractFrontmatterFromContent(mutated)
		if err != nil {
			return
		}
		if _, exists := mutatedFrontmatter.Frontmatter["timeout_minutes"]; !exists {
			return
		}
		if _, ok := mutatedFrontmatter.Frontmatter[fieldName].([]any); !ok {
			return
		}
		fixed, applied, err := getTimeoutMinutesCodemod().Apply(mutated, mutatedFrontmatter.Frontmatter)
		if err != nil {
			t.Fatalf("timeout codemod failed: %v", err)
		}
		if !applied {
			t.Fatal("timeout codemod was not applied")
		}

		resolver := func(_ context.Context, _, currentRef string, _, _ bool, _ time.Duration) (string, error) {
			if currentRef != fuzzOldRef {
				t.Fatalf("resolver received unexpected ref %q", currentRef)
			}
			return fuzzNewRef, nil
		}
		changed, got, err := updateFrontmatterRepoRefsInContentWithResolver(
			context.Background(), fixed, fieldName, objectKey, true, false, 0, resolver,
		)
		if err != nil {
			t.Fatalf("reference update failed: %v", err)
		}
		if !changed {
			t.Fatal("reference update was not applied")
		}
		if got != expected {
			t.Fatalf("upgrade changed bytes outside targeted fields\n--- got ---\n%s\n--- want ---\n%s", got, expected)
		}
	})
}

func addExistingWorkflowFuzzSeeds(f *testing.F) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		f.Fatal("unable to locate fuzz test source")
	}
	workflowPaths, err := filepath.Glob(filepath.Join(filepath.Dir(currentFile), "..", "..", ".github", "workflows", "*.md"))
	if err != nil {
		f.Fatalf("glob workflows: %v", err)
	}

	added := 0
	for _, workflowPath := range workflowPaths {
		content, readErr := os.ReadFile(workflowPath)
		if readErr != nil {
			f.Fatalf("read workflow %s: %v", workflowPath, readErr)
		}
		if len(content) > 20*1024 {
			continue
		}
		parsed, parseErr := parser.ExtractFrontmatterFromContent(string(content))
		if parseErr != nil {
			continue
		}
		conflicts := false
		for _, key := range []string{"skills", "plugins", "timeout_minutes"} {
			if _, exists := parsed.Frontmatter[key]; exists {
				conflicts = true
			}
		}
		if conflicts {
			continue
		}

		f.Add(string(content), uint8(added))
		added++
		if added == 32 {
			break
		}
	}
	if added < 16 {
		f.Fatalf("found only %d suitable existing workflow fuzz seeds", added)
	}
}

func mutateExistingWorkflowForUpgrade(content string, mutation uint8) (mutated, expected, fieldName, objectKey string) {
	indent := "  "
	if mutation&1 != 0 {
		indent = "    "
	}
	quote := "'"
	if mutation&2 != 0 {
		quote = `"`
	}
	blank := ""
	if mutation&4 != 0 {
		blank = "\n"
	}

	fieldName = "skills"
	objectKey = "skill"
	repoPath := "githubnext/skills/review/security@"
	itemPrefix := "- skill: "
	if mutation&8 != 0 {
		fieldName = "plugins"
		objectKey = noObjectKey
		repoPath = "githubnext/plugins/review/security@"
		itemPrefix = "- "
	}

	oldRef := repoPath + fuzzOldRef
	newRef := repoPath + fuzzNewRef
	timeoutMutation := "timeout_minutes: 30 # slightly outdated spelling\n"
	if strings.Contains(content, "\ntimeout-minutes:") {
		content = strings.Replace(content, "\ntimeout-minutes:", "\ntimeout_minutes:", 1)
		timeoutMutation = ""
	}
	inserted := "# fuzzed copy: preserve this comment\n" +
		timeoutMutation +
		blank +
		fieldName + ": # preserve list formatting\n" +
		indent + itemPrefix + quote + oldRef + quote + " # update only this value\n" +
		indent + "# unchanged reference in a comment: " + oldRef + "\n" +
		"# preserve quoted punctuation: \"value: # text\"\n" +
		blank

	mutated = "---\n" + inserted + strings.TrimPrefix(content, "---\n")
	expected = replaceFirstFrontmatterToken(mutated, "timeout_minutes:", "timeout-minutes:")
	expected = replaceFirstFrontmatterToken(expected, oldRef, newRef)
	return mutated, expected, fieldName, objectKey
}

func replaceFirstFrontmatterToken(content, oldValue, newValue string) string {
	firstNewline := strings.IndexByte(content, '\n')
	if firstNewline < 0 {
		panic("frontmatter opening line not found")
	}
	frontmatterStart := firstNewline + 1
	offset := strings.Index(content[frontmatterStart:], oldValue)
	if offset < 0 {
		panic("frontmatter mutation token not found")
	}
	offset += frontmatterStart
	return content[:offset] + newValue + content[offset+len(oldValue):]
}
