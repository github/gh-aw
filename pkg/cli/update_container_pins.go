package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var containerPinsLog = logger.New("cli:update_container_pins")

// imageFailure pairs a container image tag with the human-readable reason it
// could not be pinned, so the summary can surface actionable details.
type imageFailure struct {
	// image is the container image tag, e.g. "ghcr.io/github/github-mcp-server:v0.32.0".
	image string
	// reason is the human-readable error message explaining why digest resolution failed.
	reason string
}

// dockerImagesArgPattern matches the download_docker_images.sh invocation in lock files
// and captures the space-separated list of image arguments.
// Example: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" img1 img2
var dockerImagesArgPattern = regexp.MustCompile(`download_docker_images\.sh"?\s+(.+)`)

// buildxDigestPattern matches the "Digest:" line in the output of
// "docker buildx imagetools inspect", e.g. "Digest:    sha256:abc123..."
// The first capture group is the full "sha256:..." digest string.
// SHA-256 digests are always exactly 64 lowercase hexadecimal characters;
// other hash algorithms (SHA-384, SHA-512) are not used by OCI image manifests
// in practice and are not expected here.
var buildxDigestPattern = regexp.MustCompile(`(?m)^Digest:\s+(sha256:[a-f0-9]{64})`)

type containerPinUpdateDeps struct {
	fetchDigest func(ctx context.Context, image string, verbose bool) (string, error)
}

type containerPinUpdateOptions struct {
	refreshExisting     bool
	failOnResolveErrors bool
}

func defaultContainerPinUpdateDeps() containerPinUpdateDeps {
	return containerPinUpdateDeps{fetchDigest: fetchContainerDigest}
}

// UpdateContainerPins resolves SHA-256 digests for all container images referenced in
// the compiled lock files under workflowDir and stores the pins in
// .github/aw/actions-lock.json.
//
// Images that already have a digest appended (containing "@sha256:") are skipped,
// as they are already pinned. Images without a cached pin are queried via the
// Docker CLI ("docker buildx imagetools inspect").
//
// When Docker is unavailable the function logs a warning and returns nil so that
// the overall upgrade flow is not interrupted.
//
// Returns true when new container pins were added and lock files should be
// recompiled to embed the digest references.
func UpdateContainerPins(ctx context.Context, workflowDir string, verbose bool) (bool, error) {
	return updateContainerPins(ctx, defaultContainerPinUpdateDeps(), workflowDir, verbose, containerPinUpdateOptions{})
}

