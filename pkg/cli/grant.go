package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/workflow"
	"gopkg.in/yaml.v3"
)

var grantLog = logger.New("cli:grant")

const (
	grantPolicyFilename      = ".grant.yaml"
	grantContainerPolicyPath = "/tmp/gh-aw-grant-policy.yaml"
)

type grantOutput struct {
	Tool string `json:"tool"`
	Run  struct {
		Targets []grantTargetResult `json:"targets"`
	} `json:"run"`
}

type grantTargetResult struct {
	Source struct {
		Ref string `json:"ref"`
	} `json:"source"`
	Evaluation grantTargetEvaluation `json:"evaluation"`
}

type grantTargetEvaluation struct {
	Status   string `json:"status"`
	Findings struct {
		Packages []grantPackageFinding `json:"packages"`
	} `json:"findings"`
}

type grantPackageFinding struct {
	Name     string               `json:"name"`
	Version  string               `json:"version"`
	Decision string               `json:"decision"`
	Licenses []grantLicenseDetail `json:"licenses"`
}

type grantLicenseDetail struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// runGrantOnLockFiles extracts container image references from the gh-aw-manifest
// headers in the provided lock files, deduplicates them, and runs the grant
// license scanner on each unique image via Docker.
func runGrantOnLockFiles(lockFiles []string, verbose bool, strict bool) error {
	if len(lockFiles) == 0 {
		return nil
	}

	images := collectContainerImagesFromLockFiles(lockFiles)
	if len(images) == 0 {
		grantLog.Print("No container images found in lock files")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("No container images found in lock files to scan with grant"))
		}
		return nil
	}

	policyFile, err := grantPolicyFile()
	if err != nil {
		return err
	}

	ignoredImages, err := grantIgnoredImagePatterns(policyFile)
	if err != nil {
		return err
	}

	scannable := make([]workflow.GHAWManifestContainer, 0, len(images))
	for _, img := range images {
		if grantIsImageIgnored(ignoredImages, img.Image, img.PinnedImage) {
			name := img.Image
			if name == "" {
				name = img.PinnedImage
			}
			grantLog.Printf("Skipping license scan for ignored image %s", name)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(
					fmt.Sprintf("Skipping grant license scan for %s (ignore-images in %s)", name, grantPolicyFilename)))
			}
			continue
		}
		scannable = append(scannable, img)
	}
	images = scannable

	if len(images) == 0 {
		grantLog.Print("All container images are excluded by ignore-images")
		return nil
	}

	if len(images) == 1 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running grant license scanner on 1 container image"))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(
			fmt.Sprintf("Running grant license scanner on %d container images", len(images))))
	}

	totalFindings := 0
	var scanErrors []string

	for _, img := range images {
		imageRef := img.PinnedImage
		if imageRef == "" {
			imageRef = img.Image
		}
		displayName := img.Image
		if displayName == "" {
			displayName = imageRef
		}

		output, err := grantRunOnImage(imageRef, policyFile, verbose)
		if err != nil {
			grantLog.Printf("Grant scan failed for %s: %v", displayName, err)
			scanErrors = append(scanErrors, fmt.Sprintf("%s: %v", displayName, err))
			continue
		}

		count, err := grantDisplayFindings(displayName, output)
		if err != nil {
			grantLog.Printf("Grant findings invalid for %s: %v", displayName, err)
			scanErrors = append(scanErrors, fmt.Sprintf("%s: %v", displayName, err))
			continue
		}
		totalFindings += count
	}

	if len(scanErrors) > 0 {
		errMsg := fmt.Sprintf("grant scan failed for %d image(s): %s",
			len(scanErrors), strings.Join(scanErrors, "; "))
		if strict {
			return errors.New(errMsg)
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(errMsg))
	}

	if strict && totalFindings > 0 {
		return fmt.Errorf("strict mode: grant found %d license policy finding(s) in container images", totalFindings)
	}

	return nil
}

func grantPolicyFile() (string, error) {
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return "", fmt.Errorf("grant requires a git repository checkout to locate %s: %w", grantPolicyFilename, err)
	}

	policyFile := filepath.Join(repoRoot, grantPolicyFilename)
	info, err := os.Stat(policyFile)
	if err != nil {
		return "", fmt.Errorf(
			"grant requires %s at the repository root (create it or run compile without --grant): %w",
			grantPolicyFilename,
			err,
		)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("grant policy file is not a regular file: %s", policyFile)
	}

	return policyFile, nil
}

// grantPolicy captures the gh-aw specific subset of the .grant.yaml policy.
// The ignore-images key is not part of grant's own policy schema; grant ignores
// unknown keys, and gh-aw uses it to skip license scanning for entire images.
type grantPolicy struct {
	IgnoreImages []string `yaml:"ignore-images"`
}

// grantIgnoredImagePatterns reads the ignore-images glob patterns from the grant
// policy file. Images matching one of these patterns are excluded from license
// scanning.
func grantIgnoredImagePatterns(policyFile string) ([]string, error) {
	data, err := os.ReadFile(filepath.Clean(policyFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read grant policy file %s: %w", policyFile, err)
	}

	var policy grantPolicy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("failed to parse grant policy file %s: %w", policyFile, err)
	}

	return policy.IgnoreImages, nil
}

