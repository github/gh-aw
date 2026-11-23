//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func TestAdvisoryGitBundleRoundTrip(t *testing.T) {
	upstream, upstreamGit, older := advisoryGitFixture(t)
	upstreamGit("commit", "--allow-empty", "-m", "Tested base")
	base := upstreamGit("rev-parse", "HEAD")

	producer := advisoryGit{ctx: t.Context(), dir: t.TempDir()}
	receiver := advisoryGit{ctx: t.Context(), dir: t.TempDir()}
	run := func(repo advisoryGit, args ...string) string {
		t.Helper()
		output, err := repo.run(nil, args...)
		require.NoError(t, err)
		return output
	}
	run(producer, "clone", "--no-local", "--depth=1", "--branch", "main", upstream.dir, ".")
	require.Equal(t, "true", run(producer, "rev-parse", "--is-shallow-repository"))
	run(producer, "switch", "-c", "advisory-fix")
	require.NoError(t, os.WriteFile(filepath.Join(producer.dir, "fixture.bin"), []byte{0, 1, 2, 255}, constants.FilePermSensitive))
	run(producer, "add", "fixture.bin")
	run(producer, "commit", "-m", "Fix fixture")
	tip := run(producer, "rev-parse", "HEAD")
	bundle := filepath.Join(t.TempDir(), "fix.bundle")
	run(producer, "bundle", "create", bundle, base+"..refs/heads/advisory-fix")

	run(receiver, "init", "--quiet", "--template=", "-b", "main")
	_, err := receiver.run(nil, "bundle", "verify", bundle)
	require.Error(t, err, "the bundle must require the tested base")
	run(receiver, "fetch", "--depth=1", upstream.dir, "refs/heads/main:refs/heads/tested-base")
	require.Equal(t, "true", run(receiver, "rev-parse", "--is-shallow-repository"))
	require.Equal(t, base, run(receiver, "rev-parse", "refs/heads/tested-base"))
	run(receiver, "bundle", "verify", bundle)
	run(receiver, "fetch", bundle, "refs/heads/advisory-fix:refs/heads/advisory-fix")

	require.Equal(t, tip, run(receiver, "rev-parse", "refs/heads/advisory-fix"))
	require.Equal(t, base, run(receiver, "rev-parse", "refs/heads/advisory-fix^"))
	require.Equal(t, run(producer, "rev-parse", "HEAD:fixture.bin"), run(receiver, "rev-parse", "refs/heads/advisory-fix:fixture.bin"))
	_, err = receiver.run(nil, "cat-file", "-e", older+"^{commit}")
	require.Error(t, err, "the bundle must not bring in upstream history")
}
