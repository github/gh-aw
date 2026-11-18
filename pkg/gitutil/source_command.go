package gitutil

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/github/gh-aw/pkg/errorutil"
)

// SourceCommandContext constructs Git commands for disposable source downloads.
// Use it for every operation on those clones, not for development checkouts.
// Only HTTPS transport and host-scoped gh credentials are supported; user Git
// configuration, hooks, filters, credential helpers and interactive prompts are disabled.
func SourceCommandContext(ctx context.Context, remoteURL string, args ...string) *exec.Cmd {
	cmd, _ := sourceCommandContext(ctx, remoteURL, true, args...)
	return cmd
}

// SourceCommandOutputContext runs an isolated source-download Git command and
// retries without credentials if the remote rejects the configured token.
func SourceCommandOutputContext(ctx context.Context, remoteURL string, args ...string) ([]byte, error) {
	cmd, hasCredentials := sourceCommandContext(ctx, remoteURL, true, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err == nil || !hasCredentials || !isSourceAuthenticationError(stderr.String()) {
		return output, err
	}

	anonymousCmd, _ := sourceCommandContext(ctx, remoteURL, false, args...)
	return anonymousCmd.Output()
}

// SourceCommandCombinedOutputContext runs an isolated source-download Git
// command and retries without credentials if the remote rejects the token.
func SourceCommandCombinedOutputContext(ctx context.Context, remoteURL string, args ...string) ([]byte, error) {
	cmd, hasCredentials := sourceCommandContext(ctx, remoteURL, true, args...)
	output, err := cmd.CombinedOutput()
	if err == nil || !hasCredentials || !isSourceAuthenticationError(string(output)) {
		return output, err
	}

	anonymousCmd, _ := sourceCommandContext(ctx, remoteURL, false, args...)
	return anonymousCmd.CombinedOutput()
}

func sourceCommandContext(ctx context.Context, remoteURL string, includeCredentials bool, args ...string) (*exec.Cmd, bool) {
	base := []string{"--no-pager", "--no-replace-objects",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "init.templateDir=",
		"-c", "credential.helper=",
		"-c", "core.askPass=",
		"-c", "protocol.allow=never",
		"-c", "protocol.https.allow=always",
		"-c", "http.followRedirects=initial",
		"-c", "maintenance.auto=false",
		"-c", "gc.auto=0",
		"-c", "submodule.recurse=false",
	}
	cmdDir := sourceCommandWorkingDirectory()
	gitPath := trustedGitPath()
	cmd := exec.CommandContext(ctx, gitPath, append(base, args...)...)
	cmd.Dir = cmdDir
	cmd.Env = sourceCommandEnvironment(cmdDir)
	if gitExecPath := trustedGitExecPath(gitPath, cmdDir, cmd.Env); gitExecPath != "" {
		cmd.Env = append(cmd.Env, "GIT_EXEC_PATH="+gitExecPath)
	}
	hasCredentials := false
	if includeCredentials {
		if remote, err := url.Parse(remoteURL); err == nil && remote.Scheme == "https" && remote.Hostname() != "" {
			if token, _ := auth.TokenForHost(remote.Hostname()); token != "" {
				// Keep credentials out of argv, clone URLs and on-disk configuration.
				header := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
				cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1",
					"GIT_CONFIG_KEY_0=http."+remoteURL+".extraheader",
					"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+header)
				hasCredentials = true
			}
		}
	}
	return cmd, hasCredentials
}

func sourceCommandEnvironment(cmdDir string) []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		upperKey := strings.ToUpper(key)
		if !strings.HasPrefix(upperKey, "GIT_") && !strings.EqualFold(key, "SSH_ASKPASS") {
			env = append(env, value)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CEILING_DIRECTORIES="+cmdDir, "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1")
}

func sourceCommandWorkingDirectory() string {
	dir, err := filepath.Abs(os.TempDir())
	if err == nil {
		dir, err = filepath.EvalSymlinks(dir)
	}
	if err != nil {
		return filepath.Clean(string(filepath.Separator))
	}

	safeDir := dir
	for current := dir; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			safeDir = filepath.Dir(current)
		} else if !os.IsNotExist(err) {
			return filepath.Clean(string(filepath.Separator))
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return safeDir
}

func trustedGitPath() string {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "git"
	}
	if !filepath.IsAbs(gitPath) {
		if absolutePath, absErr := filepath.Abs(gitPath); absErr == nil {
			return absolutePath
		}
	}
	return gitPath
}

func trustedGitExecPath(gitPath, cmdDir string, env []string) string {
	cmd := exec.Command(gitPath, "--exec-path")
	cmd.Dir = cmdDir
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	execPath := strings.TrimSpace(string(output))
	if execPath == "" {
		return ""
	}
	if !filepath.IsAbs(execPath) {
		execPath = filepath.Join(cmdDir, execPath)
	}
	execPath, err = filepath.Abs(execPath)
	if err != nil {
		return ""
	}
	info, err := os.Stat(execPath)
	if err != nil || !info.IsDir() {
		return ""
	}
	return execPath
}

func isSourceAuthenticationError(output string) bool {
	lowerOutput := strings.ToLower(output)
	return errorutil.IsAuthError(output) ||
		strings.Contains(lowerOutput, "returned error: 401") ||
		strings.Contains(lowerOutput, "returned error: 403") ||
		strings.Contains(lowerOutput, "could not read username") ||
		strings.Contains(lowerOutput, "authentication failed")
}

// CreateSourceTempDir makes an absolute directory for disposable source downloads.
func CreateSourceTempDir(pattern string) (string, error) {
	tmpDir, err := os.MkdirTemp("", pattern)
	if err != nil {
		return "", err
	}
	absolutePath, err := filepath.Abs(tmpDir)
	if err != nil {
		if cleanupErr := os.RemoveAll(tmpDir); cleanupErr != nil {
			return "", fmt.Errorf(
				"failed to resolve source temp directory: %w (cleanup failed: %w)",
				err,
				cleanupErr,
			)
		}
		return "", err
	}
	return absolutePath, nil
}
