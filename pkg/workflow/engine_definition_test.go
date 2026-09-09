//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var knownEngineImportsTestMu sync.Mutex

const knownEngineImportTestRef = "e3caca541fa7311d84947e25d3a7bfe4be3b431f"

func withKnownEngineImportsForTest(t *testing.T, content []byte, downloadErr error) {
	t.Helper()
	knownEngineImportsTestMu.Lock()
	cleanupRegistered := false
	defer func() {
		if !cleanupRegistered {
			knownEngineImportsTestMu.Unlock()
		}
	}()

	knownEngineImportsMu.Lock()
	defer knownEngineImportsMu.Unlock()

	originalDownload := knownEngineImportsDownload
	originalVersion := GetVersion()

	knownEngineImportsDownload = func(context.Context) ([]byte, error) {
		return content, downloadErr
	}
	knownEngineImportsLoaded = false
	knownEngineImports = nil
	SetVersion(knownEngineImportTestRef)

	t.Cleanup(func() {
		knownEngineImportsMu.Lock()
		defer knownEngineImportsMu.Unlock()

		knownEngineImportsDownload = originalDownload
		knownEngineImportsLoaded = false
		knownEngineImports = nil
		SetVersion(originalVersion)
		knownEngineImportsTestMu.Unlock()
	})
	cleanupRegistered = true
}

// TestNewEngineCatalog_BuiltIns checks that all built-in engines are registered
// and resolve to the expected runtime adapters.
func TestNewEngineCatalog_BuiltIns(t *testing.T) {
	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	tests := []struct {
		engineID    string
		displayName string
		provider    string
		mcp         *bool
	}{
		{"claude", "Claude Code", "anthropic", nil},
		{"codex", "Codex", "openai", nil},
		{"copilot", "GitHub Copilot CLI", "github", nil},
		{"gemini", "Google Gemini CLI", "google", nil},
		{"pi", "Pi", "github", boolPtr(false)},
	}

	for _, tt := range tests {
		t.Run(tt.engineID, func(t *testing.T) {
			resolved, err := catalog.Resolve(tt.engineID, &EngineConfig{ID: tt.engineID})
			require.NoError(t, err, "expected %s to resolve without error", tt.engineID)
			require.NotNil(t, resolved, "expected non-nil ResolvedEngineTarget for %s", tt.engineID)

			assert.Equal(t, tt.engineID, resolved.Definition.ID, "Definition.ID should match")
			assert.Equal(t, tt.displayName, resolved.Definition.DisplayName, "Definition.DisplayName should match")
			assert.Equal(t, tt.provider, resolved.Definition.Provider.Name, "Definition.Provider.Name should match")
			assert.Equal(t, tt.engineID, resolved.Runtime.GetID(), "Runtime.GetID() should match engine ID")
			assert.Equal(t, tt.mcp, resolved.Definition.MCP, "Definition.MCP should match")
		})
	}
}

// TestEngineCatalog_Resolve_LegacyStringFormat verifies that resolving via plain string
// ("engine: claude") and object ID format ("engine.id: claude") produce the same runtime.
func TestEngineCatalog_Resolve_LegacyStringFormat(t *testing.T) {
	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	// Simulate "engine: claude" — EngineConfig built from string
	stringConfig := &EngineConfig{ID: "claude"}
	resolvedString, err := catalog.Resolve("claude", stringConfig)
	require.NoError(t, err, "string-format engine should resolve without error")

	// Simulate "engine:\n  id: claude" — same logical ID
	objectConfig := &EngineConfig{ID: "claude"}
	resolvedObject, err := catalog.Resolve("claude", objectConfig)
	require.NoError(t, err, "object-format engine should resolve without error")

	assert.Equal(t, resolvedString.Runtime.GetID(), resolvedObject.Runtime.GetID(),
		"both formats should resolve to the same runtime adapter")
	assert.Equal(t, resolvedString.Definition.ID, resolvedObject.Definition.ID,
		"both formats should resolve to the same definition")
}

// TestEngineCatalog_Resolve_PrefixFallback verifies backward-compat prefix matching
// (e.g. "codex-experimental" should resolve to the codex runtime).
func TestEngineCatalog_Resolve_PrefixFallback(t *testing.T) {
	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	resolved, err := catalog.Resolve("codex-experimental", &EngineConfig{ID: "codex-experimental"})
	require.NoError(t, err, "prefix-matched engine should resolve without error")
	require.NotNil(t, resolved, "expected non-nil ResolvedEngineTarget for prefix match")

	assert.Equal(t, "codex", resolved.Runtime.GetID(), "prefix match should resolve to codex runtime")
}

