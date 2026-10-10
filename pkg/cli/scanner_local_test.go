//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
)

func installTestScanner(t *testing.T, dir, name, version string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" +
		"case \"${0##*/}\" in\n" +
		"  zizmor) [ \"$#\" -eq 1 ] && [ \"$1\" = \"--version\" ] || exit 2 ;;\n" +
		"  poutine) [ \"$#\" -eq 2 ] && [ \"$1\" = \"version\" ] && [ \"$2\" = \"--disable-version-check\" ] || exit 2 ;;\n" +
		"  actionlint) [ \"$#\" -eq 1 ] && [ \"$1\" = \"--version\" ] || exit 2 ;;\n" +
		"esac\n" +
		"printf '%s\\n' '" + version + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolvedPath
}

func TestLocalScannerPathVersionSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test binaries use a POSIX shell")
	}
	for _, tt := range []struct {
		name, version string
		want          bool
	}{
		{"zizmor", "zizmor 1.30.1", true},
		{"zizmor", "zizmor 1.31.0", true},
		{"zizmor", "zizmor 1.9.9", false},
		{"zizmor", "zizmor 1.30.1-rc.1", false},
		{"zizmor", "zizmor unknown", false},
		{"poutine", "poutine version v1.1.6", true},
		{"poutine", "poutine 1.2.0", true},
		{"poutine", "poutine 1.1.5", false},
		{"actionlint", "1.7.12\ninstalled by building from source\nbuilt with go1.25", true},
		{"actionlint", "1.8.0", true},
		{"actionlint", "1.7.11", false},
		{"actionlint", "1.7.12-rc.1", false},
		{"actionlint", "unknown", false},
	} {
		t.Run(tt.name+" "+tt.version, func(t *testing.T) {
			dir := t.TempDir()
			path := installTestScanner(t, dir, tt.name, tt.version)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			got := localScannerPath(context.Background(), tt.name)
			if (got == path) != tt.want {
				t.Fatalf("localScannerPath(%q) = %q; expected local binary: %t", tt.name, got, tt.want)
			}
		})
	}
}

func TestLocalScannerMinimumVersionsMatchDockerImages(t *testing.T) {
	if !strings.Contains(ZizmorImage, ":"+minZizmorVersion+"@") {
		t.Fatalf("zizmor minimum %s does not match pinned image", minZizmorVersion)
	}
	if !strings.Contains(PoutineImage, ":"+minPoutineVersion+"@") {
		t.Fatalf("poutine minimum %s does not match pinned image", minPoutineVersion)
	}
	if !strings.Contains(ActionlintImage, ":"+minActionlintVersion+"@") {
		t.Fatalf("actionlint minimum %s does not match pinned image", minActionlintVersion)
	}
}

func TestLocalScannerCommandsAndDockerFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test binaries use a POSIX shell")
	}
	dir := t.TempDir()
	zizmor := installTestScanner(t, dir, "zizmor", "zizmor 1.30.1")
	poutine := installTestScanner(t, dir, "poutine", "poutine 1.1.6")
	docker := installTestScanner(t, dir, "docker", "docker")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Fatal(err)
	}
	zizmorCmd, _, zizmorArgs, err := buildZizmorCommand([]string{filepath.Join(gitRoot, "README.md")})
	if err != nil || zizmorCmd.Path != zizmor || zizmorCmd.Dir != gitRoot ||
		!strings.Contains(strings.Join(zizmorArgs, " "), "--format json ./README.md") {
		t.Fatalf("local zizmor command: %v, %v, %v", zizmorCmd, zizmorArgs, err)
	}

	cmd, args, err := buildPoutineCommand(root)
	if err != nil || cmd.Path != poutine || cmd.Dir != root || !strings.Contains(strings.Join(args, " "), "analyze_local . --format json --quiet") {
		t.Fatalf("local poutine command: %v, %v, %v", cmd, args, err)
	}
	if err := os.Remove(poutine); err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("PATH")
	t.Setenv("PATH", dir)
	cmd, args, err = buildPoutineCommand(root)
	t.Setenv("PATH", path)
	if err != nil || cmd.Path != docker || cmd.Dir != root || !strings.Contains(strings.Join(args, " "), PoutineImage) {
		t.Fatalf("docker poutine command: %v, %v, %v", cmd, args, err)
	}
	installTestScanner(t, dir, "poutine", "poutine 1.1.5")
	cmd, _, err = buildPoutineCommand(root)
	if err != nil || cmd.Path != docker {
		t.Fatalf("outdated poutine must fall back to Docker: %v, %v", cmd, err)
	}
	if localScannerPath(context.Background(), "zizmor") != zizmor {
		t.Fatal("compatible zizmor not selected")
	}
	if err := os.Remove(zizmor); err != nil {
		t.Fatal(err)
	}
	installTestScanner(t, dir, "zizmor", "zizmor 1.29.0")
	zizmorCmd, _, zizmorArgs, err = buildZizmorCommand([]string{filepath.Join(gitRoot, "README.md")})
	if err != nil || zizmorCmd.Path != docker || !strings.Contains(strings.Join(zizmorArgs, " "), ZizmorImage) {
		t.Fatalf("docker zizmor command: %v, %v, %v", zizmorCmd, zizmorArgs, err)
	}
}

