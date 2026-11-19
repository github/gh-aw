package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestAuthorizationDependencyProvenance(t *testing.T) {
	for _, test := range []struct {
		name, extra, body, dependency, content string
	}{
		{
			name: "section import", extra: "imports:\n  - shared/policy.md#Tools\n",
			dependency: ".github/workflows/shared/policy.md",
			content:    "---\ntools:\n  bash: [echo]\n---\n## Tools\nUse the configured tools.\n",
		},
		{
			name: "sibling include", body: "\n@include ../shared/policy.md\n",
			dependency: ".github/shared/policy.md", content: "# Included policy\nDo nothing.\n",
		},
		{
			name: "nested import", extra: "imports:\n  - shared/policy.md\n",
			dependency: ".github/workflows/shared/policy.md",
			content:    "---\nimports:\n  - ./nested.md\n---\nShared policy.\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := strings.Replace(authorizationSourceFixture, "on: workflow_dispatch\n", "on: workflow_dispatch\n"+test.extra, 1) + test.body
			file := filepath.Join(root, ".github/workflows/triage.md")
			writeAuthorizationTestFile(t, file, source)
			writeAuthorizationTestFile(t, filepath.Join(root, test.dependency), test.content)
			// This display-relative location must never shadow the real sibling include.
			writeAuthorizationTestFile(t, filepath.Join(root, ".github/workflows/.github/shared/policy.md"), "WRONG FILE\n")
			const nestedContent = "---\ntools:\n  bash: [echo]\n---\nNested policy.\n"
			if test.name == "nested import" {
				writeAuthorizationTestFile(t, filepath.Join(root, ".github/workflows/shared/nested.md"), nestedContent)
			}
			data, generated, err := workflow.NewAuthorizationVerificationCompiler(root).CompileAuthorizationSnapshot(file)
			require.NoError(t, err)
			writeAuthorizationTestFile(t, filepath.Join(root, ".github/workflows/triage.lock.yml"), generated)
			result := &AuthorizationPolicyResult{}
			verifyAuthorizationSnapshot(context.Background(), root, mustParseAuthorizationPolicy(t), result, nil)
			require.Empty(t, result.Findings)
			require.Len(t, result.Workflows, 1)
			entry := result.Workflows[0]
			require.Equal(t, authorizationDigest([]byte(test.content)), entry.Imports[test.dependency])
			if test.name == "nested import" {
				require.Equal(t, authorizationDigest([]byte(nestedContent)), entry.Imports[".github/workflows/shared/nested.md"])
			}
			require.NotEmpty(t, data.ResolvedDependencyFiles)
			require.NotContains(t, entry.Imports, ".github/workflows/.github/shared/policy.md")
		})
	}
}
