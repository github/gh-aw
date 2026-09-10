//go:build !integration

package cli

import (
	"strings"
	"testing"
)

func FuzzNormalizeFrontmatter(f *testing.F) {
	f.Add("---\nengine: copilot\non: workflow_dispatch\n---\n# Body")
	f.Add("---\ndescription: |\n  before\n  ---\n  after\n---\nMarkdown")
	f.Add("---\n000000000: |\n#00\n---")
	f.Add("---\n0: |\n\n\n 00\n---")
	f.Add("---\ninvalid: [\n---")
	f.Add("# No frontmatter")

	f.Fuzz(func(t *testing.T, content string) {
		formatted, err := normalizeFrontmatter(content)
		if err != nil {
			return
		}

		_, originalSuffix, err := splitFrontmatterForFormatting(content)
		if err != nil {
			t.Fatalf("successfully formatted content could not be split: %v", err)
		}
		_, formattedSuffix, err := splitFrontmatterForFormatting(formatted)
		if err != nil {
			t.Fatalf("formatted content could not be split: %v", err)
		}
		if formattedSuffix != originalSuffix {
			t.Fatalf("Markdown content changed during formatting")
		}

		reformatted, err := normalizeFrontmatter(formatted)
		if err != nil {
			t.Fatalf("formatted content could not be formatted again: %v", err)
		}
		if reformatted != formatted {
			t.Fatalf("formatting is not idempotent:\nfirst: %q\nsecond: %q", formatted, reformatted)
		}
	})
}

func FuzzSplitFrontmatterForFormatting(f *testing.F) {
	f.Add("engine: copilot\n", "\n# Body")
	f.Add("description: |\n  before\n  ---\n  after\n", "\nMarkdown")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, frontmatter, suffix string) {
		frontmatter = strings.ReplaceAll(frontmatter, "\r", "")
		if frontmatter != "" && !strings.HasSuffix(frontmatter, "\n") {
			frontmatter += "\n"
		}
		content := "---\n" + frontmatter + "---\n" + suffix
		extracted, extractedSuffix, err := splitFrontmatterForFormatting(content)
		if err != nil {
			t.Fatalf("constructed frontmatter could not be split: %v", err)
		}
		reconstructed := "---\n" + extracted + "---" + extractedSuffix
		if reconstructed != content {
			t.Fatalf("split content did not reconstruct input")
		}
		if !strings.HasPrefix(reconstructed, "---\n") {
			t.Fatalf("reconstructed content lost opening delimiter")
		}
	})
}
