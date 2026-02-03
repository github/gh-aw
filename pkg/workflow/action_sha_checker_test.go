//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestExtractActionsFromLockFile(t *testing.T) {
	// Create a temporary lock file with test content
	tmpDir := testutil.TempDir(t, "test-*")
	lockFile := filepath.Join(tmpDir, "test.lock.yml")

	lockContent := `
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd
      - uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f
      - name: Run tests
        run: npm test
      - uses: github/codeql-action/upload-sarif@ab2e54f42aa112ff08704159b88a57517f6f0ebb
`

	if err := os.WriteFile(lockFile, []byte(lockContent), 0644); err != nil {
		t.Fatalf("Failed to create test lock file: %v", err)
	}

	// Extract actions
	actions, err := ExtractActionsFromLockFile(lockFile)
	if err != nil {
		t.Fatalf("ExtractActionsFromLockFile failed: %v", err)
	}

	// Verify we extracted the expected actions
	if len(actions) != 3 {
		t.Errorf("Expected 3 actions, got %d", len(actions))
	}

	// Check that we have the expected repositories
	expectedRepos := map[string]bool{
		"actions/checkout":                  false,
		"actions/setup-node":                false,
		"github/codeql-action/upload-sarif": false,
	}

	for _, action := range actions {
		if _, exists := expectedRepos[action.Repo]; exists {
			expectedRepos[action.Repo] = true
		}
	}

	for repo, found := range expectedRepos {
		if !found {
			t.Errorf("Expected to find action %s, but it was not extracted", repo)
		}
	}

	// Verify SHA format
	for _, action := range actions {
		if len(action.SHA) != 40 {
			t.Errorf("Expected SHA to be 40 characters, got %d for %s", len(action.SHA), action.Repo)
		}
	}
}

func TestExtractActionsFromLockFileNoDuplicates(t *testing.T) {
	// Create a temporary lock file with duplicate actions
	tmpDir := testutil.TempDir(t, "test-*")
	lockFile := filepath.Join(tmpDir, "test.lock.yml")

	lockContent := `
name: Test Workflow
on: push
jobs:
  test1:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd
      - uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f
  test2:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd
      - uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f
`

	if err := os.WriteFile(lockFile, []byte(lockContent), 0644); err != nil {
		t.Fatalf("Failed to create test lock file: %v", err)
	}

	// Extract actions
	actions, err := ExtractActionsFromLockFile(lockFile)
	if err != nil {
		t.Fatalf("ExtractActionsFromLockFile failed: %v", err)
	}

	// Verify we only have 2 unique actions despite being used twice
	if len(actions) != 2 {
		t.Errorf("Expected 2 unique actions, got %d", len(actions))
	}
}

func TestCheckActionSHAUpdates(t *testing.T) {
	// Create a test action cache
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Create test actions with known SHAs
	actions := []ActionUsage{
		{
			Repo:    "actions/checkout",
			SHA:     "93cb6efe18208431cddfb8368fd83d5badbf9bfd", // Current SHA
			Version: "v5",
		},
		{
			Repo:    "actions/setup-node",
			SHA:     "oldsha0000000000000000000000000000000000", // Outdated SHA
			Version: "v6",
		},
	}

	// Pre-populate the cache with known values
	// For actions/checkout@v5, use the same SHA (up to date)
	cache.Set("actions/checkout", "v5", "93cb6efe18208431cddfb8368fd83d5badbf9bfd")
	// For actions/setup-node@v6, use a different SHA (needs update)
	cache.Set("actions/setup-node", "v6", "newsha0000000000000000000000000000000000")

	// Create resolver with the cache
	resolver := NewActionResolver(cache)

	// Check for updates
	checks := CheckActionSHAUpdates(actions, resolver)

	// Verify results
	if len(checks) != 2 {
		t.Errorf("Expected 2 check results, got %d", len(checks))
	}

	// First action (actions/checkout) should be up to date
	if checks[0].NeedsUpdate {
		t.Errorf("Expected actions/checkout to be up to date, but it needs update")
	}

	// Second action (actions/setup-node) should need update
	if !checks[1].NeedsUpdate {
		t.Errorf("Expected actions/setup-node to need update, but it's marked as up to date")
	}
}

func TestExtractActionsFromLockFileNoActions(t *testing.T) {
	// Create a temporary lock file with no actions
	tmpDir := testutil.TempDir(t, "test-*")
	lockFile := filepath.Join(tmpDir, "test.lock.yml")

	lockContent := `
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Run tests
        run: npm test
`

	if err := os.WriteFile(lockFile, []byte(lockContent), 0644); err != nil {
		t.Fatalf("Failed to create test lock file: %v", err)
	}

	// Extract actions
	actions, err := ExtractActionsFromLockFile(lockFile)
	if err != nil {
		t.Fatalf("ExtractActionsFromLockFile failed: %v", err)
	}

	// Verify we have no actions
	if len(actions) != 0 {
		t.Errorf("Expected 0 actions, got %d", len(actions))
	}
}

func TestExtractActionsFromLockFileInvalidFile(t *testing.T) {
	// Try to extract from non-existent file
	_, err := ExtractActionsFromLockFile("/nonexistent/file.yml")
	if err == nil {
		t.Error("Expected error when reading non-existent file, got nil")
	}
}

func TestExtractActionsFromLockFileWithVersionComments(t *testing.T) {
	// Create a temporary lock file with version comments
	tmpDir := testutil.TempDir(t, "test-*")
	lockFile := filepath.Join(tmpDir, "test.lock.yml")

	lockContent := `
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v5
      - uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f # v6
      - uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd # v8.0.0
      - name: Run tests
        run: npm test
`

	if err := os.WriteFile(lockFile, []byte(lockContent), 0644); err != nil {
		t.Fatalf("Failed to create test lock file: %v", err)
	}

	// Extract actions
	actions, err := ExtractActionsFromLockFile(lockFile)
	if err != nil {
		t.Fatalf("ExtractActionsFromLockFile failed: %v", err)
	}

	// Verify we extracted the expected actions with versions
	if len(actions) != 3 {
		t.Errorf("Expected 3 actions, got %d", len(actions))
	}

	// Create a map to easily look up actions by repo
	actionMap := make(map[string]ActionUsage)
	for _, action := range actions {
		actionMap[action.Repo] = action
	}

	// Verify versions were extracted correctly from comments
	tests := []struct {
		repo            string
		expectedVersion string
	}{
		{"actions/checkout", "v5"},
		{"actions/setup-node", "v6"},
		{"actions/github-script", "v8.0.0"},
	}

	for _, tt := range tests {
		action, found := actionMap[tt.repo]
		if !found {
			t.Errorf("Expected to find action %s, but it was not extracted", tt.repo)
			continue
		}

		if action.Version != tt.expectedVersion {
			t.Errorf("For %s: expected version %s, got %s", tt.repo, tt.expectedVersion, action.Version)
		}
	}
}
