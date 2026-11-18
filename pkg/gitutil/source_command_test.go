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
	"strings"
	"sync/atomic"
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
	var rejectedAuthenticatedRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/redirect.git/") {
			redirectPath := strings.Replace(r.URL.Path, "/redirect.git/", "/repo.git/", 1)
			if r.URL.RawQuery != "" {
				redirectPath += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, redirectPath, http.StatusMovedPermanently)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/repo.git/") {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/repo.git")
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/public/") {
			if r.Header.Get("Authorization") != "" {
				rejectedAuthenticatedRequests.Add(1)
				http.Error(w, "authentication rejected", http.StatusUnauthorized)
				return
			}
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/public")
			files.ServeHTTP(w, r)
			return
		}
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
	execPath := filepath.Join(root, "untrusted-exec-path")
	require.NoError(t, os.MkdirAll(execPath, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(execPath, "git-remote-https"), []byte("#!/bin/sh\nprintf executed > '"+filepath.ToSlash(marker)+"'\nexit 1\n"), 0700))
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
	t.Setenv("GIT_EXEC_PATH", execPath)
	t.Setenv("GH_ENTERPRISE_TOKEN", "source-test-token")

	checkout := filepath.Join(root, "checkout")
	run("", "init", checkout)
	checkoutConfig := fmt.Sprintf("[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n\tlogallrefupdates = true\n[url \"ext::%s\"]\n\tinsteadOf = https://\n",
		filepath.ToSlash(script))
	require.NoError(t, os.WriteFile(filepath.Join(checkout, ".git", "config"), []byte(checkoutConfig), 0600))
	tempDir := filepath.Join(checkout, "tmp")
	require.NoError(t, os.MkdirAll(tempDir, 0700))
	t.Setenv("TMPDIR", tempDir)

	cmd := SourceCommandContext(ctx, server.URL, "-c", "http.sslCAInfo="+certPath, "clone", server.URL, filepath.Join(tempDir, "clone"))
	require.NotEqual(t, execPath, envValue(cmd.Env, "GIT_EXEC_PATH"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	clone := filepath.Join(tempDir, "clone")
	content, err := os.ReadFile(filepath.Join(clone, "source.md"))
	require.NoError(t, err)
	require.Equal(t, "unmodified source\n", string(content))
	require.NoFileExists(t, filepath.Join(clone, ".git", "hooks", "post-checkout"))
	require.NoFileExists(t, marker)

	redirectClone := filepath.Join(tempDir, "redirect-clone")
	output, err = SourceCommandContext(ctx, server.URL+"/redirect.git", "-c", "http.sslCAInfo="+certPath, "clone", server.URL+"/redirect.git", redirectClone).CombinedOutput()
	require.NoError(t, err, "%s", output)

	// Follow-up operations must override executable settings even in local config.
	run("", "-C", clone, "config", "core.hooksPath", hooks)
	run("", "-C", clone, "config", "core.fsmonitor", script)
	run("", "-C", clone, "checkout", "--detach", "HEAD")
	run("", "-C", clone, "status", "--porcelain")
	require.NoFileExists(t, marker)

	anonymousClone := filepath.Join(tempDir, "anonymous-clone")
	output, err = SourceCommandCombinedOutputContext(ctx, server.URL+"/public", "-c", "http.sslCAInfo="+certPath, "clone", server.URL+"/public", anonymousClone)
	require.NoError(t, err, "%s", output)
	require.Positive(t, rejectedAuthenticatedRequests.Load())
	anonymousContent, err := os.ReadFile(filepath.Join(anonymousClone, "source.md"))
	require.NoError(t, err)
	require.Equal(t, "unmodified source\n", string(anonymousContent))
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

func TestCreateSourceTempDirWithRelativeTMPDIR(t *testing.T) {
	workingDir, err := os.Getwd()
	require.NoError(t, err)
	tmpRoot := t.TempDir()
	relativeTmpRoot, err := filepath.Rel(workingDir, tmpRoot)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(relativeTmpRoot, 0700))
	t.Setenv("TMPDIR", relativeTmpRoot)

	tmpDir, err := CreateSourceTempDir("gh-aw-test-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(tmpDir))
	})
	require.True(t, filepath.IsAbs(tmpDir), "temporary directory should be absolute: %q", tmpDir)
}

func envValue(env []string, name string) string {
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}
