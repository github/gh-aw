//go:build !integration

package workflow

import (
	"testing"
)

func TestConvertToRemoteActionRef(t *testing.T) {
	t.Run("local path with ./ prefix and version tag", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.2.3")
		data := &WorkflowData{}
		ref := compiler.convertToRemoteActionRef("./actions/create-issue", data)
		expected := "github/gh-aw/actions/create-issue@v1.2.3"
		if ref != expected {
			t.Errorf("Expected %q, got %q", expected, ref)
		}
	})

	t.Run("local path without ./ prefix and version tag", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.0.0")
		data := &WorkflowData{}
		ref := compiler.convertToRemoteActionRef("actions/create-issue", data)
		expected := "github/gh-aw/actions/create-issue@v1.0.0"
		if ref != expected {
			t.Errorf("Expected %q, got %q", expected, ref)
		}
	})

	t.Run("nested action path with version tag", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v2.0.0")
		data := &WorkflowData{}
		ref := compiler.convertToRemoteActionRef("./actions/nested/action", data)
		expected := "github/gh-aw/actions/nested/action@v2.0.0"
		if ref != expected {
			t.Errorf("Expected %q, got %q", expected, ref)
		}
	})

	t.Run("dev version returns empty", func(t *testing.T) {
		compiler := NewCompilerWithVersion("dev")
		data := &WorkflowData{}
		ref := compiler.convertToRemoteActionRef("./actions/create-issue", data)
		if ref != "" {
			t.Errorf("Expected empty string with 'dev' version, got %q", ref)
		}
	})

	t.Run("empty version returns empty", func(t *testing.T) {
		compiler := NewCompiler()
		data := &WorkflowData{}
		ref := compiler.convertToRemoteActionRef("./actions/create-issue", data)
		if ref != "" {
			t.Errorf("Expected empty string with empty version, got %q", ref)
		}
	})

	t.Run("action-tag overrides version", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.0.0")
		data := &WorkflowData{Features: map[string]any{"action-tag": "latest"}}
		ref := compiler.convertToRemoteActionRef("./actions/create-issue", data)
		expected := "github/gh-aw/actions/create-issue@latest"
		if ref != expected {
			t.Errorf("Expected %q, got %q", expected, ref)
		}
	})

	t.Run("action-tag with specific SHA", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.0.0")
		data := &WorkflowData{Features: map[string]any{"action-tag": "abc123def456"}}
		ref := compiler.convertToRemoteActionRef("./actions/setup", data)
		expected := "github/gh-aw/actions/setup@abc123def456"
		if ref != expected {
			t.Errorf("Expected %q, got %q", expected, ref)
		}
	})

	t.Run("action-tag with version tag format", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.0.0")
		data := &WorkflowData{Features: map[string]any{"action-tag": "v2.5.0"}}
		ref := compiler.convertToRemoteActionRef("./actions/setup", data)
		expected := "github/gh-aw/actions/setup@v2.5.0"
		if ref != expected {
			t.Errorf("Expected %q, got %q", expected, ref)
		}
	})

	t.Run("empty action-tag falls back to version", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.5.0")
		data := &WorkflowData{Features: map[string]any{"action-tag": ""}}
		ref := compiler.convertToRemoteActionRef("./actions/create-issue", data)
		expected := "github/gh-aw/actions/create-issue@v1.5.0"
		if ref != expected {
			t.Errorf("Expected %q, got %q", expected, ref)
		}
	})

	t.Run("nil data falls back to version", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.5.0")
		ref := compiler.convertToRemoteActionRef("./actions/create-issue", nil)
		expected := "github/gh-aw/actions/create-issue@v1.5.0"
		if ref != expected {
			t.Errorf("Expected %q, got %q", expected, ref)
		}
	})
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
			name:        "release mode with version tag",
			actionMode:  ActionModeRelease,
			localPath:   "./actions/create-issue",
			version:     "v1.0.0",
			expectedRef: "github/gh-aw/actions/create-issue@v1.0.0",
			description: "Release mode should return version-based reference",
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
			name:        "release mode with action-tag overrides version",
			actionMode:  ActionModeRelease,
			localPath:   "./actions/setup",
			version:     "v1.0.0",
			actionTag:   "latest",
			expectedRef: "github/gh-aw/actions/setup@latest",
			description: "Release mode with action-tag should use action-tag instead of version",
		},
		{
			name:        "release mode with action-tag using SHA",
			actionMode:  ActionModeRelease,
			localPath:   "./actions/setup",
			version:     "v1.0.0",
			actionTag:   "abc123def456789",
			expectedRef: "github/gh-aw/actions/setup@abc123def456789",
			description: "Release mode with action-tag SHA should use the SHA",
		},
		{
			name:        "dev mode with action-tag uses remote reference",
			actionMode:  ActionModeDev,
			localPath:   "./actions/setup",
			version:     "v1.0.0",
			actionTag:   "latest",
			expectedRef: "github/gh-aw/actions/setup@latest",
			description: "Dev mode with action-tag should override and use remote reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompilerWithVersion(tt.version)
			compiler.SetActionMode(tt.actionMode)

			data := &WorkflowData{}
			if tt.actionTag != "" {
				data.Features = map[string]any{"action-tag": tt.actionTag}
			}
			ref := compiler.resolveActionReference(tt.localPath, data)

			if tt.shouldBeEmpty {
				if ref != "" {
					t.Errorf("%s: expected empty string, got %q", tt.description, ref)
				}
			} else {
				if ref != tt.expectedRef {
					t.Errorf("%s: expected %q, got %q", tt.description, tt.expectedRef, ref)
				}
			}
		})
	}
}

