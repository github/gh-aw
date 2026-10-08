//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
)

func installTestScanner(t *testing.T, dir, name, version string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho '"+version+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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
	cmd, args, err = buildPoutineCommand(root)
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
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ResetDockerPullState()
	t.Cleanup(ResetDockerPullState)
	SetMockDockerAvailable(false)

	if err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true, Poutine: true}); err != nil {
		t.Fatalf("compatible local scanners should not require Docker: %v", err)
	}
	if err := CheckAndPrepareDockerImages(context.Background(), DockerImagesOptions{Zizmor: true, Poutine: true, Actionlint: true}); err == nil {
		t.Fatal("actionlint still requires Docker")
	}
}
