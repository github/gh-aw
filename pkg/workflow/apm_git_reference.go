package workflow

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var repoSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var gitRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]*$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// GitReference identifies a GitHub-hosted repository and optional primitive path.
// Other transports and hosts are deliberately not admitted by the skill backend.
type GitReference struct {
	Repository string
	Path       string
	Ref        string
}

func ParseGitHubReference(value string) (GitReference, error) {
	var result GitReference
	value = strings.TrimSpace(value)
	base, ref, hasRef := strings.Cut(value, "#")
	if hasRef {
		if !gitRefPattern.MatchString(ref) || strings.Contains(ref, "..") {
			return result, fmt.Errorf("invalid Git ref in %q", value)
		}
		result.Ref = ref
	}
	if strings.HasPrefix(base, "https://") {
		u, err := url.Parse(base)
		if err != nil || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
			return result, fmt.Errorf("unsupported GitHub URL %q", base)
		}
		base = strings.TrimPrefix(u.Path, "/")
	} else {
		base = strings.TrimPrefix(base, "github.com/")
	}
	parts := strings.Split(base, "/")
	if len(parts) < 2 {
		return result, fmt.Errorf("expected owner/repo[/path] in %q", value)
	}
	for _, part := range parts {
		if part == "." || part == ".." || !repoSegmentPattern.MatchString(part) {
			return result, fmt.Errorf("invalid repository or path segment in %q", value)
		}
	}
	if strings.Contains(parts[0], ".") { //nolint:uncheckedsliceindex // len(parts) >= 2 was verified above.
		return result, fmt.Errorf("only github.com Git sources are supported: %q", value)
	}
	result.Repository = strings.Join([]string{parts[0], strings.TrimSuffix(parts[1], ".git")}, "/") //nolint:uncheckedsliceindex // len(parts) >= 2 was verified above.
	if len(parts) > 2 {
		result.Path = strings.Join(parts[2:], "/")
	}
	return result, nil
}

func (r GitReference) identity() string {
	return strings.Join([]string{strings.ToLower(r.Repository), r.Path}, "/")
}

// SkillSpec produces gh-aw's native, immutable skill reference spelling.
func (r GitReference) SkillSpec(commit string) string {
	base := r.Repository
	if r.Path != "" {
		base = strings.Join([]string{base, r.Path}, "/")
	}
	return base + "@" + commit
}
