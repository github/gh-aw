//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestVerifyGitCredentials(t *testing.T) {
	script, err := filepath.Abs("../../actions/setup/sh/verify_git_credentials.sh")
	require.NoError(t, err)
	tests := []struct {
		name, config string
		wantError    bool
	}{
		{"clean", "[remote \"origin\"]\nurl = https://github.com/example/repo\n", false},
		{"helper", "[credential]\nhelper = store\n", true},
		{"scoped-helper", "[credential \"https://github.com\"]\nhelper = store\n", true},
		{"header", "[http \"https://github.com/\"]\nextraheader = Authorization: dummy-token\n", true},
		{"url", "[remote \"origin\"]\nurl = https://dummy-token@github.com/example/repo\n", true},
		{"push-url", "[remote \"origin\"]\npushurl = https://dummy-token@github.com/example/repo\n", true},
		{"rewrite", "[url \"https://dummy-token@github.com/\"]\ninsteadOf = https://github.com/\n", true},
		{"invalid", "[broken\n", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "config")
			require.NoError(t, os.WriteFile(config, []byte(tc.config), 0600))
			output, err := exec.Command("bash", script, config).CombinedOutput()
			if tc.wantError {
				require.Error(t, err, "must reject residual credentials or unverifiable configuration")
				require.Contains(t, string(output), "ERROR:")
			} else {
				require.NoError(t, err, "%s", output)
			}
			require.NotContains(t, string(output), "dummy-token", "verification must not log credentials")
		})
	}
}

func TestVerifyGitCredentialsIncludes(t *testing.T) {
	dir := t.TempDir()
	included := filepath.Join(dir, "included")
	require.NoError(t, os.WriteFile(included, []byte("[credential]\nhelper = store\n"), 0600))
	config := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(config, []byte("[include]\npath = "+included+"\n"), 0600))
	output, err := exec.Command("bash", "../../actions/setup/sh/verify_git_credentials.sh", config).CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "Git credentials remain")
}

func TestVerifyGitCredentialsConditionalIncludes(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repository")
	require.NoError(t, exec.Command("git", "init", "--quiet", repo).Run())
	included := filepath.Join(dir, "included")
	require.NoError(t, os.WriteFile(included, []byte("[credential]\nhelper = store\n"), 0600))
	config := filepath.Join(repo, ".git", "config")
	gitdir, err := filepath.EvalSymlinks(filepath.Join(repo, ".git"))
	require.NoError(t, err)
	content := "\n[includeIf \"gitdir:" + filepath.ToSlash(gitdir) + "\"]\npath = " + included + "\n"
	file, err := os.OpenFile(config, os.O_APPEND|os.O_WRONLY, 0600)
	require.NoError(t, err)
	_, err = file.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	cmd := exec.Command("bash", "../../actions/setup/sh/verify_git_credentials.sh", config)
	cmd.Env = append(os.Environ(), "RUNNER_TEMP="+dir)
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "verification must use each checkout's gitdir context")
	require.Contains(t, string(output), "Git credentials remain")
	snapshots, err := filepath.Glob(filepath.Join(dir, "gh-aw-git-config.*"))
	require.NoError(t, err)
	require.Empty(t, snapshots, "credential-bearing verification snapshots must be removed")
}

func TestVerifyGitCredentialsNoCheckout(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "find"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte("#!/usr/bin/env bash\nexit 99\n"), 0755))
	cmd := exec.Command("bash", "../../actions/setup/sh/verify_git_credentials.sh")
	cmd.Env = append(os.Environ(), "GITHUB_WORKSPACE="+dir,
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "no checkout must remain a no-op even without usable git: %s", output)
}

func TestCheckoutCleanupFailsClosed(t *testing.T) {
	var generated struct {
		Steps []struct {
			Run             string `yaml:"run"`
			ContinueOnError bool   `yaml:"continue-on-error"`
		} `yaml:"steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+generateCheckoutCredentialsCleanupStep()), &generated))
	require.Len(t, generated.Steps, 1)
	require.False(t, generated.Steps[0].ContinueOnError)
	for _, helper := range []string{
		"#!/usr/bin/env bash\nexit 7\n",
		"#!/usr/bin/env bash\nexit 0\n",
		"#!/usr/bin/env bash\ngit config --file \"$GITHUB_WORKSPACE/.git/config\" --unset-all http.extraheader\n",
	} {
		testCheckoutCleanup(t, generated.Steps[0].Run, helper)
	}
}

func testCheckoutCleanup(t *testing.T, run, helper string) {
	t.Helper()
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, ".git"), 0755))
	config := filepath.Join(workspace, ".git", "config")
	require.NoError(t, os.WriteFile(config, []byte("[http]\nextraheader = Authorization: dummy-token\n"), 0600))
	actions := filepath.Join(dir, "gh-aw", "actions")
	require.NoError(t, os.MkdirAll(actions, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(actions, "clean_git_credentials_checkout.sh"), []byte(helper), 0755))
	verify, err := os.ReadFile("../../actions/setup/sh/verify_git_credentials.sh")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(actions, "verify_git_credentials.sh"), verify, 0755))
	path := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(path, 0755))
	find, err := exec.LookPath("find")
	require.NoError(t, err)
	// Restrict the runtime's /tmp scan to the test workspace on shared machines.
	wrapper := "#!/usr/bin/env bash\nexec \"$GH_AW_TEST_FIND\" \"$GITHUB_WORKSPACE\" -type f -path '*/.git/config' -print0\n"
	require.NoError(t, os.WriteFile(filepath.Join(path, "find"), []byte(wrapper), 0755))
	cmd := exec.Command("bash", "-e", "-c", run+"\nprintf started >\"$RUNNER_TEMP/agent-started\"")
	cmd.Env = append(os.Environ(), "RUNNER_TEMP="+dir, "GITHUB_WORKSPACE="+workspace,
		"GH_AW_TEST_FIND="+find, "PATH="+path+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	if strings.Contains(helper, "--unset-all") {
		require.NoError(t, err, "%s", output)
		require.FileExists(t, filepath.Join(dir, "agent-started"))
	} else {
		require.Error(t, err, "both failed cleanup and false-success cleanup must block execution")
		require.NoFileExists(t, filepath.Join(dir, "agent-started"))
	}
	require.NotContains(t, string(output), "dummy-token")
}