func updateContainerPins(ctx context.Context, deps containerPinUpdateDeps, workflowDir string, verbose bool, opts containerPinUpdateOptions) (bool, error) {
	containerPinsLog.Print("Starting container pin update")

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Updating container image pins..."))
	}

	// Collect all container images referenced in the compiled lock files.
	images, err := collectImagesFromLockFiles(workflowDir)
	if err != nil {
		containerPinsLog.Printf("Failed to collect images from lock files: %v", err)
		// Non-fatal — just skip
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to collect container images: %v", err)))
		}
		return false, nil
	}

	if len(images) == 0 {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("No container images found in lock files"))
		}
		return false, nil
	}

	containerPinsLog.Printf("Found %d unique container image(s) across lock files", len(images))

	// Load the action cache.
	actionsLockPath := filepath.Join(".github", "aw", "actions-lock.json")
	actionCache := workflow.NewActionCache(".")
	if fileutil.FileExists(actionsLockPath) {
		if loadErr := actionCache.Load(); loadErr != nil {
			return false, fmt.Errorf("failed to load actions-lock.json: %w", loadErr)
		}
	}

	imageSet := make(map[string]struct{}, len(images))
	for _, img := range images {
		base, _, _ := strings.Cut(img, "@sha256:")
		imageSet[base] = struct{}{}
	}

	// Remove any container pin entries that are no longer referenced by the
	// compiled lock files.  This keeps actions-lock.json consistent with what
	// compile actually emits and prevents stale version accumulation.
	prunedCount := actionCache.PruneStaleContainerPins(imageSet)
	if prunedCount > 0 {
		containerPinsLog.Printf("Pruned %d stale container pin(s) from actions-lock.json", prunedCount)
	}

	// Resolve digests for images that are not yet pinned.
	type pinnedEntry struct {
		image       string
		pinnedImage string // image@sha256:...
	}
	var updatedImages []pinnedEntry
	var failedImages []imageFailure
	var skippedImages []string

	for _, image := range images {
		existingPin, hasExistingPin := actionCache.GetContainerPin(image)
		if hasExistingPin && existingPin.Digest != "" && !opts.refreshExisting {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("%s already pinned: %s", image, existingPin.Digest)))
			}
			skippedImages = append(skippedImages, image)
			continue
		}

		digest, resolveErr := deps.fetchDigest(ctx, image, verbose)
		if resolveErr != nil {
			containerPinsLog.Printf("Failed to resolve digest for %s: %v", image, resolveErr)
			failedImages = append(failedImages, imageFailure{image: image, reason: resolveErr.Error()})
			continue
		}

		pinnedImage := image + "@" + digest
		if hasExistingPin && existingPin.Digest == digest && existingPin.PinnedImage == pinnedImage {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("%s already pinned: %s", image, digest)))
			}
			skippedImages = append(skippedImages, image)
			continue
		}
		actionCache.SetContainerPin(image, digest, pinnedImage)
		updatedImages = append(updatedImages, pinnedEntry{image: image, pinnedImage: pinnedImage})

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Pinned %s → %s", image, digest)))
		}
	}

	// Print summary.
	fmt.Fprintln(os.Stderr, "")

	if len(updatedImages) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Updated %d container image pin(s):", len(updatedImages))))
		for _, entry := range updatedImages {
			fmt.Fprintln(os.Stderr, console.FormatListItem(entry.pinnedImage))
		}
		fmt.Fprintln(os.Stderr, "")
	}

	if prunedCount > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Pruned %d stale container pin(s) from actions-lock.json", prunedCount)))
		fmt.Fprintln(os.Stderr, "")
	}

	if len(skippedImages) > 0 && verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("%d container image(s) already up to date", len(skippedImages))))
		fmt.Fprintln(os.Stderr, "")
	}

	if len(failedImages) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to resolve digest for %d image(s) (Docker/crane may be unavailable):", len(failedImages))))
		for _, f := range failedImages {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", f.image, f.reason)
		}
		fmt.Fprintln(os.Stderr, "")
	}

	if len(updatedImages) > 0 || prunedCount > 0 {
		if err := actionCache.Save(); err != nil {
			return false, fmt.Errorf("failed to save actions-lock.json: %w", err)
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Updated container pins in actions-lock.json"))
	}

	if len(failedImages) > 0 && opts.failOnResolveErrors {
		return len(updatedImages) > 0, fmt.Errorf("failed to resolve digest for %d image(s)", len(failedImages))
	}

	return len(updatedImages) > 0, nil
}

// collectImagesFromLockFiles scans all .lock.yml files under workflowDir and returns
// a sorted, deduplicated list of container image tags referenced in
// "download_docker_images.sh" invocations.
func collectImagesFromLockFiles(workflowDir string) ([]string, error) {
	if workflowDir == "" {
		workflowDir = constants.GetWorkflowDir()
	}

	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	imageSet := make(map[string]struct {
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lock.yml") {
			continue
		}

		content, readErr := os.ReadFile(filepath.Join(workflowDir, entry.Name()))
		if readErr != nil {
			containerPinsLog.Printf("Warning: could not read %s: %v", entry.Name(), readErr)
			continue
		}

		for line := range strings.SplitSeq(string(content), "\n") {
			matches := dockerImagesArgPattern.FindStringSubmatch(line)
			if len(matches) < 2 {
				continue
			}
			for img := range strings.FieldsSeq(matches[1]) {
				if base, _, _ := strings.Cut(img, "@sha256:"); base != "" {
					imageSet[base] = struct{}{}
				}
			}
		}
	}

	images := sliceutil.SortedKeys(imageSet)
	if images == nil {
		images = []string{}
	}

	containerPinsLog.Printf("Collected %d unique container image(s) from lock files in %s", len(images), workflowDir)
	return images, nil
}

// dockerCmdTimeout is the maximum time allowed for a single Docker CLI operation.
// 60 seconds is sufficient for most registry metadata lookups and short pulls
// while still bounding any hung Docker daemon or slow network connections.
const dockerCmdTimeout = 60 * time.Second

