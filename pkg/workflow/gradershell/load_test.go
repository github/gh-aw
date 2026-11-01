package gradershell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shellTestDirectory(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".grader-shell-test-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	absolute, err := filepath.Abs(dir)
	require.NoError(t, err)
	return absolute
}

func shellTestRepository(t testing.TB) (string, string) {
	t.Helper()
	root := shellTestDirectory(t)
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o700))
	dir := filepath.Join(root, ".github", "workflows", "shared")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	workflow := filepath.Join(dir, "workflow.md")
	require.NoError(t, os.WriteFile(workflow, []byte("---\n---\n"), 0o600))
	return root, workflow
}

func TestValidRunPath(t *testing.T) {
	for _, path := range []string{"grader.sh", "scripts/grader.sh", ".github/graders/check.sh", "./check.sh", "./nested/check.sh", "a-b_2.sh"} {
		assert.True(t, ValidRunPath(path), path)
	}
	for _, path := range []string{"", "echo 1", "/check.sh", "../check.sh", "nested/../check.sh", "./../check.sh", "a//b.sh", "././b.sh", "a/./b.sh", "a.sh/", ".sh\n", "a.SH", "${{ inputs.file }}.sh", "$FILE.sh", "$(pwd)/a.sh", "a`pwd`.sh", "C:/a.sh", `a\b.sh`, "a b.sh", "~/a.sh", "*.sh"} {
		assert.False(t, ValidRunPath(path), path)
	}
}

func TestGraderShellRejectsNonLocalRunPaths(t *testing.T) {
	for _, test := range []struct{ name, path string }{
		{"absolute", "/check.sh"},
		{"network", "//example.com/check.sh"},
		{"slash backslash", `/\example.com/check.sh`},
		{"UNC", `\\example.com\check.sh`},
		{"Windows absolute", `C:\check.sh`},
		{"Windows drive relative", "C:check.sh"},
		{"backslash traversal", `nested\..\check.sh`},
		{"root traversal", "../check.sh"},
		{"nested traversal", "nested/../check.sh"},
		{"workflow traversal", "./../check.sh"},
		{"workflow absolute", ".//check.sh"},
		{"workflow network", ".///example.com/check.sh"},
		{"repeated separator", "nested//check.sh"},
		{"dot segment", "nested/./check.sh"},
		{"encoded traversal", "%2e%2e/check.sh"},
		{"URL", "https://example.com/check.sh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.False(t, ValidRunPath(test.path))
			_, err := Load("", test.path)
			require.ErrorContains(t, err, "literal relative .sh path")
		})
	}
}

func TestLoadGraderShell(t *testing.T) {
	root, workflow := shellTestRepository(t)
	rootSource := "#!/usr/bin/env bash\njq '.events | length' \"$GH_AW_SESSION\"\n"
	localSource := "#!/bin/bash\nprintf '%s\\n' 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "check.sh"), []byte(rootSource), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(workflow), "check.sh"), []byte(localSource), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "nested"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(filepath.Dir(workflow), "nested"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "check.sh"), []byte(rootSource), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(workflow), "nested", "check.sh"), []byte(localSource), 0o600))
	for _, test := range []struct{ path, want string }{
		{"check.sh", rootSource},
		{"./check.sh", localSource},
		{"nested/check.sh", rootSource},
		{"./nested/check.sh", localSource},
	} {
		source, err := Load(workflow, test.path)
		require.NoError(t, err)
		assert.Equal(t, test.want, source)
	}
	_, err := Load("", "./check.sh")
	require.ErrorContains(t, err, "declaring workflow")
}

func TestLoadGraderShellRejectsInvalidSource(t *testing.T) {
	root, workflow := shellTestRepository(t)
	for _, test := range []struct {
		name   string
		source []byte
	}{
		{"missing shebang", []byte("printf 1\n")},
		{"other interpreter", []byte("#!/bin/sh\nprintf 1\n")},
		{"other env interpreter", []byte("#!/usr/bin/env python3\nprint(1)\n")},
		{"invalid UTF-8", []byte("#!/bin/bash\n# \xff\n")},
		{"NUL", []byte("#!/bin/bash\n# \x00\n")},
		{"oversize", []byte("#!/bin/bash\n" + strings.Repeat("#", maxSourceBytes))},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "check.sh"), test.source, 0o600))
			_, err := Load(workflow, "check.sh")
			require.Error(t, err)
			assert.NotContains(t, err.Error(), string(test.source))
		})
	}
	source := "#!/bin/bash\n" + strings.Repeat("#", maxSourceBytes-len("#!/bin/bash\n"))
	require.NoError(t, os.WriteFile(filepath.Join(root, "check.sh"), []byte(source), 0o600))
	loaded, err := Load(workflow, "check.sh")
	require.NoError(t, err)
	assert.Len(t, loaded, maxSourceBytes)
	require.NoError(t, os.Mkdir(filepath.Join(root, "directory.sh"), 0o700))
	_, err = Load(workflow, "directory.sh")
	require.ErrorContains(t, err, "regular non-symlink")
	_, err = Load(workflow, "missing.sh")
	require.ErrorContains(t, err, "existing regular")
	_, err = Load(workflow, "../PRIVATE.sh")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "PRIVATE")
}

func TestLoadGraderShellSymlinkConfinement(t *testing.T) {
	root, workflow := shellTestRepository(t)
	outside := shellTestDirectory(t)
	require.NoError(t, os.WriteFile(filepath.Join(outside, "check.sh"), []byte("#!/bin/bash\nprintf 1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "real.sh"), []byte("#!/bin/bash\nprintf 1\n"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(root, "real.sh"), filepath.Join(root, "leaf.sh")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))
	require.NoError(t, os.Symlink(root, filepath.Join(filepath.Dir(workflow), "other")))
	for _, path := range []string{"leaf.sh", "escape/check.sh", "escape/missing.sh", "./other/real.sh"} {
		_, err := Load(workflow, path)
		require.Error(t, err, path)
	}
	linkedWorkflow := filepath.Join(root, "linked.md")
	require.NoError(t, os.Symlink(filepath.Join(outside, "workflow.md"), linkedWorkflow))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "workflow.md"), []byte("---\n---\n"), 0o600))
	_, err := Load(linkedWorkflow, "real.sh")
	require.ErrorContains(t, err, "declaring workflow escapes")
}

func TestLoadGraderShellNeverExecutes(t *testing.T) {
	root, workflow := shellTestRepository(t)
	marker := filepath.Join(root, "must-not-exist")
	source := "#!/bin/bash\ntouch '" + marker + "'\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "check.sh"), []byte(source), 0o600))
	loaded, err := Load(workflow, "check.sh")
	require.NoError(t, err)
	findings, err := Check(t.Context(), loaded)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	_, err = os.Stat(marker)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestGraderShellBashShebang(t *testing.T) {
	for _, shebang := range []string{"#!/bin/bash", "#!/usr/bin/bash", "#!/usr/local/bin/bash -eu", "#!/usr/bin/env bash", "#!/usr/bin/env -S bash -euo pipefail"} {
		assert.True(t, bashShebang(shebang), shebang)
	}
	for _, shebang := range []string{"#!/bin/sh", "#!/usr/bin/env sh", "#!/usr/bin/env bash -e", "#!/bash-wrapper", " #!/bin/bash", ""} {
		assert.False(t, bashShebang(shebang), shebang)
	}
}
