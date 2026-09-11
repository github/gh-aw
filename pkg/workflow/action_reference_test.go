//go:build !integration

package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type staticSHAResolver struct {
	sha string
	err error
}

func (r staticSHAResolver) ResolveSHA(context.Context, string, string) (string, error) {
	return r.sha, r.err
}

func TestConvertToRemoteActionRef(t *testing.T) {
	tests := []struct {
		name          string
		version       string
		actionTag     string
		setEmptyTag   bool
		localPath     string
		nilData       bool
		expectedRef   string
		shouldBeEmpty bool
	}{
		{
			name:        "local path with ./ prefix and version tag",
			version:     "v1.2.3",
			localPath:   "./actions/create-issue",
			expectedRef: "github/gh-aw/actions/create-issue@v1.2.3",
		},
		{
			name:        "local path without ./ prefix and version tag",
			version:     "v1.0.0",
			localPath:   "actions/create-issue",
			expectedRef: "github/gh-aw/actions/create-issue@v1.0.0",
		},
		{
			name:        "nested action path with version tag",
			version:     "v2.0.0",
			localPath:   "./actions/nested/action",
			expectedRef: "github/gh-aw/actions/nested/action@v2.0.0",
		},
		{
			name:          "dev version returns empty",
			version:       "dev",
			localPath:     "./actions/create-issue",
			shouldBeEmpty: true,
		},
		{
			name:          "empty version returns empty",
			version:       "",
			localPath:     "./actions/create-issue",
			shouldBeEmpty: true,
		},
		{
			name:        "action-tag overrides version",
			version:     "v1.0.0",
			actionTag:   "latest",
			localPath:   "./actions/create-issue",
			expectedRef: "github/gh-aw/actions/create-issue@latest",
		},
		{
			name:        "action-tag with specific SHA",
			version:     "v1.0.0",
			actionTag:   "abc123def456",
			localPath:   "./actions/setup",
			expectedRef: "github/gh-aw/actions/setup@abc123def456",
		},
		{
			name:        "action-tag with version tag format",
			version:     "v1.0.0",
			actionTag:   "v2.5.0",
			localPath:   "./actions/setup",
			expectedRef: "github/gh-aw/actions/setup@v2.5.0",
		},
		{
			name:        "empty action-tag falls back to version",
			version:     "v1.5.0",
			setEmptyTag: true,
			localPath:   "./actions/create-issue",
			expectedRef: "github/gh-aw/actions/create-issue@v1.5.0",
		},
		{
			name:        "nil data falls back to version",
			version:     "v1.5.0",
			nilData:     true,
			localPath:   "./actions/create-issue",
			expectedRef: "github/gh-aw/actions/create-issue@v1.5.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithVersion(tt.version))

			var data *WorkflowData
			if !tt.nilData {
				data = &WorkflowData{}
				if tt.actionTag != "" {
					data.Features = map[string]any{"action-tag": tt.actionTag}
				} else if tt.setEmptyTag {
					data.Features = map[string]any{"action-tag": ""}
				}
			}

			ref := compiler.convertToRemoteActionRef(tt.localPath, data)

			if tt.shouldBeEmpty {
				assert.Empty(t, ref, "should return empty string for invalid/dev version")
			} else {
				assert.Equal(t, tt.expectedRef, ref, "should construct correct remote reference")
			}
		})
	}
}

