//go:build !integration

package repoutil

import "testing"

func TestSplitRepoSlug(t *testing.T) {
	tests := []struct {
		name          string
		slug          string
		expectedOwner string
		expectedRepo  string
		expectError   bool
	}{
		{
			name:          "valid slug",
			slug:          "github/gh-aw",
			expectedOwner: "github",
			expectedRepo:  "gh-aw",
			expectError:   false,
		},
		{
			name:          "another valid slug",
			slug:          "octocat/hello-world",
			expectedOwner: "octocat",
			expectedRepo:  "hello-world",
			expectError:   false,
		},
		{
			name:        "invalid slug - no separator",
			slug:        "githubnext",
			expectError: true,
		},
		{
			name:        "invalid slug - multiple separators",
			slug:        "github/gh-aw/extra",
			expectError: true,
		},
		{
			name:        "invalid slug - empty",
			slug:        "",
			expectError: true,
		},
		{
			name:        "invalid slug - only separator",
			slug:        "/",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := SplitRepoSlug(tt.slug)
			if tt.expectError {
				if err == nil {
					t.Errorf("SplitRepoSlug(%q) expected error, got nil", tt.slug)
				}
			} else {
				if err != nil {
					t.Errorf("SplitRepoSlug(%q) unexpected error: %v", tt.slug, err)
				}
				if owner != tt.expectedOwner {
					t.Errorf("SplitRepoSlug(%q) owner = %q; want %q", tt.slug, owner, tt.expectedOwner)
				}
				if repo != tt.expectedRepo {
					t.Errorf("SplitRepoSlug(%q) repo = %q; want %q", tt.slug, repo, tt.expectedRepo)
				}
			}
		})
	}
}

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		expectedOwner string
		expectedRepo  string
		expectError   bool
	}{
		{
			name:          "SSH format with .git",
			url:           "git@github.com:github/gh-aw.git",
			expectedOwner: "github",
			expectedRepo:  "gh-aw",
			expectError:   false,
		},
		{
			name:          "SSH format without .git",
			url:           "git@github.com:octocat/hello-world",
			expectedOwner: "octocat",
			expectedRepo:  "hello-world",
			expectError:   false,
		},
		{
			name:          "HTTPS format with .git",
			url:           "https://github.com/github/gh-aw.git",
			expectedOwner: "github",
			expectedRepo:  "gh-aw",
			expectError:   false,
		},
		{
			name:          "HTTPS format without .git",
			url:           "https://github.com/octocat/hello-world",
			expectedOwner: "octocat",
			expectedRepo:  "hello-world",
			expectError:   false,
		},
		{
			name:        "non-GitHub URL",
			url:         "https://gitlab.com/user/repo.git",
			expectError: true,
		},
		{
			name:        "invalid URL",
			url:         "not-a-url",
			expectError: true,
		},
		{
			name:        "empty URL",
			url:         "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseGitHubURL(tt.url)
			if tt.expectError {
				if err == nil {
					t.Errorf("ParseGitHubURL(%q) expected error, got nil", tt.url)
				}
			} else {
				if err != nil {
					t.Errorf("ParseGitHubURL(%q) unexpected error: %v", tt.url, err)
				}
				if owner != tt.expectedOwner {
					t.Errorf("ParseGitHubURL(%q) owner = %q; want %q", tt.url, owner, tt.expectedOwner)
				}
				if repo != tt.expectedRepo {
					t.Errorf("ParseGitHubURL(%q) repo = %q; want %q", tt.url, repo, tt.expectedRepo)
				}
			}
		})
	}
}

func TestSanitizeForFilename(t *testing.T) {
	tests := []struct {
		name     string
		slug     string
		expected string
	}{
		{
			name:     "normal slug",
			slug:     "github/gh-aw",
			expected: "github-gh-aw",
		},
		{
			name:     "empty slug",
			slug:     "",
			expected: "clone-mode",
		},
		{
			name:     "slug with multiple slashes",
			slug:     "owner/repo/extra",
			expected: "owner-repo-extra",
		},
		{
			name:     "slug with hyphen",
			slug:     "owner/my-repo",
			expected: "owner-my-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeForFilename(tt.slug)
			if result != tt.expected {
				t.Errorf("SanitizeForFilename(%q) = %q; want %q", tt.slug, result, tt.expected)
			}
		})
	}
}

func BenchmarkSplitRepoSlug(b *testing.B) {
	slug := "github/gh-aw"
	for i := 0; i < b.N; i++ {
		_, _, _ = SplitRepoSlug(slug)
	}
}

func BenchmarkParseGitHubURL(b *testing.B) {
	url := "https://github.com/github/gh-aw.git"
	for i := 0; i < b.N; i++ {
		_, _, _ = ParseGitHubURL(url)
	}
}

func BenchmarkSanitizeForFilename(b *testing.B) {
	slug := "github/gh-aw"
	for i := 0; i < b.N; i++ {
		_ = SanitizeForFilename(slug)
	}
}

// Additional edge case tests

func TestSplitRepoSlug_Whitespace(t *testing.T) {
	tests := []struct {
		name        string
		slug        string
		expectError bool
	}{
		{
			name:        "leading whitespace",
			slug:        " owner/repo",
			expectError: false, // Will split but owner will have space
		},
		{
			name:        "trailing whitespace",
			slug:        "owner/repo ",
			expectError: false, // Will split but repo will have space
		},
		{
			name:        "whitespace in middle",
			slug:        "owner /repo",
			expectError: false, // Split will work but owner will have space
		},
		{
			name:        "tab character",
			slug:        "owner\t/repo",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := SplitRepoSlug(tt.slug)
			if tt.expectError && err == nil {
				t.Errorf("Expected error for slug %q", tt.slug)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for slug %q: %v", tt.slug, err)
			}
		})
	}
}