// fetchContainerDigest returns the SHA-256 content digest for the given image tag.
// It tries three strategies in order:
//  1. "docker buildx imagetools inspect" (no pull, preferred — works when docker daemon is running)
//  2. "crane digest" (no pull, no daemon — works in CI without Docker)
//  3. "docker pull" + "docker inspect" (fallback that pulls the full image)
//
// Returns an error when all three strategies fail.
func fetchContainerDigest(ctx context.Context, image string, verbose bool) (string, error) {
	containerPinsLog.Printf("Resolving digest for container image: %s", image)

	type strategy struct {
		name string
		fn   func() (string, error)
	}
	strategies := []strategy{
		{
			name: "docker buildx imagetools",
			fn:   func() (string, error) { return resolveDigestViaBuildx(ctx, image) },
		},
		{
			name: "crane digest",
			fn:   func() (string, error) { return resolveDigestViaCrane(ctx, image) },
		},
		{
			name: "docker pull + inspect",
			fn:   func() (string, error) { return resolveDigestViaPull(ctx, image, verbose) },
		},
	}

	var errs []string
	for _, s := range strategies {
		digest, err := s.fn()
		if err == nil && digest != "" {
			containerPinsLog.Printf("Resolved %s via %s: %s", image, s.name, digest)
			return digest, nil
		}
		msg := fmt.Sprintf("%s: %v", s.name, err)
		containerPinsLog.Printf("Strategy %q failed for %s: %v", s.name, image, err)
		errs = append(errs, msg)
	}

	return "", errors.New(strings.Join(errs, "; "))
}

// resolveDigestViaBuildx uses "docker buildx imagetools inspect" to get the content
// digest without pulling the image layers. It parses the top-level "Digest:" line
// from the human-readable text output because the --format template flag is not
// supported consistently across all Docker buildx versions.
func resolveDigestViaBuildx(ctx context.Context, image string) (string, error) {
	// Run without --format so the output is the stable human-readable text, e.g.:
	//   Name:      ghcr.io/github/github-mcp-server:v0.32.0
	//   MediaType: application/vnd.oci.image.index.v1+json
	//   Digest:    sha256:abc123...
	ctx, cancel := context.WithTimeout(ctx, dockerCmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "buildx", "imagetools", "inspect", image).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("buildx inspect failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	matches := buildxDigestPattern.FindSubmatch(out)
	if len(matches) < 2 {
		return "", fmt.Errorf("no sha256 digest found in buildx inspect output for %s", image)
	}
	return string(matches[1]), nil
}

// resolveDigestViaCrane uses the "crane" CLI tool to resolve a digest without
// requiring the Docker daemon. crane is part of google/go-containerregistry and
// is commonly pre-installed in CI environments. It works with public and
// authenticated private registries using the local credential store.
func resolveDigestViaCrane(ctx context.Context, image string) (string, error) {
	// crane digest IMAGE outputs a single line like: sha256:abc123...
	ctx, cancel := context.WithTimeout(ctx, dockerCmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "crane", "digest", image).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("crane digest failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	digest := strings.TrimSpace(string(out))
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("unexpected digest format from crane: %q", digest)
	}
	return digest, nil
}

// resolveDigestViaPull pulls the image and then reads its RepoDigests field.
func resolveDigestViaPull(ctx context.Context, image string, verbose bool) (string, error) {
	if verbose {
		fmt.Fprintf(os.Stderr, "  Pulling %s to resolve digest...\n", image)
	}

	pullCtx, pullCancel := context.WithTimeout(ctx, dockerCmdTimeout)
	defer pullCancel()
	if out, err := exec.CommandContext(pullCtx, "docker", "pull", "--quiet", image).CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker pull failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	inspectCtx, inspectCancel := context.WithTimeout(ctx, dockerCmdTimeout)
	defer inspectCancel()
	out, err := exec.CommandContext(inspectCtx, "docker", "inspect",
		"--format", "{{index .RepoDigests 0}}", image).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker inspect failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// RepoDigest format: "registry/image@sha256:..."  or "image@sha256:..."
	repoDigest := strings.TrimSpace(string(out))
	idx := strings.Index(repoDigest, "@sha256:")
	if idx < 0 {
		return "", fmt.Errorf("no sha256 digest in repo digest %q", repoDigest)
	}
	return repoDigest[idx+1:], nil // return "sha256:..."
}
