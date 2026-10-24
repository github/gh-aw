//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchContainerDigestNeverPullsImages(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executables use a POSIX shell")
	}
	for _, resolver := range []string{"buildx", "crane", "unavailable"} {
		t.Run(resolver, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "commands.log")
			t.Setenv("PATH", dir)
			t.Setenv("DIGEST_COMMAND_LOG", logPath)
			t.Setenv("DIGEST_RESOLVER", resolver)
			digest := "sha256:" + strings.Repeat("a", 64)
			t.Setenv("EXPECTED_DIGEST", digest)
			const docker = `#!/bin/sh
printf 'docker %s\n' "$*" >> "$DIGEST_COMMAND_LOG"
if [ "$1 $2 $3" = "buildx imagetools inspect" ] && [ "$DIGEST_RESOLVER" = buildx ]; then
  printf 'Digest: %s\n' "$EXPECTED_DIGEST"
  exit 0
fi
printf 'metadata lookup unavailable\n' >&2
exit 1
`
			const crane = `#!/bin/sh
printf 'crane %s\n' "$*" >> "$DIGEST_COMMAND_LOG"
if [ "$1" = digest ] && [ "$DIGEST_RESOLVER" = crane ]; then
  printf '%s\n' "$EXPECTED_DIGEST"
  exit 0
fi
printf 'metadata lookup unavailable\n' >&2
exit 1
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(docker), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "crane"), []byte(crane), 0o755))
			const image = "ghcr.io/example/agent:v1"
			got, err := fetchContainerDigest(context.Background(), image, true)
			if resolver == "unavailable" {
				require.ErrorContains(t, err, "without downloading image layers")
				require.Empty(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, digest, got)
			}
			commands, err := os.ReadFile(logPath)
			require.NoError(t, err)
			want := "docker buildx imagetools inspect " + image + "\n"
			if resolver != "buildx" {
				want += "crane digest " + image + "\n"
			}
			require.Equal(t, want, string(commands), "only registry metadata commands may run")
		})
	}
}
