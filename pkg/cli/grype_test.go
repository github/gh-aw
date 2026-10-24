//go:build !integration

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGrypeDisplayFindings_NilOutput(t *testing.T) {
	t.Parallel()
	count := grypeDisplayFindings("test-image:latest", nil)
	if count != 0 {
		t.Errorf("Expected 0 findings for nil output, got %d", count)
	}
}

func TestGrypeDisplayFindings_EmptyMatches(t *testing.T) {
	t.Parallel()
	output := &grypeOutput{Matches: []grypeFinding{}}
	count := grypeDisplayFindings("test-image:latest", output)
	if count != 0 {
		t.Errorf("Expected 0 findings for empty output, got %d", count)
	}
}

func TestGrypeDisplayFindings_WithFindings(t *testing.T) {
	t.Parallel()
	output := &grypeOutput{
		Matches: []grypeFinding{
			makeGrypeFinding("CVE-2021-12345", "High", "libssl", "1.1.1", []string{"1.1.2"}, "https://nvd.nist.gov/vuln/detail/CVE-2021-12345"),
			makeGrypeFinding("CVE-2021-99999", "Critical", "openssl", "1.0.0", nil, ""),
		},
	}

	count := grypeDisplayFindings("ubuntu:20.04", output)
	if count != 2 {
		t.Errorf("Expected 2 findings, got %d", count)
	}
}

func TestGrypeDisplayFindings_SeverityMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		severity string
		wantType string
	}{
		{"Critical", "error"},
		{"High", "error"},
		{"Medium", "warning"},
		{"Low", "info"},
		{"Negligible", "info"},
		{"Informational", "info"},
		{"Unknown", "warning"},
		{"", "warning"},
	}

	for _, tc := range tests {
		t.Run(tc.severity, func(t *testing.T) {
			t.Parallel()
			output := &grypeOutput{
				Matches: []grypeFinding{
					makeGrypeFinding("CVE-2021-00000", tc.severity, "pkg", "1.0", nil, ""),
				},
			}
			// We can't easily test the errorType without capturing stderr,
			// but we can verify the function returns the right count.
			count := grypeDisplayFindings("test-image:latest", output)
			if count != 1 {
				t.Errorf("Expected 1 finding for severity %q, got %d", tc.severity, count)
			}
		})
	}
}

func TestGrypeCacheGetSet(t *testing.T) {
	t.Parallel()
	cache := &grypeCache{
		results: make(map[string]*grypeOutput),
		errors:  make(map[string]error),
	}

	key := "test-image:latest"

	// Initially no entry.
	result, err, ok := cache.get(key)
	if ok {
		t.Error("Expected no cache entry initially")
	}
	if result != nil || err != nil {
		t.Error("Expected nil result and nil error for empty cache")
	}

	// Set a result.
	expected := &grypeOutput{Matches: []grypeFinding{}}
	cache.set(key, expected)

	result, err, ok = cache.get(key)
	if !ok {
		t.Error("Expected cache hit after set")
	}
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result != expected {
		t.Error("Expected cached result to match stored result")
	}
}

func TestGrypeCacheSetError(t *testing.T) {
	t.Parallel()
	cache := &grypeCache{
		results: make(map[string]*grypeOutput),
		errors:  make(map[string]error),
	}

	key := "test-image:v1.0"
	testErr := errors.New("test scan error")
	cache.setError(key, testErr)

	result, err, ok := cache.get(key)
	if !ok {
		t.Error("Expected cache hit after setError")
	}
	if result != nil {
		t.Error("Expected nil result for error entry")
	}
	if !errors.Is(err, testErr) {
		t.Errorf("Expected stored error %v, got %v", testErr, err)
	}
}

func TestRunGrypeOnLockFiles_NoLockFiles(t *testing.T) {
	t.Parallel()
	err := runGrypeOnLockFiles([]string{}, false, false)
	if err != nil {
		t.Errorf("Expected no error for empty lock file list, got: %v", err)
	}
}

func TestCollectContainerImagesFromLockFiles_Nil(t *testing.T) {
	t.Parallel()
	images := collectContainerImagesFromLockFiles(nil)
	if images != nil {
		t.Errorf("Expected nil for nil input, got %v", images)
	}
}

