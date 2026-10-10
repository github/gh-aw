// This file provides container image vulnerability scanning for workflow compilation.
//
// It uses the grype vulnerability scanner (via Docker) to scan container images
// referenced in compiled lock files. Images are extracted from the gh-aw-manifest
// header embedded in each lock file, deduplicated by pinned image reference, and
// scanned once per unique image per compile run (results are cached in memory).
//
// # Integration
//
// This scanner integrates alongside actionlint, zizmor, poutine, and runner-guard
// as a post-compilation step invoked via the --grype flag. Unlike the workflow-file
// scanners, grype operates on the container images referenced in the manifests rather
// than the YAML files themselves.
//
// # Caching
//
// Scan results are cached by image reference (pinned image@digest when available, or
// image tag otherwise). This prevents re-scanning the same image when multiple lock
// files reference it.

package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/workflow"
)

var grypeLog = logger.New("cli:grype")

const (
	// grypeConfigFilename is the optional grype configuration file at the repository
	// root. It carries documented, risk-accepted ignore rules for vulnerabilities that
	// have no upstream fix available.
	grypeConfigFilename      = ".grype.yaml"
	grypeContainerConfigPath = "/tmp/gh-aw-grype-config.yaml"
)

// grypeFinding represents a single vulnerability match from grype JSON output.
type grypeFinding struct {
	Vulnerability struct {
		ID         string `json:"id"`
		DataSource string `json:"dataSource"`
		Severity   string `json:"severity"`
		Fix        struct {
			Versions []string `json:"versions"`
			State    string   `json:"state"`
		} `json:"fix"`
	} `json:"vulnerability"`
	Artifact struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Type    string `json:"type"`
	} `json:"artifact"`
}

// grypeOutput represents the complete JSON output from grype.
type grypeOutput struct {
	Matches []grypeFinding `json:"matches"`
}

// grypeCache caches grype scan results by image reference and config content to
// avoid rescanning identical scans within a single compile run.
type grypeCache struct {
	mu      sync.Mutex
	results map[string]*grypeOutput
	errors  map[string]error
}

// get returns a cached result and whether an entry exists for the key.
func (c *grypeCache) get(key string) (result *grypeOutput, err error, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, found := c.results[key]; found {
		return r, nil, true
	}
	if e, found := c.errors[key]; found {
		return nil, e, true
	}
	return nil, nil, false
}

// set stores a successful scan result.
func (c *grypeCache) set(key string, result *grypeOutput) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results[key] = result
}

// setError stores a scan error so the same failure is not retried.
func (c *grypeCache) setError(key string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errors[key] = err
}

// grypeScanResultCache is the process-wide grype result cache. Its keys include
// the image reference and the grype configuration content.
var grypeScanResultCache = &grypeCache{
	results: make(map[string]*grypeOutput),
	errors:  make(map[string]error),
}

// collectContainerImagesFromLockFiles extracts unique container image references from
// the gh-aw-manifest embedded in each lock file's comment header.
// Images are deduplicated using the pinned image reference (image@digest) as the key
// when available, falling back to the bare image tag.
func collectContainerImagesFromLockFiles(lockFiles []string) []workflow.GHAWManifestContainer {
	if len(lockFiles) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var images []workflow.GHAWManifestContainer

	for _, lockFile := range lockFiles {
		// #nosec G304 -- lockFile is a path produced by the compiler from trusted markdown
		// sources. Paths are validated by the compile pipeline before being passed here.
		content, err := os.ReadFile(lockFile)
		if err != nil {
			grypeLog.Printf("Skipping %s: failed to read file: %v", lockFile, err)
			continue
		}

		manifest, err := workflow.ExtractGHAWManifestFromLockFile(string(content))
		if err != nil {
			grypeLog.Printf("Skipping %s: failed to extract manifest: %v", lockFile, err)
			continue
		}
		if manifest == nil {
			grypeLog.Printf("Skipping %s: no manifest header", lockFile)
			continue
		}

		for _, c := range manifest.Containers {
			// Use the pinned image (image@sha256:...) as the deduplication key when
			// available; fall back to the bare image tag for unpinned references.
			key := c.PinnedImage
			if key == "" {
				key = c.Image
			}
			if key == "" {
				continue
			}
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				images = append(images, c)
			}
		}
	}

	return images
}

