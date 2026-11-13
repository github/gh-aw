// Package semverutil provides shared semantic versioning primitives used across
// the pkg/workflow and pkg/cli packages. Centralizing these helpers ensures that
// semver parsing, comparison, and compatibility logic is fixed in one place.
//
// Both workflow and cli packages previously duplicated the "ensure v-prefix" pattern
// and independently called golang.org/x/mod/semver. This package provides the
// canonical implementations so both packages can delegate here.
package semverutil

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"golang.org/x/mod/semver"
)

var semverLog = logger.New("semverutil:semverutil")

// actionVersionTagRegex matches version tags: vmajor, vmajor.minor, or vmajor.minor.patch.
// It intentionally excludes prerelease and build-metadata suffixes because GitHub Actions
// version pins use only these three forms.
var actionVersionTagRegex = regexp.MustCompile(`^v[0-9]+(\.[0-9]+(\.[0-9]+)?)?$`)
var gitDescribePrereleaseRegex = regexp.MustCompile(`^[0-9]+-g[0-9a-f]+(?:-dirty)?$`)

// SemanticVersion represents a parsed semantic version.
type SemanticVersion struct {
	Major int
	Minor int
	Patch int
	Pre   string
	Raw   string
}

// EnsureVPrefix returns v with a leading "v" added if it is not already present.
// The golang.org/x/mod/semver package requires the "v" prefix; callers that may
// receive bare version strings (e.g. "1.2.3") should normalise via this helper.
func EnsureVPrefix(v string) string {
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// IsActionVersionTag reports whether s is a valid GitHub Actions version tag.
// Accepted formats are vmajor, vmajor.minor, and vmajor.minor.patch; prerelease
// and build-metadata suffixes are not accepted.
func IsActionVersionTag(s string) bool {
	return actionVersionTagRegex.MatchString(s)
}

// IsValid reports whether ref is a valid semantic version string.
// It uses golang.org/x/mod/semver and accepts any valid semver, including
// prerelease and build-metadata suffixes. A bare version without a leading "v"
// is also accepted (the prefix is added internally).
func IsValid(ref string) bool {
	return semver.IsValid(EnsureVPrefix(ref))
}

// ParseVersion parses v into a SemanticVersion.
// It returns nil if v is not a valid semantic version string.
func ParseVersion(v string) *SemanticVersion {
	semverLog.Printf("Parsing semantic version: %s", v)
	v = EnsureVPrefix(v)

	if !semver.IsValid(v) {
		semverLog.Printf("Invalid semantic version: %s", v)
		return nil
	}

	ver := &SemanticVersion{Raw: strings.TrimPrefix(v, "v")}

	// Use semver.Canonical to get normalized version
	canonical := semver.Canonical(v)

	// Strip prerelease and build metadata before splitting, since semver.Canonical
	// preserves the prerelease suffix (e.g. "v1.2.3-beta.1" stays "v1.2.3-beta.1")
	corePart := strings.TrimPrefix(canonical, "v")
	if idx := strings.IndexAny(corePart, "-+"); idx >= 0 {
		corePart = corePart[:idx]
	}
	parts := strings.Split(corePart, ".")
	// Parse the numeric components; failed conversions leave zero values, matching
	// the previous behavior where non-numeric input produced 0.
	if len(parts) >= 1 {
		if n, err := strconv.Atoi(parts[0]); err == nil {
			ver.Major = n
		}
	}
	if len(parts) >= 2 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			ver.Minor = n
		}
	}
	if len(parts) >= 3 {
		if n, err := strconv.Atoi(parts[2]); err == nil {
			ver.Patch = n
		}
	}

	// Get prerelease if any; semver.Prerelease includes the leading hyphen, strip it
	ver.Pre = strings.TrimPrefix(semver.Prerelease(v), "-")

	return ver
}

// Compare compares two semantic versions and returns 1 if v1 > v2, -1 if v1 < v2,
// or 0 if they are equal. A bare version without a leading "v" is accepted.
func Compare(v1, v2 string) int {
	v1 = EnsureVPrefix(v1)
	v2 = EnsureVPrefix(v2)

	result := semver.Compare(v1, v2)

	if result > 0 {
		semverLog.Printf("Version comparison result: %s > %s", v1, v2)
	} else if result < 0 {
		semverLog.Printf("Version comparison result: %s < %s", v1, v2)
	} else {
		semverLog.Printf("Version comparison result: %s == %s", v1, v2)
	}

	return result
}