// TestEngineCatalog_Resolve_UnknownEngine verifies that unknown engine IDs return a
// descriptive validation error containing the engine ID, a list of valid engines, and
// the documentation URL.
func TestEngineCatalog_Resolve_UnknownEngine(t *testing.T) {
	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	_, err := catalog.Resolve("nonexistent-engine", &EngineConfig{ID: "nonexistent-engine"})
	require.Error(t, err, "unknown engine should return an error")
	require.ErrorContains(t, err, "invalid engine",
		"error should mention 'invalid engine', got: %s", err.Error())
	require.ErrorContains(t, err, "nonexistent-engine",
		"error should mention the unknown engine ID, got: %s", err.Error())
	require.ErrorContains(t, err, string(constants.DocsEnginesURL),
		"error should include the engines documentation URL, got: %s", err.Error())
	require.ErrorContains(t, err, "engine: copilot",
		"error should include an example, got: %s", err.Error())
}

// TestEngineCatalog_Resolve_KnownImportTip verifies that a helpful import tip is included
// in the error when a known (but not built-in) engine is referenced without importing
// its shared engine definition file first.
func TestEngineCatalog_Resolve_KnownImportTip(t *testing.T) {
	withKnownEngineImportsForTest(t, []byte(`{
		"engines": [
			{"id": "opencode", "import": "github/gh-aw/.github/workflows/shared/opencode.md"},
			{"id": "crush", "import": "github/gh-aw/.github/workflows/shared/crush.md"},
			{"id": "cursor", "import": "github/gh-aw/.github/workflows/shared/cursor.md"},
			{"id": "deepseek-harness", "import": "github/gh-aw/.github/workflows/shared/deepseek-harness.md"},
			{"id": "aider", "import": "github/gh-aw/.github/workflows/shared/aider.md"},
			{"id": "goose", "import": "github/gh-aw/.github/workflows/shared/goose.md"},
			{"id": "kiro", "import": "github/gh-aw/.github/workflows/shared/kiro.md"},
			{"id": "pydantic-ai", "import": "pydantic/pydantic-ai-harness/gh-aw/pydantic.md"},
			{"id": "custom", "import": "github/gh-aw/.github/workflows/shared/genaiscript.md"}
		]
	}`), nil)

	tests := []struct {
		name           string
		engineID       string
		wantImportPath string
	}{
		{
			name:           "opencode tip",
			engineID:       "opencode",
			wantImportPath: "github/gh-aw/.github/workflows/shared/opencode.md@" + knownEngineImportTestRef,
		},
		{
			name:           "opencode case-insensitive tip",
			engineID:       "OpenCode",
			wantImportPath: "github/gh-aw/.github/workflows/shared/opencode.md@" + knownEngineImportTestRef,
		},
		{
			name:           "crush tip",
			engineID:       "crush",
			wantImportPath: "github/gh-aw/.github/workflows/shared/crush.md@" + knownEngineImportTestRef,
		},
		{
			name:           "crush case-insensitive tip",
			engineID:       "CRUSH",
			wantImportPath: "github/gh-aw/.github/workflows/shared/crush.md@" + knownEngineImportTestRef,
		},
		{
			name:           "cursor tip",
			engineID:       "cursor",
			wantImportPath: "github/gh-aw/.github/workflows/shared/cursor.md@" + knownEngineImportTestRef,
		},
		{
			name:           "deepseek-harness tip",
			engineID:       "deepseek-harness",
			wantImportPath: "github/gh-aw/.github/workflows/shared/deepseek-harness.md@" + knownEngineImportTestRef,
		},
		{
			name:           "aider tip",
			engineID:       "aider",
			wantImportPath: "github/gh-aw/.github/workflows/shared/aider.md@" + knownEngineImportTestRef,
		},
		{
			name:           "goose tip",
			engineID:       "goose",
			wantImportPath: "github/gh-aw/.github/workflows/shared/goose.md@" + knownEngineImportTestRef,
		},
		{
			name:           "kiro tip",
			engineID:       "kiro",
			wantImportPath: "github/gh-aw/.github/workflows/shared/kiro.md@" + knownEngineImportTestRef,
		},
		{
			name:           "pydantic-ai tip",
			engineID:       "pydantic-ai",
			wantImportPath: "pydantic/pydantic-ai-harness/gh-aw/pydantic.md@main",
		},
		{
			name:           "custom tip",
			engineID:       "custom",
			wantImportPath: "github/gh-aw/.github/workflows/shared/genaiscript.md@" + knownEngineImportTestRef,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewEngineRegistry()
			catalog := NewEngineCatalog(registry)

			_, err := catalog.Resolve(tt.engineID, &EngineConfig{ID: tt.engineID})
			require.Error(t, err)
			require.ErrorContains(t, err, "invalid engine")
			require.ErrorContains(t, err, tt.engineID)
			require.ErrorContains(t, err, "Tip:", "error should contain an import tip for known engine")
			require.ErrorContains(t, err, tt.wantImportPath, "tip should reference the known import path")
			require.ErrorContains(t, err, "imports:", "tip should show an imports example")
		})
	}
}