// runGrypeOnLockFiles extracts container image references from the gh-aw-manifest
// headers in the provided lock files, deduplicates them, and runs the grype
// vulnerability scanner on each unique image via Docker.
func runGrypeOnLockFiles(lockFiles []string, verbose bool, strict bool) error {
	if len(lockFiles) == 0 {
		return nil
	}

	images := collectContainerImagesFromLockFiles(lockFiles)
	if len(images) == 0 {
		grypeLog.Print("No container images found in lock files")
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running grype vulnerability scanner (0 container images found in lock files)"))
		return nil
	}

	if len(images) == 1 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running grype vulnerability scanner on 1 container image"))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(
			fmt.Sprintf("Running grype vulnerability scanner on %d container images", len(images))))
	}

	totalFindings := 0
	var scanErrors []string

	configFile := grypeConfigFile()
	if configFile != "" {
		grypeLog.Printf("Using grype config %s", configFile)
	}

	for _, img := range images {
		// Prefer the pinned reference (image@sha256:...) for immutability guarantees.
		imageRef := img.PinnedImage
		if imageRef == "" {
			imageRef = img.Image
		}

		output, err := grypeRunOnImage(imageRef, configFile, verbose)
		if err != nil {
			grypeLog.Printf("Grype scan failed for %s: %v", img.Image, err)
			scanErrors = append(scanErrors, fmt.Sprintf("%s: %v", img.Image, err))
			continue
		}

		count := grypeDisplayFindings(img.Image, output)
		totalFindings += count
	}

	if len(scanErrors) > 0 {
		errMsg := fmt.Sprintf("grype scan failed for %d image(s): %s",
			len(scanErrors), strings.Join(scanErrors, "; "))
		if strict {
			return errors.New(errMsg)
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(errMsg))
	}

	if strict && totalFindings > 0 {
		return fmt.Errorf("strict mode: grype found %d vulnerability finding(s) in container images", totalFindings)
	}

	return nil
}

// grypeConfigFile returns the path to the optional repository-root grype configuration
// file, or an empty string when the file is absent or the current directory is not a
// git checkout. The config carries documented ignore rules for risk-accepted findings.
func grypeConfigFile() string {
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		grypeLog.Printf("Skipping grype config lookup: %v", err)
		return ""
	}

	configFile := filepath.Join(repoRoot, grypeConfigFilename)
	info, err := os.Stat(configFile)
	if err != nil || !info.Mode().IsRegular() {
		grypeLog.Printf("No grype config found at %s", configFile)
		return ""
	}

	return configFile
}

// grypeDockerArgs builds the `docker run` arguments used to scan a single image.
// When configFile is non-empty, it is mounted read-only into the scanner container
// and passed to grype via --config so repository-level ignore rules are applied.
func grypeDockerArgs(validatedImageRef, configFile string) ([]string, error) {
	if err := validateExecArgument(validatedImageRef); err != nil {
		return nil, fmt.Errorf("invalid grype image reference: %w", err)
	}

	args := []string{"run", "--rm"}

	grypeImageRef, err := validateDockerImageRef(GrypeImage)
	if err != nil {
		return nil, fmt.Errorf("invalid grype scanner image reference %q: %w", GrypeImage, err)
	}

	var configArgs []string
	if configFile != "" {
		containerConfigPath, err := validateContainerMountPath(grypeContainerConfigPath)
		if err != nil {
			return nil, fmt.Errorf("invalid grype container config path %q: %w", grypeContainerConfigPath, err)
		}
		volumeMount, err := buildDockerReadonlyFileMount(configFile, containerConfigPath)
		if err != nil {
			return nil, fmt.Errorf("invalid grype config mount: %w", err)
		}
		if err := validateExecArgument(volumeMount); err != nil {
			return nil, fmt.Errorf("invalid grype config volume mount: %w", err)
		}
		args = append(args, "-v", volumeMount)
		configArgs = []string{"--config", containerConfigPath}
	}

	args = append(args, grypeImageRef)
	args = append(args, configArgs...)
	args = append(args, validatedImageRef, "-o", "json")

	return args, nil
}

// grypeCacheKey returns a cache key that varies with the image and the contents
// of the optional Grype configuration. Configuration paths alone are insufficient
// because separate repositories can use different policies at the same path.
func grypeCacheKey(imageRef, configFile string) (string, error) {
	if configFile == "" {
		return imageRef + "\x00", nil
	}

	content, err := os.ReadFile(configFile)
	if err != nil {
		return "", fmt.Errorf("read grype config %q: %w", configFile, err)
	}
	digest := sha256.Sum256(content)
	return fmt.Sprintf("%s\x00%x", imageRef, digest), nil
}

// validateExecArgument is a final defense-in-depth gate applied to variable-origin
// docker argv values (image references, volume mounts, container paths) right where
// they are constructed. It must not be applied to hardcoded literals such as "--rm"
// or "-v", since those legitimately start with '-'.
func validateExecArgument(arg string) error {
	if arg == "" {
		return errors.New("argument cannot be empty")
	}
	if strings.HasPrefix(arg, "-") {
		return errors.New("argument must not start with '-' (flag injection risk)")
	}
	if containsControlCharacters(arg) {
		return errors.New("argument contains invalid control characters")
	}
	return nil
}