func TestResolveActionReference(t *testing.T) {
	tests := []struct {
		name          string
		actionMode    ActionMode
		localPath     string
		version       string
		actionTag     string
		expectedRef   string
		shouldBeEmpty bool
		description   string
	}{
		{
			name:        "dev mode",
			actionMode:  ActionModeDev,
			localPath:   "./actions/create-issue",
			version:     "v1.0.0",
			expectedRef: "./actions/create-issue",
			description: "Dev mode should return local path",
		},
		{
			name:          "release mode with version tag and unresolved pin",
			actionMode:    ActionModeRelease,
			localPath:     "./actions/create-issue",
			version:       "v1.0.0",
			shouldBeEmpty: true,
			description:   "Release mode should fail closed when action cannot be pinned",
		},
		{
			name:          "release mode with dev version",
			actionMode:    ActionModeRelease,
			localPath:     "./actions/create-issue",
			version:       "dev",
			shouldBeEmpty: true,
			description:   "Release mode with 'dev' version should return empty",
		},
		{
			name:          "release mode with unresolved action-tag fails closed",
			actionMode:    ActionModeRelease,
			localPath:     "./actions/setup",
			version:       "v1.0.0",
			actionTag:     "latest",
			shouldBeEmpty: true,
			description:   "Frontmatter action-tag should use strict action mode and fail closed when unresolved",
		},
		{
			name:          "release mode with short action-tag SHA fails closed",
			actionMode:    ActionModeRelease,
			localPath:     "./actions/setup",
			version:       "v1.0.0",
			actionTag:     "abc123def456789",
			shouldBeEmpty: true,
			description:   "Frontmatter action-tag SHA must be a full immutable commit SHA",
		},
		{
			name:          "dev mode with unresolved action-tag fails closed",
			actionMode:    ActionModeDev,
			localPath:     "./actions/setup",
			version:       "v1.0.0",
			actionTag:     "latest",
			shouldBeEmpty: true,
			description:   "Dev mode with frontmatter action-tag should use strict action mode and fail closed when unresolved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithVersion(tt.version))
			compiler.SetActionMode(tt.actionMode)

			data := &WorkflowData{}
			if tt.actionTag != "" {
				data.Features = map[string]any{"action-tag": tt.actionTag}
			}
			ref := compiler.resolveActionReference(tt.localPath, data)

			if tt.shouldBeEmpty {
				assert.Empty(t, ref, tt.description)
			} else {
				assert.Equal(t, tt.expectedRef, ref, tt.description)
			}
		})
	}
}

func TestCompilerActionTag(t *testing.T) {
	tests := []struct {
		name              string
		version           string
		compilerActionTag string
		frontmatterTag    string
		localPath         string
		useResolve        bool
		expectedRef       string
	}{
		{
			name:              "compiler actionTag overrides frontmatter action-tag",
			version:           "v1.0.0",
			compilerActionTag: "v2.0.0",
			frontmatterTag:    "v1.5.0",
			localPath:         "./actions/setup",
			expectedRef:       "github/gh-aw/actions/setup@v2.0.0",
		},
		{
			name:              "compiler actionTag overrides version",
			version:           "v1.0.0",
			compilerActionTag: "abc123def456",
			localPath:         "./actions/create-issue",
			expectedRef:       "github/gh-aw/actions/create-issue@abc123def456",
		},
		{
			name:              "compiler actionTag with dev mode forces release behavior",
			version:           "v1.0.0",
			compilerActionTag: "v2.0.0",
			localPath:         "./actions/setup",
			useResolve:        true,
			expectedRef:       "github/gh-aw/actions/setup@v2.0.0",
		},
		{
			name:           "empty compiler actionTag falls back to frontmatter",
			version:        "v1.0.0",
			frontmatterTag: "v1.5.0",
			localPath:      "./actions/setup",
			expectedRef:    "github/gh-aw/actions/setup@v1.5.0",
		},
		{
			name:        "empty compiler actionTag and no frontmatter uses version",
			version:     "v1.2.3",
			localPath:   "./actions/setup",
			expectedRef: "github/gh-aw/actions/setup@v1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithVersion(tt.version))
			compiler.SetActionMode(ActionModeRelease)
			if tt.compilerActionTag != "" {
				compiler.SetActionTag(tt.compilerActionTag)
			}

			data := &WorkflowData{}
			if tt.frontmatterTag != "" {
				data.Features = map[string]any{"action-tag": tt.frontmatterTag}
			}

			var ref string
			if tt.useResolve {
				ref = compiler.resolveActionReference(tt.localPath, data)
			} else {
				ref = compiler.convertToRemoteActionRef(tt.localPath, data)
			}

			assert.Equal(t, tt.expectedRef, ref, "should use correct tag priority order")
		})
	}
}

