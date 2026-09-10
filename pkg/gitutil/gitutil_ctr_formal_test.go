//go:build !integration

package gitutil

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertValidationError(t *testing.T, err error, input, errContains string) {
	t.Helper()
	require.Error(t, err)
	require.ErrorContains(t, err, errContains)
	if input != "" {
		assert.ErrorContains(t, err, fmt.Sprintf("%q", input))
	}
}

func TestValidateGitRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		ref         string
		errContains string
	}{
		{name: "valid branch name", ref: "main"},
		{name: "valid tag name", ref: "v1.2.3"},
		{name: "valid SHA", ref: "abcdef0123456789abcdef0123456789abcdef01"},
		{name: "valid branch with slash", ref: "feature/my-branch"},
		{name: "empty ref is rejected", errContains: "must not be empty"},
		{name: "leading dash is rejected", ref: "-evil", errContains: "must not start with '-'"},
		{name: "leading double dash is rejected", ref: "--upload-pack=malicious", errContains: "must not start with '-'"},
		{name: "NUL byte is rejected", ref: "main\x00evil", errContains: "must not contain NUL bytes"},
		{name: "dotdot is rejected", ref: "main..evil", errContains: "must not contain '..'"},
		{name: "dotdot prefix is rejected", ref: "..evil", errContains: "must not contain '..'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateGitRef(tt.ref)
			if tt.errContains == "" {
				require.NoError(t, err)
				return
			}
			assertValidationError(t, err, tt.ref, tt.errContains)
		})
	}
}

func TestValidateGitPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		path        string
		errContains string
	}{
		{name: "valid workflow path", path: ".github/workflows/workflow.md"},
		{name: "valid filename", path: "file.md"},
		{name: "valid nested path", path: "docs/spec.md"},
		{name: "empty path is rejected", errContains: "must not be empty"},
		{name: "leading dash is rejected", path: "-evil", errContains: "must not start with '-'"},
		{name: "leading double dash is rejected", path: "--output=/etc/passwd", errContains: "must not start with '-'"},
		{name: "absolute path is rejected", path: "/etc/passwd", errContains: "must not be absolute"},
		{name: "path traversal is rejected", path: "../etc/passwd", errContains: "must not contain '..' path traversal"},
		{name: "nested path traversal is rejected", path: "dir/../../etc/passwd", errContains: "must not contain '..' path traversal"},
		{name: "NUL byte is rejected", path: "docs/spec.md\x00evil", errContains: "must not contain NUL bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateGitPath(tt.path)
			if tt.errContains == "" {
				require.NoError(t, err)
				return
			}
			assertValidationError(t, err, tt.path, tt.errContains)
		})
	}
}