// grypeRunOnImage runs grype on a single container image reference via Docker,
// using the result cache to avoid re-scanning images already checked in this run.
// When configFile is non-empty it is mounted read-only into the scanner container
// and passed to grype via --config.
func grypeRunOnImage(imageRef, configFile string, verbose bool) (*grypeOutput, error) { //nolint:largefunc
	cacheKey, err := grypeCacheKey(imageRef, configFile)
	if err != nil {
		return nil, err
	}

	// Check cache first.
	if result, err, ok := grypeScanResultCache.get(cacheKey); ok {
		grypeLog.Printf("Grype cache hit for %s", imageRef)
		return result, err
	}

	grypeLog.Printf("Scanning %s with grype", imageRef)

	// Validate the image reference before it reaches docker: lock-file manifests can carry
	// attacker-influenced content, and an image reference starting with "-" (or containing
	// control characters) would otherwise be interpreted as a docker/grype option.
	validatedImageRef, err := validateDockerImageRef(imageRef)
	if err != nil {
		return nil, err
	}

	dockerPath, err := fileutil.ResolveExecutablePath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker command not found: %w", err)
	}

	dockerArgs, err := grypeDockerArgs(validatedImageRef, configFile)
	if err != nil {
		return nil, err
	}

	// #nosec G204 -- dockerPath is resolved from the fixed executable name "docker" and
	// validatedImageRef is allow-list validated above. exec.Command passes args directly to
	// the OS without shell interpretation, preventing command injection.
	cmd := exec.Command(dockerPath, dockerArgs...)

	if verbose {
		dockerCmd := shellJoinArgs(append([]string{"docker"}, dockerArgs...))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Run grype directly: "+dockerCmd))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// Parse JSON output regardless of exit code — grype exits non-zero when vulnerabilities
	// are found (exit 1), so a non-zero exit does not necessarily indicate a tool failure.
	var output grypeOutput
	var parseErr error
	if stdout.Len() > 0 && strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		parseErr = json.Unmarshal(stdout.Bytes(), &output)
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			// Command could not be started (e.g., Docker not found).
			scanErr := fmt.Errorf("grype failed: %w", runErr)
			grypeScanResultCache.setError(cacheKey, scanErr)
			return nil, scanErr
		}
		exitCode := exitErr.ExitCode()
		// Exit code 1 means grype found vulnerabilities — that is expected and parseable.
		// Any other non-zero code signals a real tool failure.
		if exitCode != 1 || (parseErr != nil && stdout.Len() == 0) {
			stderrStr := strings.TrimSpace(stderr.String())
			if stderrStr != "" {
				grypeLog.Printf("grype stderr for %s: %s", imageRef, stderrStr)
			}
			scanErr := fmt.Errorf("grype failed with exit code %d on %s", exitCode, imageRef)
			grypeScanResultCache.setError(cacheKey, scanErr)
			return nil, scanErr
		}
		// Exit code 1 with JSON output — vulnerability findings were returned normally.
	}

	if parseErr != nil {
		scanErr := fmt.Errorf("failed to parse grype JSON output for %s: %w", imageRef, parseErr)
		grypeScanResultCache.setError(cacheKey, scanErr)
		return nil, scanErr
	}

	grypeScanResultCache.set(cacheKey, &output)
	return &output, nil
}

// grypeDisplayFindings renders grype vulnerability findings using the CompilerError
// format so they are presented consistently with other scanner output.
// Returns the total number of findings displayed.
func grypeDisplayFindings(imageTag string, output *grypeOutput) int {
	if output == nil || len(output.Matches) == 0 {
		return 0
	}

	findings := grypeFindingsToShared(imageTag, output.Matches)
	scanfindings.Render(os.Stderr, findings)

	return len(findings)
}

// grypeFindingsToShared maps grype's native vulnerability matches onto the shared
// finding representation used by every scanner integration. Container images have
// no source location, so the image tag is reported as the finding location.
func grypeFindingsToShared(imageTag string, matches []grypeFinding) []scanfindings.Finding {
	findings := make([]scanfindings.Finding, 0, len(matches))
	for _, match := range matches {
		vuln := match.Vulnerability
		art := match.Artifact

		severityLabel := vuln.Severity
		if severityLabel == "" {
			severityLabel = "Unknown"
		}

		// Build a compact message: [Severity] CVE-ID: package@version (fix: x.y.z) (url)
		message := fmt.Sprintf("[%s] %s: %s@%s", severityLabel, vuln.ID, art.Name, art.Version)
		if len(vuln.Fix.Versions) > 0 {
			message = fmt.Sprintf("%s (fix: %s)", message, strings.Join(vuln.Fix.Versions, ", "))
		}
		if vuln.DataSource != "" {
			message = fmt.Sprintf("%s (%s)", message, vuln.DataSource)
		}

		findings = append(findings, scanfindings.Finding{
			RuleID:   vuln.ID,
			Severity: scanfindings.ParseSeverity(severityLabel),
			Message:  message,
			File:     imageTag,
			Line:     1,
			Column:   1,
		})
	}
	return findings
}