func TestCollectContainerImagesFromLockFiles_Empty(t *testing.T) {
	t.Parallel()
	images := collectContainerImagesFromLockFiles([]string{})
	if images != nil {
		t.Errorf("Expected nil for empty input, got %v", images)
	}
}

func TestCollectContainerImagesFromLockFiles_NonExistentFile(t *testing.T) {
	t.Parallel()
	images := collectContainerImagesFromLockFiles([]string{"/nonexistent/path.lock.yml"})
	if len(images) != 0 {
		t.Errorf("Expected 0 images for non-existent file, got %d", len(images))
	}
}

func TestCollectContainerImagesFromLockFiles_NoManifest(t *testing.T) {
	t.Parallel()
	// A lock file with no gh-aw-manifest header.
	tmpFile, err := os.CreateTemp("", "test-*.lock.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString("# Generated workflow YAML\nname: test\n"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	images := collectContainerImagesFromLockFiles([]string{tmpFile.Name()})
	if len(images) != 0 {
		t.Errorf("Expected 0 images for lock file without manifest, got %d", len(images))
	}
}

func TestCollectContainerImagesFromLockFiles_WithManifest(t *testing.T) {
	t.Parallel()
	tmpFile, err := os.CreateTemp("", "test-*.lock.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	manifest := `# gh-aw-manifest: {"version":1,"secrets":[],"actions":[],"containers":[{"image":"ghcr.io/test/image:v1.0","digest":"sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1","pinned_image":"ghcr.io/test/image:v1.0@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1"}]}`
	if _, err := tmpFile.WriteString(manifest + "\n"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	images := collectContainerImagesFromLockFiles([]string{tmpFile.Name()})
	if len(images) != 1 {
		t.Fatalf("Expected 1 image, got %d: %v", len(images), images)
	}
	if images[0].Image != "ghcr.io/test/image:v1.0" {
		t.Errorf("Expected image tag %q, got %q", "ghcr.io/test/image:v1.0", images[0].Image)
	}
	if images[0].PinnedImage == "" {
		t.Error("Expected non-empty PinnedImage")
	}
}

func TestCollectContainerImagesFromLockFiles_DeduplicatesByPinnedImage(t *testing.T) {
	t.Parallel()
	manifest := `# gh-aw-manifest: {"version":1,"secrets":[],"actions":[],"containers":[{"image":"ghcr.io/test/image:v1.0","digest":"sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1","pinned_image":"ghcr.io/test/image:v1.0@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1"}]}`

	file1, err := os.CreateTemp("", "test-*.lock.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file 1: %v", err)
	}
	defer os.Remove(file1.Name())
	file1.WriteString(manifest + "\n")
	file1.Close()

	file2, err := os.CreateTemp("", "test-*.lock.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file 2: %v", err)
	}
	defer os.Remove(file2.Name())
	file2.WriteString(manifest + "\n")
	file2.Close()

	images := collectContainerImagesFromLockFiles([]string{file1.Name(), file2.Name()})
	if len(images) != 1 {
		t.Errorf("Expected 1 unique image after deduplication, got %d", len(images))
	}
}

func TestCollectContainerImagesFromLockFiles_MultipleDistinctImages(t *testing.T) {
	t.Parallel()
	manifest1 := `# gh-aw-manifest: {"version":1,"secrets":[],"actions":[],"containers":[{"image":"ghcr.io/test/image-a:v1.0","pinned_image":"ghcr.io/test/image-a:v1.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`
	manifest2 := `# gh-aw-manifest: {"version":1,"secrets":[],"actions":[],"containers":[{"image":"ghcr.io/test/image-b:v2.0","pinned_image":"ghcr.io/test/image-b:v2.0@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`

	file1, err := os.CreateTemp("", "test-*.lock.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file 1: %v", err)
	}
	defer os.Remove(file1.Name())
	file1.WriteString(manifest1 + "\n")
	file1.Close()

	file2, err := os.CreateTemp("", "test-*.lock.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file 2: %v", err)
	}
	defer os.Remove(file2.Name())
	file2.WriteString(manifest2 + "\n")
	file2.Close()

	images := collectContainerImagesFromLockFiles([]string{file1.Name(), file2.Name()})
	if len(images) != 2 {
		t.Errorf("Expected 2 distinct images, got %d", len(images))
	}
}

