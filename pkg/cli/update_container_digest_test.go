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

	"github.com/stretchr/testify/require"
)

func TestFetchContainerDigestFailureReasons(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executables use a POSIX shell")
	}
	for _, tc := range []struct {
		name   string
		output string
		reason string
	}{
		{"access denied", "failed to fetch oauth token: 403 Forbidden", "registry access denied (HTTP 403)"},
		{"authentication required", "unexpected status: 401 Unauthorized", "registry authentication required (HTTP 401)"},
		{"lowercase forbidden", "unexpected status: 403 forbidden", "registry access denied (HTTP 403)"},
		{"registry denied", "denied: requested access to the resource is denied", "registry access denied"},
		{"registry DENIED", "DENIED: requested access to the resource is denied", "registry access denied"},
		{"registry unauthorized", "UNAUTHORIZED: authentication required", "registry authentication required"},
		{"registry forbidden", "GET https://ghcr.io/v2/example: FORBIDDEN: access forbidden", "registry access denied"},
		{"local permission error", "open /home/user/.docker/config.json: permission denied", "registry metadata lookup failed"},
		{"unrelated forbidden text", "unknown flag: --forbidden", "registry metadata lookup failed"},
		{"network failure", "dial tcp: connection refused", "registry metadata lookup failed"},
		{"tools missing", "", "Docker Buildx and crane are unavailable"},
		{"buildx plugin missing", "docker: 'buildx' is not a docker command.", "Docker Buildx and crane are unavailable"},
		{"unknown buildx command", `unknown command "buildx" for "docker"`, "Docker Buildx and crane are unavailable"},
		{"unknown docker buildx command", "docker: unknown command: docker buildx", "Docker Buildx and crane are unavailable"},
	} {
		for _, resolver := range []string{"docker", "crane"} {
			if resolver == "crane" && strings.Contains(tc.output, "buildx") {
				continue
			}
			t.Run(tc.name+"/"+resolver, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("PATH", dir)
				if tc.output != "" {
					t.Setenv("REGISTRY_ERROR", tc.output)
					const script = "#!/bin/sh\nprintf '%s\\n' \"$REGISTRY_ERROR\" >&2\nexit 1\n"
					require.NoError(t, os.WriteFile(filepath.Join(dir, resolver), []byte(script), 0o755))
				}
				digest, err := fetchContainerDigest(context.Background(), "ghcr.io/example/agent:v1", false)
				require.Empty(t, digest)
				var resolutionErr *containerDigestResolutionError
				require.ErrorAs(t, err, &resolutionErr)
				require.Equal(t, tc.reason, resolutionErr.reason)
				require.Contains(t, err.Error(), "("+tc.reason+")", "include the category in debug diagnostics")
				if tc.output != "" {
					require.Contains(t, err.Error(), tc.output, "retain full diagnostics for debug logging")
				}
			})
		}
	}
}

func TestFetchContainerDigestMixedFailureReasons(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executables use a POSIX shell")
	}
	for _, tc := range []struct {
		name   string
		buildx string
		crane  string
		reason string
	}{
		{"denial before authentication", "403 Forbidden", "401 Unauthorized", "registry access denied (HTTP 403)"},
		{"authentication before denial", "401 Unauthorized", "403 Forbidden", "registry authentication required (HTTP 401)"},
		{"denial before network failure", "403 Forbidden", "dial tcp: connection refused", "registry access denied (HTTP 403)"},
		{"network failure before denial", "dial tcp: connection refused", "403 Forbidden", "registry access denied (HTTP 403)"},
		{"missing plugin before denial", "docker: unknown command: docker buildx", "DENIED: access denied", "registry access denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir)
			t.Setenv("BUILDX_ERROR", tc.buildx)
			t.Setenv("CRANE_ERROR", tc.crane)
			for resolver, variable := range map[string]string{"docker": "BUILDX_ERROR", "crane": "CRANE_ERROR"} {
				script := "#!/bin/sh\nprintf '%s\\n' \"$" + variable + "\" >&2\nexit 1\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, resolver), []byte(script), 0o755))
			}
			digest, err := fetchContainerDigest(context.Background(), "ghcr.io/example/agent:v1", false)
			require.Empty(t, digest)
			var resolutionErr *containerDigestResolutionError
			require.ErrorAs(t, err, &resolutionErr)
			require.Equal(t, tc.reason, resolutionErr.reason)
			require.Contains(t, err.Error(), tc.buildx)
			require.Contains(t, err.Error(), tc.crane)
		})
	}
}

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

func TestFetchContainerDigestInterrupted(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		var ctx context.Context
		var cancel context.CancelFunc
		if deadline {
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		} else {
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
		}
		digest, err := fetchContainerDigest(ctx, "ghcr.io/example/agent:v1", false)
		cancel()
		require.Empty(t, digest)
		require.ErrorIs(t, err, ctx.Err())
	}
}