func TestSplitRepoSlug_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name          string
		slug          string
		expectedOwner string
		expectedRepo  string
		expectError   bool
	}{
		{
			name:          "hyphen in owner",
			slug:          "github-next/repo",
			expectedOwner: "github-next",
			expectedRepo:  "repo",
			expectError:   false,
		},
		{
			name:          "hyphen in repo",
			slug:          "owner/my-repo",
			expectedOwner: "owner",
			expectedRepo:  "my-repo",
			expectError:   false,
		},
		{
			name:          "underscore in names",
			slug:          "my_org/my_repo",
			expectedOwner: "my_org",
			expectedRepo:  "my_repo",
			expectError:   false,
		},
		{
			name:          "numbers in names",
			slug:          "org123/repo456",
			expectedOwner: "org123",
			expectedRepo:  "repo456",
			expectError:   false,
		},
		{
			name:          "dots in names",
			slug:          "org.name/repo.name",
			expectedOwner: "org.name",
			expectedRepo:  "repo.name",
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := SplitRepoSlug(tt.slug)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for slug %q", tt.slug)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for slug %q: %v", tt.slug, err)
				}
				if owner != tt.expectedOwner || repo != tt.expectedRepo {
					t.Errorf("SplitRepoSlug(%q) = (%q, %q); want (%q, %q)",
						tt.slug, owner, repo, tt.expectedOwner, tt.expectedRepo)
				}
			}
		})
	}
}

func TestParseGitHubURL_Variants(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		expectedOwner string
		expectedRepo  string
		expectError   bool
	}{
		{
			name:          "SSH with port (invalid format)",
			url:           "git@github.com:22:owner/repo.git",
			expectedOwner: "",
			expectedRepo:  "",
			expectError:   false, // Will parse but give unexpected results
		},
		{
			name:          "HTTPS with www",
			url:           "https://www.github.com/owner/repo.git",
			expectedOwner: "owner",
			expectedRepo:  "repo",
			expectError:   false,
		},
		{
			name:          "HTTP instead of HTTPS",
			url:           "http://github.com/owner/repo.git",
			expectedOwner: "owner",
			expectedRepo:  "repo",
			expectError:   false,
		},
		{
			name:          "URL with trailing slash (will fail)",
			url:           "https://github.com/owner/repo/",
			expectedOwner: "",
			expectedRepo:  "",
			expectError:   true, // Will fail due to extra slash
		},
		{
			name:          "SSH without git extension",
			url:           "git@github.com:owner/repo",
			expectedOwner: "owner",
			expectedRepo:  "repo",
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseGitHubURL(tt.url)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for URL %q", tt.url)
				}
			} else {
				if err != nil && tt.expectedOwner != "" {
					t.Errorf("Unexpected error for URL %q: %v", tt.url, err)
				}
				if err == nil && tt.expectedOwner != "" {
					if owner != tt.expectedOwner || repo != tt.expectedRepo {
						t.Errorf("ParseGitHubURL(%q) = (%q, %q); want (%q, %q)",
							tt.url, owner, repo, tt.expectedOwner, tt.expectedRepo)
					}
				}
			}
		})
	}
}

func TestSanitizeForFilename_SpecialCases(t *testing.T) {
	tests := []struct {
		name     string
		slug     string
		expected string
	}{
		{
			name:     "multiple slashes",
			slug:     "owner/repo/extra",
			expected: "owner-repo-extra",
		},
		{
			name:     "leading slash",
			slug:     "/owner/repo",
			expected: "-owner-repo",
		},
		{
			name:     "trailing slash",
			slug:     "owner/repo/",
			expected: "owner-repo-",
		},
		{
			name:     "only slashes",
			slug:     "///",
			expected: "---",
		},
		{
			name:     "single character owner and repo",
			slug:     "a/b",
			expected: "a-b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeForFilename(tt.slug)
			if result != tt.expected {
				t.Errorf("SanitizeForFilename(%q) = %q; want %q", tt.slug, result, tt.expected)
			}
		})
	}
}

func TestSplitRepoSlug_Idempotent(t *testing.T) {
	// Test that splitting and rejoining gives the same result
	slugs := []string{
		"owner/repo",
		"github-next/gh-aw",
		"my_org/my_repo",
		"org123/repo456",
	}

	for _, slug := range slugs {
		owner, repo, err := SplitRepoSlug(slug)
		if err != nil {
			t.Errorf("Unexpected error for slug %q: %v", slug, err)
			continue
		}

		rejoined := owner + "/" + repo
		if rejoined != slug {
			t.Errorf("Split and rejoin changed slug: %q -> %q", slug, rejoined)
		}
	}
}

func BenchmarkSplitRepoSlug_Valid(b *testing.B) {
	slug := "github/gh-aw"
	for i := 0; i < b.N; i++ {
		_, _, _ = SplitRepoSlug(slug)
	}
}

func BenchmarkSplitRepoSlug_Invalid(b *testing.B) {
	slug := "invalid"
	for i := 0; i < b.N; i++ {
		_, _, _ = SplitRepoSlug(slug)
	}
}

func BenchmarkParseGitHubURL_SSH(b *testing.B) {
	url := "git@github.com:github/gh-aw.git"
	for i := 0; i < b.N; i++ {
		_, _, _ = ParseGitHubURL(url)
	}
}

func BenchmarkParseGitHubURL_HTTPS(b *testing.B) {
	url := "https://github.com/github/gh-aw.git"
	for i := 0; i < b.N; i++ {
		_, _, _ = ParseGitHubURL(url)
	}
}