func TestCollectContainerImagesFromLockFiles_EmptyImageIgnored(t *testing.T) {
	t.Parallel()
	manifest := `# gh-aw-manifest: {"version":1,"secrets":[],"actions":[],"containers":[{"image":"","pinned_image":""}]}`

	tmpFile, err := os.CreateTemp("", "test-*.lock.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString(manifest + "\n")
	tmpFile.Close()

	images := collectContainerImagesFromLockFiles([]string{tmpFile.Name()})
	if len(images) != 0 {
		t.Errorf("Expected 0 images (empty image name ignored), got %d", len(images))
	}
}

func TestCollectContainerImagesFromLockFiles_NoContainers(t *testing.T) {
	t.Parallel()
	manifest := `# gh-aw-manifest: {"version":1,"secrets":["MY_SECRET"],"actions":[]}`

	tmpFile, err := os.CreateTemp("", "test-*.lock.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString(manifest + "\n")
	tmpFile.Close()

	images := collectContainerImagesFromLockFiles([]string{tmpFile.Name()})
	if len(images) != 0 {
		t.Errorf("Expected 0 images for manifest without containers, got %d", len(images))
	}
}

// makeGrypeFinding is a test helper that constructs a grypeFinding.
func makeGrypeFinding(id, severity, pkgName, pkgVersion string, fixVersions []string, dataSource string) grypeFinding {
	f := grypeFinding{}
	f.Vulnerability.ID = id
	f.Vulnerability.Severity = severity
	f.Vulnerability.DataSource = dataSource
	f.Vulnerability.Fix.Versions = fixVersions
	f.Artifact.Name = pkgName
	f.Artifact.Version = pkgVersion
	return f
}

func TestGrypeRunOnImage_RejectsUnsafeImageRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		imageRef string
	}{
		{"option flag", "--entrypoint=/bin/sh"},
		{"embedded newline", "alpine:latest\n--privileged"},
		{"semicolon", "ghcr.io/org/im;age:latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := grypeRunOnImage(tt.imageRef, "", false)
			if err == nil {
				t.Fatalf("Expected error for unsafe image reference %q", tt.imageRef)
			}
			if !strings.Contains(err.Error(), "docker image reference") {
				t.Errorf("Expected image reference validation error, got: %v", err)
			}
		})
	}
}

func TestValidateExecArgument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		arg     string
		wantErr bool
	}{
		{name: "valid argument", arg: "ghcr.io/anchore/grype:v0.80.0"},
		{name: "empty argument", arg: "", wantErr: true},
		{name: "argument with control character", arg: "value\nnext", wantErr: true},
		{name: "argument starting with dash", arg: "-malicious", wantErr: true},
		{name: "argument with null byte", arg: "val\x00ue", wantErr: true},
		{name: "argument with tab", arg: "val\tue", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateExecArgument(tt.arg)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for argument %q", tt.arg)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for argument %q: %v", tt.arg, err)
			}
		})
	}
}

func TestGrypeRunOnImage_AcceptsValidImageRef(t *testing.T) {
	prependFakeDockerToPath(t, `{"matches":[]}`)

	_, err := grypeRunOnImage("ghcr.io/anchore/grype:v0.80.0", "", false)
	if err != nil {
		t.Fatalf("Expected valid image reference to reach docker, got: %v", err)
	}
}

