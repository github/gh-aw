package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
)

var syftLog = logger.New("cli:syft")

type syftOutput struct {
	Artifacts []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Type    string `json:"type"`
	} `json:"artifacts"`
}

// SyftScanResult holds the results of a Syft scan.
type SyftScanResult struct {
	ImageRef     string
	PackageCount int
	SBOMPath     string // Path to the persisted SBOM file
}

// runSyftOnLockFiles extracts container image references from lock-file manifests
// and runs syft to generate SBOM data for each unique image.
// SBOM files are persisted to disk and paths are returned in the results.
func runSyftOnLockFiles(lockFiles []string, verbose bool, strict bool) error { //nolint:largefunc
	if len(lockFiles) == 0 {
		return nil
	}

	images := collectContainerImagesFromLockFiles(lockFiles)
	if len(images) == 0 {
		syftLog.Print("No container images found in lock files")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Running syft SBOM scanner (0 container images found in lock files)"))
		return nil
	}

	if len(images) == 1 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Running syft SBOM scanner on 1 container image"))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Running syft SBOM scanner on %d container images", len(images))))
	}

	// Create output directory for SBOM files
	sbomDir := filepath.Join(os.TempDir(), "gh-aw-syft-sboms")
	if err := os.MkdirAll(sbomDir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create SBOM directory: %w", err)
	}

	var scanErrors []string
	var results []SyftScanResult

	ctx := context.Background()
	for _, img := range images {
		imageRef := img.PinnedImage
		if imageRef == "" {
			imageRef = img.Image
		}

		result, err := runSyftOnImage(ctx, imageRef, sbomDir, verbose)
		if err != nil {
			syftLog.Printf("Syft scan failed for %s: %v", img.Image, err)
			scanErrors = append(scanErrors, fmt.Sprintf("%s: %v", img.Image, err))
			continue
		}
		results = append(results, *result)
	}

	// Report SBOM file locations
	if verbose && len(results) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("SBOM files saved to: "+sbomDir))
		for _, result := range results {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("  %s: %s (%d packages)", result.ImageRef, result.SBOMPath, result.PackageCount)))
		}
	}

	if len(scanErrors) == 0 {
		return nil
	}

	errMsg := fmt.Sprintf("syft scan failed for %d image(s): %s", len(scanErrors), strings.Join(scanErrors, "; "))
	if strict {
		return fmt.Errorf("%s", errMsg)
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(errMsg))
	return nil
}

func runSyftOnImage(ctx context.Context, imageRef, sbomDir string, verbose bool) (*SyftScanResult, error) { //nolint:largefunc
	syftLog.Printf("Scanning %s with syft", imageRef)

	// Validate the image reference before it reaches docker: lock-file manifests can carry
	// attacker-influenced content, and an image reference starting with "-" (or containing
	// control characters) would otherwise be interpreted as a docker/syft option.
	validatedImageRef, err := validateDockerImageRef(imageRef)
	if err != nil {
		return nil, err
	}

	dockerPath, err := fileutil.ResolveExecutablePath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker command not found: %w", err)
	}

	syftImageRef, err := validateDockerImageRef(SyftImage)
	if err != nil {
		return nil, fmt.Errorf("invalid syft scanner image reference %q: %w", SyftImage, err)
	}

	// #nosec G204 -- dockerPath is resolved from the fixed executable name "docker" and
	// validatedImageRef is allow-list validated above. exec.CommandContext passes args
	// directly to the OS without shell interpretation, preventing command injection.
	cmd := exec.CommandContext(
		ctx,
		dockerPath,
		"run",
		"--rm",
		syftImageRef,
		validatedImageRef,
		"-o", "syft-json",
	)

	if verbose {
		dockerCmd := shellJoinArgs([]string{"docker", "run", "--rm", syftImageRef, validatedImageRef, "-o", "syft-json"})
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Run syft directly: "+dockerCmd))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			syftLog.Printf("syft stderr for %s: %s", imageRef, stderrStr)
			return nil, fmt.Errorf("syft failed on %s: %w\nstderr: %s", imageRef, err, stderrStr)
		}
		return nil, fmt.Errorf("syft failed on %s: %w", imageRef, err)
	}

	var output syftOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("failed to parse syft JSON output for %s: %w", imageRef, err)
	}

	// Generate a safe filename from the image reference
	replacer := strings.NewReplacer("/", "_", ":", "_", "@", "_")
	safeImageName := replacer.Replace(imageRef)
	sbomPath := filepath.Join(sbomDir, fmt.Sprintf("sbom-%s.json", safeImageName))

	// Persist the SBOM to disk
	if err := os.WriteFile(sbomPath, stdout.Bytes(), constants.FilePermPublic); err != nil {
		return nil, fmt.Errorf("failed to write SBOM file for %s: %w", imageRef, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("syft scanned %s (%d packages, SBOM: %s)", imageRef, len(output.Artifacts), sbomPath)))

	return &SyftScanResult{
		ImageRef:     imageRef,
		PackageCount: len(output.Artifacts),
		SBOMPath:     sbomPath,
	}, nil
}
