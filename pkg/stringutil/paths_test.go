//go:build !integration

package stringutil

import "testing"

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "a/b/c",
			expected: "a/b/c",
		},
		{
			name:     "path with single dot",
			path:     "a/./b",
			expected: "a/b",
		},
		{
			name:     "path with multiple dots",
			path:     "./a/./b/./c",
			expected: "a/b/c",
		},
		{
			name:     "path with double dot",
			path:     "a/b/../c",
			expected: "a/c",
		},
		{
			name:     "path with multiple double dots",
			path:     "a/b/../../c",
			expected: "c",
		},
		{
			name:     "path with leading double dot",
			path:     "../a/b",
			expected: "a/b",
		},
		{
			name:     "path with trailing double dot",
			path:     "a/b/..",
			expected: "a",
		},
		{
			name:     "path with empty parts",
			path:     "a//b///c",
			expected: "a/b/c",
		},
		{
			name:     "complex path",
			path:     "a/./b/../c/d/../../e",
			expected: "a/e",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "single dot",
			path:     ".",
			expected: "",
		},
		{
			name:     "double dot only",
			path:     "..",
			expected: "",
		},
		{
			name:     "multiple double dots beyond root",
			path:     "../../a",
			expected: "a",
		},
		{
			name:     "mixed slashes and dots",
			path:     "a/b/./c/../d",
			expected: "a/b/d",
		},
		{
			name:     "path with only dots and slashes",
			path:     "./../.",
			expected: "",
		},
		{
			name:     "real-world bundler path",
			path:     "./lib/utils/../../helpers/common",
			expected: "helpers/common",
		},
		{
			name:     "deeply nested path with parent refs",
			path:     "a/b/c/d/../../../e/f",
			expected: "a/e/f",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePath(tt.path)
			if result != tt.expected {
				t.Errorf("NormalizePath(%q) = %q; want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func BenchmarkNormalizePath(b *testing.B) {
	path := "a/b/c/./d/../e/f/../../g"
	for i := 0; i < b.N; i++ {
		NormalizePath(path)
	}
}

func BenchmarkNormalizePath_Simple(b *testing.B) {
	path := "a/b/c/d/e"
	for i := 0; i < b.N; i++ {
		NormalizePath(path)
	}
}

func BenchmarkNormalizePath_Complex(b *testing.B) {
	path := "./a/./b/../c/d/../../e/f/g/h/../../../i"
	for i := 0; i < b.N; i++ {
		NormalizePath(path)
	}
}