func prependFakeDockerToPath(t *testing.T, stdout string) {
	t.Helper()

	binDir := t.TempDir()
	dockerPath := filepath.Join(binDir, "docker")
	script := "#!/bin/sh\nprintf '%s' '" + stdout + "'\nexit \"${TEST_DOCKER_EXIT_CODE:-0}\"\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("Failed to write fake docker executable: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestGrypeRunOnImageOutputValidation(t *testing.T) {
	tests := []struct {
		name            string
		stdout          string
		wantErr         bool
		wantErrContains string
	}{
		{"empty", "", true, ""},
		{"whitespace", " \n\t", true, ""},
		{"non-JSON", "scanner unavailable", true, ""},
		{"malformed JSON", `{"matches":[`, true, "failed to parse grype JSON output"},
		{"missing matches", `{}`, true, ""},
		{"null output", `null`, true, ""},
		{"null matches", `{"matches":null}`, true, ""},
		{"wrong matches type", `{"matches":{}}`, true, ""},
		{"zero matches", `{"matches":[]}`, false, ""},
		{"matches", `{"matches":[{"vulnerability":{"id":"CVE-2026-1234"}}]}`, false, ""},
	}
	for i, tc := range tests {
		for _, exitCode := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("%s/exit-%d", tc.name, exitCode), func(t *testing.T) {
				prependFakeDockerToPath(t, tc.stdout)
				t.Setenv("TEST_DOCKER_EXIT_CODE", strconv.Itoa(exitCode))
				imageRef := fmt.Sprintf("test-output-%d-%d:latest", i, exitCode)
				output, err := grypeRunOnImage(imageRef, "", false)
				wantErr := tc.wantErr || exitCode == 2
				if (err != nil) != wantErr {
					t.Fatalf("scan error = %v, want error = %v", err, wantErr)
				}
				key, keyErr := grypeCacheKey(imageRef, "")
				if keyErr != nil {
					t.Fatal(keyErr)
				}
				cached, cachedErr, ok := grypeScanResultCache.get(key)
				if !ok || (cachedErr != nil) != wantErr {
					t.Fatalf("cache entry = (%v, %v, %v), want error = %v", cached, cachedErr, ok, wantErr)
				}
				if wantErr {
					if output != nil || cached != nil {
						t.Fatal("failed scan must not return or cache a successful result")
					}
					if !strings.Contains(err.Error(), imageRef) {
						t.Fatalf("scan error must identify image: %v", err)
					}
					if exitCode == 1 && tc.wantErrContains != "" && !strings.Contains(err.Error(), tc.wantErrContains) {
						t.Fatalf("scan error = %q, want it to contain %q", err, tc.wantErrContains)
					}
				} else if output == nil || cached != output {
					t.Fatal("successful scan must return and cache its result")
				}
			})
		}
	}
}

func TestRunGrypeOnLockFilesImageStatus(t *testing.T) {
	prependFakeDockerToPath(t, `{"matches":[]}`)
	for _, failed := range []bool{false, true} {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("failed-%t/strict-%t", failed, strict), func(t *testing.T) {
				imageRef := fmt.Sprintf("test-status-%t-%t:latest@sha256:%s", failed, strict, strings.Repeat("a", 64))
				otherRef := fmt.Sprintf("test-status-other-%t-%t:latest", failed, strict)
				lockFile := filepath.Join(t.TempDir(), "test.lock.yml")
				manifest := fmt.Sprintf("# gh-aw-manifest: {\"version\":1,\"containers\":[{\"image\":\"test-status:latest\",\"pinned_image\":%q},{\"image\":%q}]}\n", imageRef, otherRef)
				if err := os.WriteFile(lockFile, []byte(manifest), 0o600); err != nil {
					t.Fatal(err)
				}
				if failed {
					key, err := grypeCacheKey(imageRef, grypeConfigFile())
					if err != nil {
						t.Fatal(err)
					}
					grypeScanResultCache.setError(key, errors.New("invalid scanner output"))
				}
				var scanErr error
				log := captureStderrForGuardPolicyReportTest(func() {
					scanErr = runGrypeOnLockFiles([]string{lockFile}, false, strict)
				})
				if (scanErr != nil) != (failed && strict) {
					t.Fatalf("scan error = %v, failed = %t, strict = %t", scanErr, failed, strict)
				}
				for _, ref := range []string{imageRef, otherRef} {
					if !strings.Contains(log, "Grype scan planned for "+ref) {
						t.Fatalf("missing planned image %s: %s", ref, log)
					}
				}
				if !strings.Contains(log, "Grype scan completed for "+otherRef+": 0 vulnerability finding(s)") {
					t.Fatalf("missing successful zero-finding image: %s", log)
				}
				if failed {
					if !strings.Contains(log, "Grype scan failed for "+imageRef+":") || strings.Contains(log, "Grype scan completed for "+imageRef) {
						t.Fatalf("failed image must not be reported as completed: %s", log)
					}
				} else if !strings.Contains(log, "Grype scan completed for "+imageRef+": 0 vulnerability finding(s)") {
					t.Fatalf("missing pinned zero-finding image: %s", log)
				}
			})
		}
	}
}