// TestEngineCatalog_Resolve_UnknownNoTip verifies that truly unknown engines (not in
// the known-engine import catalog) do NOT include an import tip.
func TestEngineCatalog_Resolve_UnknownNoTip(t *testing.T) {
	withKnownEngineImportsForTest(t, []byte(`{
		"engines": [
			{"id": "opencode", "import": "github/gh-aw/.github/workflows/shared/opencode.md"}
		]
	}`), nil)

	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	_, err := catalog.Resolve("totally-unknown-engine", nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid engine")
	require.NotContains(t, err.Error(), "Tip:", "error should not contain a tip for truly unknown engines")
}

// TestEngineCatalog_Resolve_KnownImportDownloadFailureNoTip verifies that the
// remote known-engine catalog fails closed without changing the validation error.
func TestEngineCatalog_Resolve_KnownImportDownloadFailureNoTip(t *testing.T) {
	withKnownEngineImportsForTest(t, nil, errors.New("network unavailable"))

	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	_, err := catalog.Resolve("opencode", nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid engine")
	require.NotContains(t, err.Error(), "Tip:", "download failures should be silent")
}

func TestKnownEngineImportsDownload_UsesRawGitHubURL(t *testing.T) {
	knownEngineImportsTestMu.Lock()
	defer knownEngineImportsTestMu.Unlock()

	originalBaseURL := knownEngineImportsRawBaseURL
	originalHTTPClient := knownEngineImportsHTTPClient
	t.Cleanup(func() {
		knownEngineImportsRawBaseURL = originalBaseURL
		knownEngineImportsHTTPClient = originalHTTPClient
	})

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"engines":[]}`))
	}))
	defer server.Close()

	knownEngineImportsRawBaseURL = server.URL
	knownEngineImportsHTTPClient = server.Client

	content, err := knownEngineImportsDownload(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"engines":[]}`, string(content))
	require.Equal(t, "/github/gh-aw/refs/heads/main/.github/aw/engines.json", gotPath)
}

func TestKnownEngineImportWithCompilerRef(t *testing.T) {
	knownEngineImportsTestMu.Lock()
	defer knownEngineImportsTestMu.Unlock()

	originalAPIBaseURL := knownEngineImportsAPIBaseURL
	originalHTTPClient := knownEngineImportsHTTPClient
	originalVersion := GetVersion()
	t.Cleanup(func() {
		knownEngineImportsAPIBaseURL = originalAPIBaseURL
		knownEngineImportsHTTPClient = originalHTTPClient
		SetVersion(originalVersion)
	})
	SetVersion(knownEngineImportTestRef)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo" {
			t.Errorf("request path = %q, want %q", r.URL.Path, "/repos/owner/repo")
		}
		_, _ = w.Write([]byte(`{"default_branch":"trunk"}`))
	}))
	defer server.Close()

	knownEngineImportsAPIBaseURL = server.URL
	knownEngineImportsHTTPClient = server.Client

	tests := []struct {
		name       string
		importPath string
		want       string
	}{
		{
			name:       "already pinned",
			importPath: "owner/repo/path/to/definition.md@v1",
			want:       "owner/repo/path/to/definition.md@v1",
		},
		{
			name:       "github gh-aw gets compiler ref",
			importPath: GitHubOrgRepo + "/.github/workflows/shared/example.md",
			want:       GitHubOrgRepo + "/.github/workflows/shared/example.md@" + knownEngineImportTestRef,
		},
		{
			name:       "foreign repository gets target default branch",
			importPath: "owner/repo/path/to/definition.md",
			want:       "owner/repo/path/to/definition.md@trunk",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, knownEngineImportWithCompilerRef(context.Background(), tt.importPath))
		})
	}
}