// grantIsImageIgnored reports whether any of the image references matches one of
// the ignore-images patterns. Patterns support shell globbing (path.Match)
// so a single entry can cover every tag of an image.
func grantIsImageIgnored(patterns []string, imageRefs ...string) bool {
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		for _, ref := range imageRefs {
			if ref == "" {
				continue
			}
			matched, err := path.Match(pattern, ref)
			if err != nil {
				grantLog.Printf("Invalid ignore-images pattern %q: %v", pattern, err)
				continue
			}
			if matched {
				return true
			}
		}
	}
	return false
}

func grantRunOnImage(imageRef, policyFile string, verbose bool) (*grantOutput, error) {
	containerPolicyPath := grantContainerPolicyPath

	var err error
	imageRef, err = validateDockerImageRef(imageRef)
	if err != nil {
		return nil, err
	}
	containerPolicyPath, err = validateContainerMountPath(containerPolicyPath)
	if err != nil {
		return nil, fmt.Errorf("invalid grant container policy path %q: %w", grantContainerPolicyPath, err)
	}
	grantImageRef, err := validateDockerImageRef(GrantImage)
	if err != nil {
		return nil, fmt.Errorf("invalid grant scanner image reference %q: %w", GrantImage, err)
	}

	dockerPath, err := fileutil.ResolveExecutablePath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker command not found: %w", err)
	}

	volumeMount, err := buildDockerReadonlyFileMount(policyFile, containerPolicyPath)
	if err != nil {
		return nil, fmt.Errorf("invalid grant policy mount: %w", err)
	}

	// #nosec G204 -- imageRef and policyFile are derived from compiled lock files and the
	// current repository checkout. exec.Command passes arguments directly without a shell.
	cmd := exec.Command(
		dockerPath,
		"run",
		"--rm",
		"-v", volumeMount,
		grantImageRef,
		"--config", containerPolicyPath,
		"--output", "json",
		"check",
		imageRef,
	)

	if verbose {
		dockerCmd := shellJoinArgs([]string{
			"docker",
			"run",
			"--rm",
			"-v", volumeMount,
			grantImageRef,
			"--config", containerPolicyPath,
			"--output", "json",
			"check",
			imageRef,
		})
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Run grant directly: "+dockerCmd))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	var output grantOutput
	if stdout.Len() > 0 && strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			return nil, fmt.Errorf("failed to parse grant JSON output for %s: %w", imageRef, err)
		}
	}

	if len(output.Run.Targets) == 0 {
		if runErr != nil {
			stderrStr := strings.TrimSpace(stderr.String())
			if stderrStr != "" {
				return nil, fmt.Errorf("grant failed for %s: %s", imageRef, stderrStr)
			}
			return nil, fmt.Errorf("grant failed for %s: %w", imageRef, runErr)
		}
		return nil, fmt.Errorf("grant produced no scan targets for %s", imageRef)
	}

	for _, target := range output.Run.Targets {
		if target.Evaluation.Status == "error" {
			targetRef := target.Source.Ref
			if targetRef == "" {
				targetRef = imageRef
			}
			return nil, fmt.Errorf("grant reported an evaluation error for %s", targetRef)
		}
	}

	return &output, nil
}

func grantDisplayFindings(imageTag string, output *grantOutput) (int, error) {
	if output == nil || len(output.Run.Targets) == 0 {
		return 0, nil
	}

	findings := grantFindingsToShared(imageTag, output)
	scanfindings.Render(os.Stderr, findings)

	return len(findings), nil
}

// grantFindingsToShared maps grant's denied license packages onto the shared
// finding representation used by every scanner integration. Container images have
// no source location, so the image tag is reported as the finding location.
func grantFindingsToShared(imageTag string, output *grantOutput) []scanfindings.Finding {
	var findings []scanfindings.Finding
	for _, target := range output.Run.Targets {
		for _, pkg := range target.Evaluation.Findings.Packages {
			if pkg.Decision != "deny" {
				continue
			}

			licenses := "no licenses found"
			if len(pkg.Licenses) > 0 {
				names := make([]string, 0, len(pkg.Licenses))
				for _, license := range pkg.Licenses {
					name := license.ID
					if name == "" {
						name = license.Name
					}
					if name != "" {
						names = append(names, name)
					}
				}
				if len(names) > 0 {
					licenses = strings.Join(names, ", ")
				}
			}

			findings = append(findings, scanfindings.Finding{
				RuleID:   "license-policy",
				Severity: scanfindings.SeverityHigh,
				Message:  fmt.Sprintf("license policy violation: %s (%s)", grantPackageRef(pkg), licenses),
				File:     imageTag,
				Line:     1,
				Column:   1,
			})
		}
	}
	return findings
}

func grantPackageRef(pkg grantPackageFinding) string {
	if pkg.Version == "" {
		return pkg.Name
	}
	return pkg.Name + "@" + pkg.Version
}
