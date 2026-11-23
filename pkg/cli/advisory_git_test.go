package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func advisoryGitFixture(t *testing.T) (advisoryGit, func(...string) string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	g := advisoryGit{ctx: t.Context(), dir: t.TempDir()}
	run := func(args ...string) string {
		t.Helper()
		out, err := g.run(nil, args...)
		require.NoError(t, err)
		return out
	}
	run("init", "--quiet", "--template=", "-b", "main")
	run("commit", "--allow-empty", "-m", "Fixture")
	return g, run, run("rev-parse", "HEAD")
}

func advisoryGitFixturePatch(path string) []byte {
	return fmt.Appendf(nil, "diff --git a/%[1]s b/%[1]s\nnew file mode 100644\n--- /dev/null\n+++ b/%[1]s\n@@ -0,0 +1 @@\n+fixture\n", path)
}

func TestAdvisoryGitPatchScope(t *testing.T) {
	for _, path := range []string{".github/CODEOWNERS", ".github/dependabot.yml", ".GitHub/policy.txt", ".GITHUB"} {
		t.Run(path, func(t *testing.T) {
			g, _, base := advisoryGitFixture(t)
			_, err := g.patchTree(base, advisoryGitFixturePatch(path))
			require.ErrorContains(t, err, "under .github")
		})
	}
	t.Run("regular file outside .github", func(t *testing.T) {
		g, _, base := advisoryGitFixture(t)
		_, err := g.patchTree(base, advisoryGitFixturePatch(".github-notes/fixture.txt"))
		require.NoError(t, err)
	})
}

func TestAdvisoryGitStripsAmbientTokens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake Git executable uses a POSIX shell")
	}
	bin := t.TempDir()
	script := `#!/bin/sh
[ "${GH_TOKEN+x}${GITHUB_TOKEN+x}${GH_ENTERPRISE_TOKEN+x}${GITHUB_ENTERPRISE_TOKEN+x}" = "" ] || exit 1
[ "$ADVISORY_TEST_ENV" = "dummy=retained" ] || exit 2
for arg do
    [ "$arg" != 'credential.helper=!gh auth git-credential' ] || exit 0
done
exit 3
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700))
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		t.Setenv(key, "dummy-token")
	}
	t.Setenv("ADVISORY_TEST_ENV", "dummy=retained")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	g := advisoryGit{ctx: t.Context(), dir: t.TempDir()}
	_, err := g.run(nil, "--version")
	require.NoError(t, err)
}

func TestAdvisoryGitTransferRechecksBase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the local Git fixture uses a POSIX shell")
	}
	for _, tc := range []struct {
		name            string
		preexistingDest bool
	}{
		{name: "new destination"},
		{name: "already transferred", preexistingDest: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, run, base := advisoryGitFixture(t)
			advance := run("commit-tree", "HEAD^{tree}", "-p", base, "-m", "Advance base fixture")
			o := advisoryHandoff{base: base, baseBranch: "main", branch: "advisory-fix"}
			patch := advisoryGitFixturePatch("fixture.txt")
			transfer := func() error {
				return o.applyAndPush(t.Context(), t.TempDir(), "file://"+filepath.ToSlash(g.dir), patch, func() error { return nil })
			}
			var existingTip string
			if tc.preexistingDest {
				require.NoError(t, transfer())
				existingTip = run("rev-parse", o.branch)
			}

			// Simulate a concurrent main update at push time, after the pre-push check.
			realGit, err := exec.LookPath("git")
			require.NoError(t, err)
			bin := t.TempDir()
			script := `#!/bin/sh
for arg do
    if [ "$arg" = push ]; then
        "$ADVISORY_TEST_GIT" -C "$ADVISORY_TEST_REMOTE" update-ref refs/heads/main "$ADVISORY_TEST_ADVANCE" || exit 1
        break
    fi
done
exec "$ADVISORY_TEST_GIT" "$@"
`
			require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700))
			t.Setenv("ADVISORY_TEST_GIT", realGit)
			t.Setenv("ADVISORY_TEST_REMOTE", g.dir)
			t.Setenv("ADVISORY_TEST_ADVANCE", advance)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			err = transfer()
			require.ErrorContains(t, err, "base moved or could not be reconfirmed after transfer")
			require.ErrorContains(t, err, "private fix branch may already exist")
			require.Equal(t, advance, run("rev-parse", "main"))
			require.Equal(t, base, run("rev-parse", o.branch+"^"))
			require.Equal(t, "fixture", run("show", o.branch+":fixture.txt"))
			if tc.preexistingDest {
				require.Equal(t, existingTip, run("rev-parse", o.branch))
			}
		})
	}
}
