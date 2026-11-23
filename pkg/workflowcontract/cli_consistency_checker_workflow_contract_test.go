//go:build !integration

package workflowcontract

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCLIConsistencyCheckerHelpCollection(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "cli-consistency-checker.md"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	start := strings.Index(source, `      for i in "${!command_paths[@]}"; do`)
	if start < 0 {
		t.Fatal("help collection loop not found")
	}
	end := strings.Index(source[start:], "\n      done")
	if end < 0 {
		t.Fatal("end of help collection loop not found")
	}
	loop := source[start : start+end+len("\n      done")]
	loop = strings.ReplaceAll(loop, "./gh-aw", "fake_gh_aw")

	run := func(t *testing.T, paths ...string) (string, error) {
		t.Helper()
		dir := t.TempDir()
		script := `set -euo pipefail
output_dir="$1"
shift
command_paths=("$@")
fake_gh_aw() { printf '%s\n' "$*"; }
` + loop
		args := append([]string{"-c", script, "help-collector", dir}, paths...)
		err := exec.Command("bash", args...).Run()
		return dir, err
	}

	dir, err := run(t, "foo-bar", "foo bar", "main")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"foo-bar --help\n", "foo bar --help\n", "main --help\n"} {
		name := filepath.Join(dir, "command-"+strconv.Itoa(i)+".txt")
		got, err := os.ReadFile(name)
		if err != nil || string(got) != want {
			t.Errorf("%s: got %q, err %v; want %q", name, got, err, want)
		}
	}
	for _, path := range []string{"", "../escape", "-flag", "valid /invalid", "valid  invalid"} {
		t.Run(path, func(t *testing.T) {
			dir, err := run(t, path)
			if err == nil {
				t.Fatalf("accepted invalid command path %q", path)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid path wrote help files: %v, %v", entries, err)
			}
		})
	}
}