func TestPrepareDockerImagesUsesLocalScanners(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test binaries use a POSIX shell")
	}
	dir := t.TempDir()
	installTestScanner(t, dir, "zizmor", "zizmor 1.30.1")
	installTestScanner(t, dir, "poutine", "poutine 1.1.6")
	t.Setenv("PATH", dir)
	ResetDockerPullState()
	t.Cleanup(ResetDockerPullState)
	SetMockDockerAvailable(false)

	if err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true, Poutine: true}); err != nil {
		t.Fatalf("compatible local scanners should not require Docker: %v", err)
	}
	if err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true, Poutine: true, Actionlint: true}); err == nil {
		t.Fatal("missing actionlint requires Docker")
	}
	installTestScanner(t, dir, "actionlint", "1.7.11")
	if err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Actionlint: true}); err == nil {
		t.Fatal("outdated actionlint requires Docker")
	}
	installTestScanner(t, dir, "actionlint", "1.7.12")
	if err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true, Poutine: true, Actionlint: true}); err != nil {
		t.Fatalf("compatible local actionlint should not require Docker: %v", err)
	}
	if err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Actionlint: true, Grant: true}); err == nil {
		t.Fatal("Docker-only scanners still require Docker")
	}
}

func TestLocalActionlintCommandAndDockerFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test binaries use a POSIX shell")
	}
	dir := actionlintTestDir(t)
	actionlint := installTestScanner(t, dir, "actionlint", "1.7.12")
	docker := installTestScanner(t, dir, "docker", "docker")
	t.Setenv("PATH", dir)
	root := dir
	files := []string{".github/workflows/a.lock.yml", ".github/workflows/b space.lock.yml"}
	for _, file := range files {
		path := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("on: push\njobs: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	options := actionlintRunOptions{IgnorePatterns: []string{"foo bar", "baz"}}
	cmd := buildActionlintCommand(context.Background(), root, files, options)
	if cmd.Path != actionlint || cmd.Dir != root {
		t.Fatalf("local actionlint command: %v", cmd)
	}
	wantArgs := append([]string{"-format", "{{json .}}", "-shellcheck=", "-pyflakes=", "-ignore", "foo bar", "-ignore", "baz"}, files...)
	if got := cmd.Args[1:]; !slices.Equal(got, wantArgs) {
		t.Fatalf("local actionlint args = %q, want %q", got, wantArgs)
	}

	if hint := formatActionlintCommand(cmd.Args); strings.Contains(hint, "docker") || !strings.Contains(hint, `"foo bar"`) {
		t.Fatalf("unexpected local command hint: %s", hint)
	}
	for _, version := range []string{"1.7.11", "unknown", ""} {
		if err := os.Remove(actionlint); err != nil {
			t.Fatal(err)
		}
		if version != "" {
			installTestScanner(t, dir, "actionlint", version)
		}
		cmd = buildActionlintCommand(context.Background(), root, files, options)
		if cmd.Path != docker || strings.Join(cmd.Args[1:], " ") != strings.Join(buildActionlintDockerArgs(root, files, options), " ") {
			t.Fatalf("actionlint %q must fall back to Docker: %v", version, cmd)
		}
	}
}

func TestLocalActionlintVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test binaries use a POSIX shell")
	}
	dir := t.TempDir()
	installTestScanner(t, dir, "actionlint", "1.7.12\ninstalled by building from source")
	t.Setenv("PATH", dir)
	original := actionlintVersion
	actionlintVersion = ""
	t.Cleanup(func() { actionlintVersion = original })
	version, err := getActionlintVersion(context.Background())
	if err != nil || version != "1.7.12" {
		t.Fatalf("local actionlint version = %q, %v", version, err)
	}
}