// NormalizeGitDescribeSemver collapses local git-describe compiler versions to
// their base release tag for compatibility checks.
//
// Examples:
//   - v1.2.3-27-gabc1234      -> v1.2.3
//   - v1.2.3-27-gabc1234-dirty -> v1.2.3
//   - v1.2.3-dirty            -> v1.2.3
//   - v1.2.3-beta.1           -> v1.2.3-beta.1
func NormalizeGitDescribeSemver(v string) string {
	v = strings.TrimSpace(v)
	prefixed := EnsureVPrefix(v)
	if !semver.IsValid(prefixed) {
		return v
	}
	v = prefixed

	canonical := semver.Canonical(v)
	prerelease := strings.TrimPrefix(semver.Prerelease(canonical), "-")
	if prerelease == "dirty" || gitDescribePrereleaseRegex.MatchString(prerelease) {
		return strings.TrimSuffix(canonical, semver.Prerelease(canonical))
	}

	return canonical
}

// IsPreciseVersion returns true if the version has explicit minor and patch components
// (i.e., at least two dots in the core version, e.g. "v6.0.0" is precise, "v6" is not).
// Only core-version dots are counted; pre-release and build-metadata suffixes are excluded.
func (v *SemanticVersion) IsPreciseVersion() bool {
	versionPart := strings.TrimPrefix(v.Raw, "v")
	// Strip pre-release and build-metadata so their dots are not counted.
	if idx := strings.IndexAny(versionPart, "-+"); idx >= 0 {
		versionPart = versionPart[:idx]
	}
	dotCount := strings.Count(versionPart, ".")
	return dotCount >= 2
}

// IsNewer returns true if this version is newer than other.
// Uses Compare for proper semantic version comparison.
func (v *SemanticVersion) IsNewer(other *SemanticVersion) bool {
	return Compare(v.Raw, other.Raw) > 0
}

// IsMorePreciseVersion reports whether v1 should sort ahead of v2 in the
// action-version specificity ordering. Versions with more dot-separated
// components sort first (for example "v4.3.0" ahead of "v4"), and ties use
// lexicographic ordering. This is an ordering predicate, not a strict
// "more-precise-only" check. No validation is performed; callers should ensure
// both inputs are well-formed version tags.
func IsMorePreciseVersion(v1, v2 string) bool {
	dots1 := strings.Count(v1, ".")
	dots2 := strings.Count(v2, ".")
	if dots1 != dots2 {
		return dots1 > dots2
	}
	return v1 > v2
}

// IsCompatible reports whether pinVersion is semver-compatible with requestedVersion.
// Semver compatibility is defined as both versions sharing the same major version.
//
// Examples:
//   - IsCompatible("v5.0.0", "v5")    → true
//   - IsCompatible("v5.1.0", "v5.0.0") → true
//   - IsCompatible("v6.0.0", "v5")    → false
func IsCompatible(pinVersion, requestedVersion string) bool {
	pinVersion = EnsureVPrefix(pinVersion)
	requestedVersion = EnsureVPrefix(requestedVersion)

	// Guard: both versions must be valid semver before comparing majors.
	// semver.Major returns "" for invalid input, causing "" == "" to
	// incorrectly report two invalid versions as compatible.
	if !IsValid(pinVersion) || !IsValid(requestedVersion) {
		semverLog.Printf("IsCompatible: one or both versions are invalid: pin=%s, requested=%s",
			pinVersion, requestedVersion)
		return false
	}

	pinMajor := semver.Major(pinVersion)
	requestedMajor := semver.Major(requestedVersion)

	compatible := pinMajor == requestedMajor
	semverLog.Printf("Checking semver compatibility: pin=%s (major=%s), requested=%s (major=%s) -> %v",
		pinVersion, pinMajor, requestedVersion, requestedMajor, compatible)

	return compatible
}
