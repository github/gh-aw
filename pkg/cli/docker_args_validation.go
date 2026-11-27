package cli

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
)

var dockerArgsValidationLog = logger.New("cli:docker_args_validation")

var (
	dockerImageNamePattern = regexp.MustCompile(`^(?:[a-zA-Z0-9.-]+(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*$`)
	dockerImageTagPattern  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

var dockerImageDigestAlgorithms = map[string]int{
	"sha256": 64,
	"sha512": 128,
}

func containsControlCharacters(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf) || r == '\u2028' || r == '\u2029'
	}) >= 0
}

func validateContainerMountPath(containerPath string) (string, error) {
	if containerPath == "" {
		return "", errors.New("container path cannot be empty. Example: /workdir")
	}
	if containsControlCharacters(containerPath) || strings.Contains(containerPath, ":") {
		dockerArgsValidationLog.Printf("rejected container mount path with control/reserved characters: %q", containerPath)
		return "", errors.New("container path contains invalid control characters or reserved characters. Example: /workdir")
	}
	if !path.IsAbs(containerPath) {
		return "", fmt.Errorf("container path must be absolute. Example: /workdir. Got: %s", containerPath)
	}
	cleanPath := path.Clean(containerPath)
	if cleanPath != containerPath {
		return "", fmt.Errorf("container path must be normalized. Example: /workdir/config. Got: %s", containerPath)
	}
	return cleanPath, nil
}

func validateHostMountPath(hostPath string) (string, error) {
	cleanHostPath, err := fileutil.ValidateAbsolutePath(hostPath)
	if err != nil {
		dockerArgsValidationLog.Printf("host mount path %q failed validation: %v", hostPath, err)
		return "", fmt.Errorf("invalid host path %q: %w", hostPath, err)
	}
	colonPath := cleanHostPath
	if isWindowsDrivePath(cleanHostPath) {
		colonPath = cleanHostPath[2:]
	}
	if strings.Contains(colonPath, ":") {
		return "", fmt.Errorf("host path contains unsupported ':' for docker -v mount syntax. Example: /tmp/repo or C:/repo. Got: %s", cleanHostPath)
	}
	if filepath.Dir(cleanHostPath) == cleanHostPath {
		return "", fmt.Errorf("host path cannot be a filesystem root: %s", cleanHostPath)
	}
	return cleanHostPath, nil
}

func validateDockerImageRef(imageRef string) (string, error) {
	if imageRef == "" {
		return "", errors.New("docker image reference cannot be empty. Example: ghcr.io/example/image:tag")
	}
	// Image refs disallow all Unicode whitespace, while containsControlCharacters also rejects
	// non-whitespace spoofing characters such as bidi overrides and other format controls.
	if containsControlCharacters(imageRef) || strings.IndexFunc(imageRef, unicode.IsSpace) >= 0 {
		dockerArgsValidationLog.Printf("rejected docker image reference with invalid whitespace/control characters: %q", imageRef)
		return "", fmt.Errorf("docker image reference contains invalid whitespace/control characters. Example: ghcr.io/example/image:tag. Got: %q", imageRef)
	}
	if strings.HasPrefix(imageRef, "-") {
		return "", fmt.Errorf("docker image reference cannot start with '-'. Example: ghcr.io/example/image:tag. Got: %q", imageRef)
	}

	imageRefWithoutDigest := imageRef
	if strings.Count(imageRef, "@") > 1 {
		return "", fmt.Errorf("docker image reference has multiple digest separators. Example: ghcr.io/example/image@sha256:<digest>. Got: %q", imageRef)
	}
	nameWithOptionalTag, digest, hasDigest := strings.Cut(imageRef, "@")
	if hasDigest {
		if digest == "" || !isAllowedDockerImageDigest(digest) {
			return "", fmt.Errorf("docker image reference has an invalid digest format. Example: ghcr.io/example/image@sha256:<digest>. Got: %q", imageRef)
		}
		imageRefWithoutDigest = nameWithOptionalTag
	}
	if imageRefWithoutDigest == "" {
		return "", fmt.Errorf("docker image reference is missing an image name. Example: ghcr.io/example/image:tag. Got: %q", imageRef)
	}

	imageName := imageRefWithoutDigest
	if colon := strings.LastIndex(imageRefWithoutDigest, ":"); colon > strings.LastIndex(imageRefWithoutDigest, "/") {
		tag := imageRefWithoutDigest[colon+1:]
		if !dockerImageTagPattern.MatchString(tag) {
			return "", fmt.Errorf("docker image reference has an invalid tag format. Example: ghcr.io/example/image:tag. Got: %q", imageRef)
		}
		imageName = imageRefWithoutDigest[:colon]
	}

	if imageName == "" || strings.HasSuffix(imageName, "/") || !dockerImageNamePattern.MatchString(imageName) {
		return "", fmt.Errorf("docker image reference must match an allow-listed image pattern. Example: ghcr.io/example/image:tag. Got: %q", imageRef)
	}
	return imageRef, nil
}

func isAllowedDockerImageDigest(digest string) bool {
	algorithm, hexDigest, ok := strings.Cut(digest, ":")
	if !ok {
		return false
	}

	expectedLength, ok := dockerImageDigestAlgorithms[algorithm]
	if !ok || len(hexDigest) != expectedLength {
		return false
	}

	for _, r := range hexDigest {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func isWindowsDrivePath(hostPath string) bool {
	if len(hostPath) < 3 {
		return false
	}
	driveLetter := strings.ToLower(hostPath[:1])
	return strings.Contains("abcdefghijklmnopqrstuvwxyz", driveLetter) &&
		strings.HasPrefix(hostPath[1:2], ":") &&
		(strings.HasPrefix(hostPath[2:3], "\\") || strings.HasPrefix(hostPath[2:3], "/"))
}

func buildDockerVolumeMount(hostPath, containerPath string) (string, error) {
	cleanHostPath, err := validateHostMountPath(hostPath)
	if err != nil {
		return "", err
	}
	cleanContainerPath, err := validateContainerMountPath(containerPath)
	if err != nil {
		return "", err
	}
	return cleanHostPath + ":" + cleanContainerPath, nil
}

func buildDockerReadonlyFileMount(hostFile, containerPath string) (string, error) {
	cleanHostFile, err := validateHostMountPath(hostFile)
	if err != nil {
		return "", fmt.Errorf("invalid host file %q: %w", hostFile, err)
	}
	info, err := os.Stat(cleanHostFile)
	if err != nil {
		dockerArgsValidationLog.Printf("failed to stat host file %q: %v", cleanHostFile, err)
		return "", fmt.Errorf("failed to stat host file %q: %w", cleanHostFile, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("host file is not a regular file: %s", cleanHostFile)
	}
	cleanContainerPath, err := validateContainerMountPath(containerPath)
	if err != nil {
		return "", err
	}
	return cleanHostFile + ":" + cleanContainerPath + ":ro", nil
}