func TestGrypeDockerArgs_WithoutConfig(t *testing.T) {
	t.Parallel()
	args, err := grypeDockerArgs("alpine:3.20", "")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	expected := []string{"run", "--rm", GrypeImage, "alpine:3.20", "-o", "json"}
	if strings.Join(args, " ") != strings.Join(expected, " ") {
		t.Errorf("Expected args %v, got %v", expected, args)
	}
}

func TestGrypeDockerArgs_WithConfig(t *testing.T) {
	t.Parallel()
	configFile := filepath.Join(t.TempDir(), grypeConfigFilename)
	if err := os.WriteFile(configFile, []byte("ignore: []\n"), 0o644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	args, err := grypeDockerArgs("alpine:3.20", configFile)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-v "+configFile+":"+grypeContainerConfigPath+":ro") {
		t.Errorf("Expected read-only config mount in args, got %v", args)
	}
	if !strings.Contains(joined, "--config "+grypeContainerConfigPath) {
		t.Errorf("Expected --config flag in args, got %v", args)
	}
	if !strings.HasSuffix(joined, GrypeImage+" --config "+grypeContainerConfigPath+" alpine:3.20 -o json") {
		t.Errorf("Expected config flags to precede the image reference, got %v", args)
	}
}

func TestGrypeDockerArgs_MissingConfigFile(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	if _, err := grypeDockerArgs("alpine:3.20", missing); err == nil {
		t.Fatal("Expected error for missing config file")
	}
}

func TestGrypeCacheKeyIncludesConfigContent(t *testing.T) {
	t.Parallel()
	imageRef := "alpine:3.20"
	configFile := filepath.Join(t.TempDir(), grypeConfigFilename)
	if err := os.WriteFile(configFile, []byte("ignore: []\n"), 0o644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	first, err := grypeCacheKey(imageRef, configFile)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if err := os.WriteFile(configFile, []byte("ignore: [CVE-2026-5450]\n"), 0o644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}
	second, err := grypeCacheKey(imageRef, configFile)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	withoutConfig, err := grypeCacheKey(imageRef, "")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if first == second || first == withoutConfig || second == withoutConfig {
		t.Errorf("Expected distinct cache keys for each config state, got %q, %q, and %q", first, second, withoutConfig)
	}
}

func TestGrypeRunOnImageCachesSeparatelyByConfigContent(t *testing.T) {
	prependFakeDockerToPath(t, `{"matches":[]}`)

	originalCache := grypeScanResultCache
	grypeScanResultCache = &grypeCache{
		results: make(map[string]*grypeOutput),
		errors:  make(map[string]error),
	}
	t.Cleanup(func() {
		grypeScanResultCache = originalCache
	})

	configDir := t.TempDir()
	firstConfig := filepath.Join(configDir, "first.yaml")
	secondConfig := filepath.Join(configDir, "second.yaml")
	for configFile, content := range map[string]string{
		firstConfig:  "ignore: []\n",
		secondConfig: "ignore: [CVE-2026-5450]\n",
	} {
		if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}
	}

	for _, configFile := range []string{firstConfig, secondConfig} {
		if _, err := grypeRunOnImage("alpine:3.20", configFile, false); err != nil {
			t.Fatalf("Expected scan to succeed, got: %v", err)
		}
	}

	if len(grypeScanResultCache.results) != 2 {
		t.Errorf("Expected separately cached results for each config, got %d", len(grypeScanResultCache.results))
	}
}

func TestGrypeConfigFileResolvesRepositoryPolicy(t *testing.T) {
	t.Parallel()
	configFile := grypeConfigFile()
	if configFile == "" {
		t.Skip("Repository Grype policy is unavailable in this checkout")
	}
	if filepath.Base(configFile) != grypeConfigFilename {
		t.Errorf("Expected config basename %q, got %q", grypeConfigFilename, filepath.Base(configFile))
	}
}