func TestCompilerActionTag(t *testing.T) {
	t.Run("compiler actionTag overrides frontmatter action-tag", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.0.0")
		compiler.SetActionMode(ActionModeRelease)
		compiler.SetActionTag("v2.0.0")

		// Frontmatter has action-tag but compiler actionTag should take precedence
		data := &WorkflowData{Features: map[string]any{"action-tag": "v1.5.0"}}
		ref := compiler.convertToRemoteActionRef("./actions/setup", data)
		expected := "github/gh-aw/actions/setup@v2.0.0"
		if ref != expected {
			t.Errorf("Expected compiler actionTag to take precedence: got %q, want %q", ref, expected)
		}
	})

	t.Run("compiler actionTag overrides version", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.0.0")
		compiler.SetActionMode(ActionModeRelease)
		compiler.SetActionTag("abc123def456")

		data := &WorkflowData{}
		ref := compiler.convertToRemoteActionRef("./actions/create-issue", data)
		expected := "github/gh-aw/actions/create-issue@abc123def456"
		if ref != expected {
			t.Errorf("Expected %q, got %q", expected, ref)
		}
	})

	t.Run("compiler actionTag with dev mode forces release behavior", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.0.0")
		// When actionTag is set via --action-tag flag, setupActionMode sets mode to release
		// So this test should reflect that behavior
		compiler.SetActionTag("v2.0.0")
		compiler.SetActionMode(ActionModeRelease) // This is what setupActionMode does

		data := &WorkflowData{}
		ref := compiler.resolveActionReference("./actions/setup", data)
		expected := "github/gh-aw/actions/setup@v2.0.0"
		if ref != expected {
			t.Errorf("Expected compiler actionTag with release mode: got %q, want %q", ref, expected)
		}
	})

	t.Run("empty compiler actionTag falls back to frontmatter", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.0.0")
		compiler.SetActionMode(ActionModeRelease)
		// Don't set compiler actionTag

		data := &WorkflowData{Features: map[string]any{"action-tag": "v1.5.0"}}
		ref := compiler.convertToRemoteActionRef("./actions/setup", data)
		expected := "github/gh-aw/actions/setup@v1.5.0"
		if ref != expected {
			t.Errorf("Expected frontmatter action-tag to be used: got %q, want %q", ref, expected)
		}
	})

	t.Run("empty compiler actionTag and no frontmatter uses version", func(t *testing.T) {
		compiler := NewCompilerWithVersion("v1.2.3")
		compiler.SetActionMode(ActionModeRelease)

		data := &WorkflowData{}
		ref := compiler.convertToRemoteActionRef("./actions/setup", data)
		expected := "github/gh-aw/actions/setup@v1.2.3"
		if ref != expected {
			t.Errorf("Expected compiler version to be used: got %q, want %q", ref, expected)
		}
	})
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
			ref := ResolveSetupActionReference(tt.actionMode, tt.version, tt.actionTag, nil)
			if ref != tt.expectedRef {
				t.Errorf("%s: expected %q, got %q", tt.description, tt.expectedRef, ref)
			}
		})
	}
}

func TestResolveSetupActionReferenceWithData(t *testing.T) {
	t.Run("release mode with WorkflowData resolves SHA", func(t *testing.T) {
		// Create mock action resolver and cache
		cache := NewActionCache("")
		resolver := NewActionResolver(cache)

		data := &WorkflowData{
			ActionResolver: resolver,
			ActionCache:    cache,
			StrictMode:     false,
		}

		// The resolver will fail to resolve github/gh-aw/actions/setup@v1.0.0
		// since it's not a real tag, but it should fall back gracefully
		ref := ResolveSetupActionReference(ActionModeRelease, "v1.0.0", "", data)

		// Without a valid pin or successful resolution, should return tag-based reference
		expectedRef := "github/gh-aw/actions/setup@v1.0.0"
		if ref != expectedRef {
			t.Errorf("Expected %q, got %q", expectedRef, ref)
		}
	})

	t.Run("release mode with nil data returns tag-based reference", func(t *testing.T) {
		ref := ResolveSetupActionReference(ActionModeRelease, "v1.0.0", "", nil)
		expectedRef := "github/gh-aw/actions/setup@v1.0.0"
		if ref != expectedRef {
			t.Errorf("Expected %q, got %q", expectedRef, ref)
		}
	})
}
