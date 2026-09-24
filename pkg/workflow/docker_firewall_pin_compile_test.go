//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompileWorkflow_FirewallImagesPinnedForAWF0270(t *testing.T) {
	imageTag := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")

	frontmatter := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
    id: awf
    version: ` + string(constants.DefaultFirewallVersion) + `
network:
  allowed:
    - defaults
tools:
  web-fetch:
---

# Test
Test workflow.`

	tmpDir := testutil.TempDir(t, "docker-firewall-pins-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	yaml, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(yaml)
	requireEmbeddedPin := func(image string) ContainerPin {
		t.Helper()
		pin, ok := getEmbeddedContainerPin(image)
		if !ok {
			t.Fatalf("Expected embedded pin for %s", image)
		}
		return pin
	}

	expectedPins := []struct {
		name  string
		image string
	}{
		{name: "agent", image: constants.DefaultFirewallRegistry + "/agent:" + imageTag},
		{name: "api-proxy", image: constants.DefaultFirewallRegistry + "/api-proxy:" + imageTag},
		{name: "squid", image: constants.DefaultFirewallRegistry + "/squid:" + imageTag},
	}

	for _, expectedPin := range expectedPins {
		pin := requireEmbeddedPin(expectedPin.image)

		if !strings.Contains(yamlStr, `"image":"`+pin.Image+`","digest":"`+pin.Digest+`","pinned_image":"`+pin.PinnedImage+`"`) {
			t.Errorf("Expected manifest header to include pinned metadata for %s", expectedPin.image)
		}
		if !strings.Contains(yamlStr, "#   - "+pin.PinnedImage) {
			t.Errorf("Expected pinned container comment for %s", expectedPin.image)
		}
		if !strings.Contains(yamlStr, pin.PinnedImage) {
			t.Errorf("Expected pinned download reference for %s", expectedPin.image)
		}
	}

	imageTagParts := []string{
		`imageTag`,
		imageTag + `,`,
	}
	for _, expectedPin := range expectedPins {
		pin := requireEmbeddedPin(expectedPin.image)
		imageTagParts = append(imageTagParts, expectedPin.name+"="+pin.Digest)
	}

	for _, imageTagPart := range imageTagParts {
		if !strings.Contains(yamlStr, imageTagPart) {
			t.Errorf("Expected AWF config JSON to include %s", imageTagPart)
		}
	}
}

// TestCompileWorkflow_FirewallImagesPinnedForDefaultVersion is a regression test for
// gh-aw#43307: the four gh-aw-firewall images at the current default version
// (constants.DefaultFirewallVersion) must all be digest-pinned in consumer lock files
// even when no local action-cache is present.  This covers the cli-proxy image
// introduced in v0.82 as well as the three legacy images (agent, api-proxy, squid).
func TestCompileWorkflow_FirewallImagesPinnedForDefaultVersion(t *testing.T) {
	// Strip the leading "v" to get the Docker image tag (mirrors getAWFImageTag).
	imageTag := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")

	// Enable tools.github.mode=gh-proxy so that the cli-proxy sidecar container is
	// included in the Docker pull list and therefore also pinned in the lock file.
	frontmatter := `---
on: workflow_dispatch
engine: claude
network:
  allowed:
    - defaults
tools:
  github:
    mode: gh-proxy
---

# Test
Test workflow.`

	tmpDir := testutil.TempDir(t, "docker-firewall-pins-default-version-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	yaml, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(yaml)

	expectedPins := []struct {
		name  string
		image string
	}{
		{name: "agent", image: constants.DefaultFirewallRegistry + "/agent:" + imageTag},
		{name: "api-proxy", image: constants.DefaultFirewallRegistry + "/api-proxy:" + imageTag},
		{name: "cli-proxy", image: constants.DefaultFirewallRegistry + "/cli-proxy:" + imageTag},
		{name: "squid", image: constants.DefaultFirewallRegistry + "/squid:" + imageTag},
	}

	for _, expectedPin := range expectedPins {
		pin, ok := getEmbeddedContainerPin(expectedPin.image)
		if !ok {
			t.Fatalf("Expected embedded pin for %s", expectedPin.image)
		}
		pinnedImage := pin.Image + "@" + pin.Digest
		if !strings.Contains(yamlStr, `"image":"`+pin.Image+`","digest":"`+pin.Digest+`","pinned_image":"`+pinnedImage+`"`) {
			t.Errorf("Expected manifest header to include pinned metadata for %s", pin.Image)
		}
		if !strings.Contains(yamlStr, "#   - "+pinnedImage) {
			t.Errorf("Expected pinned container comment for %s", pin.Image)
		}
		if !strings.Contains(yamlStr, pinnedImage) {
			t.Errorf("Expected pinned download reference for %s", pin.Image)
		}
	}

	imageTagParts := []string{
		`imageTag`,
		imageTag + `,`,
	}
	for _, expectedPin := range expectedPins {
		pin, ok := getEmbeddedContainerPin(expectedPin.image)
		if !ok {
			t.Fatalf("Expected embedded pin for %s", expectedPin.image)
		}
		imageTagParts = append(imageTagParts, expectedPin.name+"="+pin.Digest)
	}
	if pin, ok := getEmbeddedContainerPin(constants.DefaultFirewallRegistry + "/agent-act:" + imageTag); ok {
		imageTagParts = append(imageTagParts, `agent-act=`+pin.Digest)
	}

	for _, imageTagPart := range imageTagParts {
		if !strings.Contains(yamlStr, imageTagPart) {
			t.Errorf("Expected AWF config JSON to include %s", imageTagPart)
		}
	}
}

// TestCompileWorkflow_BuildToolsImagePinnedForArcDind is a regression test for
// gh-aw#44040: when runner.topology is arc-dind, the build-tools image must be
// digest-pinned in the compiled lock file the same way the other four gh-aw-firewall
// images (agent, api-proxy, cli-proxy, squid) are.
func TestCompileWorkflow_BuildToolsImagePinnedForArcDind(t *testing.T) {
	// Strip the leading "v" to get the Docker image tag (mirrors getAWFImageTag).
	imageTag := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")

	frontmatter := `---