func TestKnownEngineImportsFile_MatchesSharedEngineFiles(t *testing.T) {
	catalogPath := filepath.Join("..", "..", knownEngineImportsPath)
	content, err := os.ReadFile(catalogPath)
	require.NoError(t, err)

	var catalog knownEngineImportsFile
	require.NoError(t, json.Unmarshal(content, &catalog))

	byID := map[string]string{}
	for _, engine := range catalog.Engines {
		require.NotEmpty(t, engine.ID)
		require.NotEmpty(t, engine.Import)
		require.NotContains(t, engine.Import, "@", "known engine imports should leave ref pinning to the CLI")
		byID[engine.ID] = engine.Import
	}

	registry := NewEngineRegistry()
	for _, id := range registry.GetSupportedEngines() {
		require.NotContains(t, byID, id, "known external engine catalog should not duplicate built-ins")
	}

	sharedDir := filepath.Join("..", "..", ".github", "workflows", "shared")
	sharedEngines := sharedExternalEngines(t, sharedDir, registry.GetSupportedEngines())
	for id, relPath := range sharedEngines {
		importPath, ok := byID[id]
		require.True(t, ok, "shared external engine %q in %s should be listed in %s", id, relPath, knownEngineImportsPath)
		require.Contains(t, importPath, relPath, "import for %q should reference its shared file", id)
	}
}

func sharedExternalEngines(t *testing.T, sharedDir string, builtIns []string) map[string]string {
	t.Helper()

	builtInSet := map[string]bool{}
	for _, id := range builtIns {
		builtInSet[id] = true
	}

	files, err := filepath.Glob(filepath.Join(sharedDir, "*.md"))
	require.NoError(t, err)

	engines := map[string]string{}
	engineObjectID := regexp.MustCompile(`(?ms)^engine:\s*\n(?:  .*\n)*?  id:\s*"?([^"\s#]+)"?`)
	engineInlineID := regexp.MustCompile(`(?m)^engine:\s*"?([^"\s#]+)"?`)
	for _, file := range files {
		content, err := os.ReadFile(file)
		require.NoError(t, err)

		id := ""
		if match := engineObjectID.FindSubmatch(content); match != nil {
			id = string(match[1])
		} else if match := engineInlineID.FindSubmatch(content); match != nil {
			id = string(match[1])
		}
		if id == "" || builtInSet[id] {
			continue
		}

		relPath, err := filepath.Rel(filepath.Join("..", ".."), file)
		require.NoError(t, err)
		engines[id] = filepath.ToSlash(relPath)
	}

	return engines
}

// TestEngineCatalog_Resolve_ConfigPassthrough verifies that the EngineConfig passed to
// Resolve is surfaced unchanged in the ResolvedEngineTarget.
func TestEngineCatalog_Resolve_ConfigPassthrough(t *testing.T) {
	registry := NewEngineRegistry()
	catalog := NewEngineCatalog(registry)

	cfg := &EngineConfig{ID: "copilot", MaxTurns: "10"}
	resolved, err := catalog.Resolve("copilot", cfg)
	require.NoError(t, err, "copilot with config should resolve without error")
	assert.Equal(t, cfg, resolved.Config, "resolved Config should be the same pointer passed in")
}

// TestEngineCatalog_Register_Custom verifies that a custom engine definition can be
// registered and resolved via the catalog.
func TestEngineCatalog_Register_Custom(t *testing.T) {
	registry := NewEngineRegistry()
	// Register a test engine in the registry so the catalog can look it up
	require.NoError(t, registry.Register(NewCopilotEngine()), "copilot engine should register without error") // reuse copilot as the backing runtime

	catalog := NewEngineCatalog(registry)
	catalog.Register(&EngineDefinition{
		ID:          "my-custom-engine",
		DisplayName: "My Custom Engine",
		Description: "A custom engine for testing",
		RuntimeID:   "copilot", // backed by copilot runtime
		Provider:    ProviderSelection{Name: "custom"},
	})

	resolved, err := catalog.Resolve("my-custom-engine", &EngineConfig{ID: "my-custom-engine"})
	require.NoError(t, err, "custom engine should resolve without error")
	assert.Equal(t, "my-custom-engine", resolved.Definition.ID, "custom engine definition ID should match")
	assert.Equal(t, "copilot", resolved.Runtime.GetID(), "custom engine should use copilot runtime")
}
