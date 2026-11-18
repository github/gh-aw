package gitutil

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/cli/go-gh/v2/pkg/auth"
)

// SourceCommandContext constructs Git commands for disposable source downloads.
// Use it for every operation on those clones, not for development checkouts.
// Only HTTPS transport and host-scoped gh credentials are supported; user Git
// configuration, hooks, filters, credential helpers and interactive prompts are disabled.
func SourceCommandContext(ctx context.Context, remoteURL string, args ...string) *exec.Cmd {
	base := []string{"--no-pager", "--no-replace-objects",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "init.templateDir=",
		"-c", "credential.helper=",
		"-c", "core.askPass=",
		"-c", "protocol.allow=never",
		"-c", "protocol.https.allow=always",
		"-c", "http.followRedirects=false",
		"-c", "maintenance.auto=false",
		"-c", "gc.auto=0",
		"-c", "submodule.recurse=false",
	}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	// Do not discover configuration in the caller's checkout.
	cmd.Dir = os.TempDir()
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		// Relocatable Git installations need their bundled HTTPS transport.
		if (!strings.HasPrefix(strings.ToUpper(key), "GIT_") || key == "GIT_EXEC_PATH") && key != "SSH_ASKPASS" {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1")
	if remote, err := url.Parse(remoteURL); err == nil && remote.Scheme == "https" && remote.Hostname() != "" {
		if token, _ := auth.TokenForHost(remote.Hostname()); token != "" {
			// Keep credentials out of argv, clone URLs and on-disk configuration.
			header := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
			cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=http."+remoteURL+".extraheader",
				"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+header)
		}
	}
	return cmd
}