on: workflow_dispatch
engine: claude
runner:
  topology: arc-dind
network:
  allowed:
    - defaults
---

# Test
Test workflow.`

	tmpDir := testutil.TempDir(t, "docker-firewall-pins-arc-dind-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	buildToolsImage := "ghcr.io/github/gh-aw-firewall/build-tools:" + imageTag
	// Use a synthetic (but valid-format) digest to deterministically verify cache-driven
	// pin propagation when runner.topology=arc-dind, even if embedded pins do not include
	// the build-tools image for the default firewall tag.
	buildToolsDigest := "sha256:9f1e0b27f54f2271ca2897f9d2a18fb8c0f0d5a7fdb6f441b8c8137f95ae3b24"
	pinnedBuildTools := buildToolsImage + "@" + buildToolsDigest
	compiler.GetSharedActionCache().SetContainerPin(buildToolsImage, buildToolsDigest, pinnedBuildTools)
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	yaml, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(yaml)

	if !strings.Contains(yamlStr, `"image":"`+buildToolsImage+`","digest":"`+buildToolsDigest+`","pinned_image":"`+pinnedBuildTools+`"`) {
		t.Errorf("Expected manifest header to include pinned metadata for %s", buildToolsImage)
	}
	if !strings.Contains(yamlStr, "#   - "+pinnedBuildTools) {
		t.Errorf("Expected pinned container comment for %s", buildToolsImage)
	}
	if !strings.Contains(yamlStr, pinnedBuildTools) {
		t.Errorf("Expected pinned download reference for %s", buildToolsImage)
	}

	if !strings.Contains(yamlStr, `build-tools=`+buildToolsDigest) {
		t.Errorf("Expected AWF config JSON to include build-tools=%s", buildToolsDigest)
	}
}

// TestCompileWorkflow_AllManifestContainersArePinned is the golden/compile test
// suggested in gh-aw#51248: rather than enumerating a fixed list of expected
// images (which previous regression tests did, and which can silently miss a
// newly-unpinned image), this test parses the actual gh-aw-manifest emitted for
// a firewall-enabled workflow with several container-backed tools and asserts
// that *every* entry in containers[] carries a non-empty digest/pinned_image.
//
// This is a repeat of gh-aw#38561 / #43307 / #44040: gh-aw-firewall images have
// silently lost their digest pin multiple times across releases while other
// container families kept theirs.
func TestCompileWorkflow_AllManifestContainersArePinned(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: claude
network:
  allowed:
    - defaults
tools:
  github:
    mode: gh-proxy
  web-fetch:
---

# Test
Test workflow.`

	tmpDir := testutil.TempDir(t, "docker-all-containers-pinned-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	yaml, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	manifest, err := ExtractGHAWManifestFromLockFile(string(yaml))
	if err != nil {
		t.Fatalf("Failed to parse gh-aw-manifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("Expected a gh-aw-manifest header in the compiled lock file")
	}
	if len(manifest.Containers) == 0 {
		t.Fatal("Expected at least one container entry in the gh-aw-manifest")
	}

	var unpinned []string
	for _, c := range manifest.Containers {
		if c.Digest == "" || c.PinnedImage == "" {
			unpinned = append(unpinned, c.Image)
		}
	}
	if len(unpinned) > 0 {
		t.Errorf("Expected every manifest container entry to carry digest/pinned_image, but %d were unpinned: %v", len(unpinned), unpinned)
	}

	if len(manifest.ResolutionFailures) > 0 {
		t.Errorf("Expected no resolution failures for default-version containers, got: %+v", manifest.ResolutionFailures)
	}
}

// TestCompileWorkflow_UnresolvedFirewallImageRecordsResolutionFailure is a
// regression test for gh-aw#51248: when a gh-aw-firewall image has no cached or
// embedded digest pin (e.g. a workflow explicitly pins a version that has never
// been resolved locally), the compiler must
// not silently emit the bare tag with no trace. It must record a resolution
// failure in the manifest so the gap is auditable, matching the issue's
// complaint that "resolution_failures in the manifest does not mention these
// images".
func TestCompileWorkflow_UnresolvedFirewallImageRecordsResolutionFailure(t *testing.T) {
	const unresolvedVersion = "v99.99.99"
	imageTag := strings.TrimPrefix(unresolvedVersion, "v")

	frontmatter := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
    id: awf
    version: ` + unresolvedVersion + `
network:
  allowed:
    - defaults
---

# Test
Test workflow.`

	tmpDir := testutil.TempDir(t, "docker-firewall-unresolved-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	yaml, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	manifest, err := ExtractGHAWManifestFromLockFile(string(yaml))
	if err != nil {
		t.Fatalf("Failed to parse gh-aw-manifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("Expected a gh-aw-manifest header in the compiled lock file")
	}

	agentRepo := constants.DefaultFirewallRegistry + "/agent"
	found := false
	for _, f := range manifest.ResolutionFailures {
		if f.Repo == agentRepo && f.Ref == imageTag {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected a resolution failure recorded for unpinned gh-aw-firewall image %s:%s, got: %+v", agentRepo, imageTag, manifest.ResolutionFailures)
	}
}
