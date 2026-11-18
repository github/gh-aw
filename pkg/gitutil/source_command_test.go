//go:build !integration

package gitutil

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceCommandContextClone(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("GH_CONFIG_DIR", root)
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		t.Setenv(key, "")
	}
	run := func(remote string, args ...string) []byte {
		t.Helper()
		cmd := SourceCommandContext(ctx, remote, args...)
		require.NotContains(t, fmt.Sprint(cmd.Args), "source-test-token")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return output
	}

	source := filepath.Join(root, "repo.git")
	run("", "init", source)
	require.NoError(t, os.WriteFile(filepath.Join(source, ".gitattributes"), []byte("*.md filter=hostile\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "source.md"), []byte("unmodified source\n"), 0600))
	run("", "-C", source, "add", ".")
	run("", "-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "source")
	run("", "-C", source, "update-server-info")

	files := http.FileServer(http.Dir(filepath.Join(source, ".git")))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "x-access-token" || password != "source-test-token" {
			w.Header().Set("WWW-Authenticate", `Basic realm="source"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer server.Close()
	certPath := filepath.Join(root, "ca.pem")
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	require.NoError(t, os.WriteFile(certPath, cert, 0600))

	marker := filepath.Join(root, "executed")
	script := filepath.Join(root, "execute")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nprintf executed > '"+filepath.ToSlash(marker)+"'\nexit 1\n"), 0700))
	template := filepath.Join(root, "template")
	hooks := filepath.Join(template, "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\nexec '"+filepath.ToSlash(script)+"'\n"), 0700))
	config := filepath.Join(root, ".gitconfig")
	configText := fmt.Sprintf("[core]\n\thooksPath = %q\n\tfsmonitor = %q\n[init]\n\ttemplateDir = %q\n[filter \"hostile\"]\n\tsmudge = %q\n\trequired = true\n[credential]\n\thelper = !%s\n[url \"ext::%s\"]\n\tinsteadOf = https://\n",
		filepath.ToSlash(hooks), filepath.ToSlash(script), filepath.ToSlash(template),
		filepath.ToSlash(script), filepath.ToSlash(script), filepath.ToSlash(script))
	require.NoError(t, os.WriteFile(config, []byte(configText), 0600))
	t.Setenv("HOME", root)
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_TEMPLATE_DIR", template)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hooks)
	t.Setenv("GIT_CONFIG_PARAMETERS", "'protocol.https.allow=never'")
	t.Setenv("GH_ENTERPRISE_TOKEN", "source-test-token")

	clone := filepath.Join(root, "clone")
	run(server.URL, "-c", "http.sslCAInfo="+certPath, "clone", server.URL, clone)
	content, err := os.ReadFile(filepath.Join(clone, "source.md"))
	require.NoError(t, err)
	require.Equal(t, "unmodified source\n", string(content))
	require.NoFileExists(t, filepath.Join(clone, ".git", "hooks", "post-checkout"))

	// Follow-up operations must override executable settings even in local config.
	run("", "-C", clone, "config", "core.hooksPath", hooks)
	run("", "-C", clone, "config", "core.fsmonitor", script)
	run("", "-C", clone, "checkout", "--detach", "HEAD")
	run("", "-C", clone, "status", "--porcelain")
	require.NoFileExists(t, marker)
}

func TestSourceCommandContextRejectsExecutableTransports(t *testing.T) {
	for _, remote := range []string{"ext::echo unexpected", "ssh://example.com/repo", "file:///nonexistent"} {
		t.Run(remote, func(t *testing.T) {
			output, err := SourceCommandContext(context.Background(), remote, "ls-remote", remote).CombinedOutput()
			require.Error(t, err)
			require.Contains(t, string(output), "not allowed")
		})
	}
}