func TestResolveSetupActionReference(t *testing.T) {
	tests := []struct {
		name        string
		actionMode  ActionMode
		version     string
		actionTag   string
		expectedRef string
		description string
	}{
		{
			name:        "dev mode returns local path",
			actionMode:  ActionModeDev,
			version:     "v1.0.0",
			actionTag:   "",
			expectedRef: "./actions/setup",
			description: "Dev mode should return local path",
		},
		{
			name:        "release mode with version",
			actionMode:  ActionModeRelease,
			version:     "v1.0.0",
			actionTag:   "",
			expectedRef: "github/gh-aw/actions/setup@v1.0.0",
			description: "Release mode should return remote reference with version",
		},
		{
			name:        "release mode with actionTag overrides version",
			actionMode:  ActionModeRelease,
			version:     "v1.0.0",
			actionTag:   "v2.5.0",
			expectedRef: "github/gh-aw/actions/setup@v2.5.0",
			description: "Release mode with actionTag should use actionTag instead of version",
		},
		{
			name:        "release mode with SHA actionTag",
			actionMode:  ActionModeRelease,
			version:     "v1.0.0",
			actionTag:   "abc123def456789012345678901234567890abcd",
			expectedRef: "github/gh-aw/actions/setup@abc123def456789012345678901234567890abcd",
			description: "Release mode with SHA actionTag should use the SHA",
		},
		{
			name:        "release mode with dev version falls back to local",
			actionMode:  ActionModeRelease,
			version:     "dev",
			actionTag:   "",
			expectedRef: "./actions/setup",
			description: "Release mode with 'dev' version should fall back to local path",
		},
		{
			name:        "release mode with dev version but actionTag specified",
			actionMode:  ActionModeRelease,
			version:     "dev",
			actionTag:   "v2.0.0",
			expectedRef: "github/gh-aw/actions/setup@v2.0.0",
			description: "Release mode with actionTag should work even with 'dev' version",
		},
		{
			name:        "dev mode with actionTag uses local path (actionTag not checked here)",
			actionMode:  ActionModeDev,
			version:     "v1.0.0",
			actionTag:   "v2.0.0",
			expectedRef: "./actions/setup",
			description: "Dev mode should return local path even if actionTag is specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pass nil for data to test backward compatibility with standalone usage
			ref := ResolveSetupActionReference(context.Background(), tt.actionMode, tt.version, tt.actionTag, nil)
			assert.Equal(t, tt.expectedRef, ref, tt.description)
		})
	}
}

func TestResolveSetupActionReferenceWithData(t *testing.T) {
	t.Run("release mode with resolver resolves SHA", func(t *testing.T) {
		// Create mock action resolver and cache
		cache := NewActionCache("")
		resolver := NewActionResolver(cache)

		// The resolver will fail to resolve github/gh-aw/actions/setup@v1.0.0
		// since it's not a real tag, but it should fall back gracefully
		ref := ResolveSetupActionReference(context.Background(), ActionModeRelease, "v1.0.0", "", resolver)

		// Without a valid pin or successful resolution, should return tag-based reference
		assert.Equal(t, "github/gh-aw/actions/setup@v1.0.0", ref, "should return tag-based reference when SHA resolution fails")
	})

	t.Run("release mode with nil resolver returns tag-based reference", func(t *testing.T) {
		ref := ResolveSetupActionReference(context.Background(), ActionModeRelease, "v1.0.0", "", nil)
		assert.Equal(t, "github/gh-aw/actions/setup@v1.0.0", ref, "should return tag-based reference when no resolver provided")
	})
}

func TestResolveSetupActionReferenceActionModeRequiresFullSHA(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"

	t.Run("resolver full SHA is accepted", func(t *testing.T) {
		ref := ResolveSetupActionReference(context.Background(), ActionModeAction, "v1.0.0", "", staticSHAResolver{sha: sha})
		assert.Equal(t, "github/gh-aw-actions/setup@"+sha+" # v1.0.0", ref)
	})

	t.Run("resolver short SHA is rejected", func(t *testing.T) {
		ref := ResolveSetupActionReference(context.Background(), ActionModeAction, "v1.0.0", "", staticSHAResolver{sha: "abc123"})
		assert.Empty(t, ref)
	})

	t.Run("nil resolver unresolved version is rejected", func(t *testing.T) {
		ref := ResolveSetupActionReference(context.Background(), ActionModeAction, "v1.0.0", "", nil)
		assert.Empty(t, ref)
	})

	t.Run("full SHA tag is accepted without resolver", func(t *testing.T) {
		ref := ResolveSetupActionReference(context.Background(), ActionModeAction, sha, "", nil)
		assert.Equal(t, "github/gh-aw-actions/setup@"+sha+" # "+sha, ref)
	})
}
