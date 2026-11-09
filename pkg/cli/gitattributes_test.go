//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGitRepo(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Skipping test - git not available")
	}

	repoDir := t.TempDir()
	t.Chdir(repoDir)
	cmd := exec.Command("git", "init")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git init failed: %s", output)
}

func TestEnsureGitAttributes(t *testing.T) {
	tests := []struct {
		name            string
		existingContent string
		expectedContent string
		expectUpdated   bool
		makeDirectory   bool
		unreadable      bool
		unwritableFile  bool
		wantErr         string
	}{
		{
			name:            "creates new gitattributes file",
			expectedContent: constants.WorkflowsLockYmlGitAttributesEntry,
			expectUpdated:   true,
		},
		{
			name:            "adds entry to existing file",
			existingContent: "*.generated linguist-generated=true\n",
			expectedContent: "*.generated linguist-generated=true\n\n" + constants.WorkflowsLockYmlGitAttributesEntry,
			expectUpdated:   true,
		},
		{
			name:            "does not duplicate existing entry",
			existingContent: constants.WorkflowsLockYmlGitAttributesEntry + "\n",
			expectedContent: constants.WorkflowsLockYmlGitAttributesEntry + "\n",
		},
		{
			name:            "does not duplicate entry with different order",
			existingContent: "*.md linguist-documentation=true\n" + constants.WorkflowsLockYmlGitAttributesEntry + "\n*.txt text=auto\n",
			expectedContent: "*.md linguist-documentation=true\n" + constants.WorkflowsLockYmlGitAttributesEntry + "\n*.txt text=auto\n",
		},
		{
			name:            "migrates legacy merge=ours entry",
			existingContent: "*.md linguist-documentation=true\n" + constants.WorkflowsLockYmlGitAttributesEntryLegacy + "\n*.txt text=auto\n",
			expectedContent: "*.md linguist-documentation=true\n" + constants.WorkflowsLockYmlGitAttributesEntry + "\n*.txt text=auto\n",
			expectUpdated:   true,
		},
		{
			name:            "does not rewrite repository-owned lock-yml policy",
			existingContent: "*.md linguist-documentation=true\n.github/workflows/*.lock.yml linguist-generated=true merge=union\n*.txt text=auto\n",
			expectedContent: "*.md linguist-documentation=true\n.github/workflows/*.lock.yml linguist-generated=true merge=union\n*.txt text=auto\n\n" + constants.WorkflowsLockYmlGitAttributesEntry,
			expectUpdated:   true,
		},
		{
			name:          "fails to read when gitattributes is a directory",
			makeDirectory: true,
			wantErr:       "failed to read .gitattributes",
		},
		{
			name:            "fails to read an unreadable gitattributes file",
			existingContent: "*.generated linguist-generated=true\n",
			expectedContent: "*.generated linguist-generated=true\n",
			unreadable:      true,
			wantErr:         "failed to read .gitattributes",
		},
		{
			name:            "fails to write when the file is read-only",
			existingContent: "*.generated linguist-generated=true\n",
			expectedContent: "*.generated linguist-generated=true\n",
			unwritableFile:  true,
			wantErr:         "failed to write .gitattributes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupGitRepo(t)
			gitAttributesPath := ".gitattributes"

			if tt.makeDirectory {
				require.NoError(t, os.Mkdir(gitAttributesPath, 0o755))
			} else if tt.existingContent != "" {
				require.NoError(t, os.WriteFile(gitAttributesPath, []byte(tt.existingContent), 0o644))
			}

			if tt.unreadable {
				require.NoError(t, os.Chmod(gitAttributesPath, 0o200))
				if _, err := os.ReadFile(gitAttributesPath); err == nil {
					t.Skip("permission bits do not prevent reads for this user")
				}
				defer func() {
					require.NoError(t, os.Chmod(gitAttributesPath, 0o644))
				}()
			}
			if tt.unwritableFile {
				require.NoError(t, os.Chmod(gitAttributesPath, 0o444))
				defer func() {
					require.NoError(t, os.Chmod(gitAttributesPath, 0o644))
				}()
				if err := os.WriteFile(gitAttributesPath, []byte("probe"), 0o644); err == nil {
					t.Skip("file permissions do not prevent writes for this user")
				}
			}

			updated, err := ensureGitAttributes()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				if tt.expectedContent != "" {
					if tt.unreadable {
						require.NoError(t, os.Chmod(gitAttributesPath, 0o644))
					}
					content, readErr := os.ReadFile(gitAttributesPath)
					require.NoError(t, readErr)
					assert.Equal(t, tt.expectedContent, string(content))
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectUpdated, updated)

			content, err := os.ReadFile(gitAttributesPath)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedContent, string(content))

			updatedAgain, errAgain := ensureGitAttributes()
			require.NoError(t, errAgain)
			assert.False(t, updatedAgain, "second call should leave .gitattributes unchanged")
		})
	}
}

func TestEnsureGitAttributes_DoesNotAddCampaignEntry(t *testing.T) {
	setupGitRepo(t)

	_, err := ensureGitAttributes()
	require.NoError(t, err)

	content, err := os.ReadFile(".gitattributes")
	require.NoError(t, err)
	assert.NotContains(t, string(content), ".github/workflows/*.campaign.g.md")
}

func TestEnsureGitAttributes_NotInGitRepo(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := ensureGitAttributes()
	require.Error(t, err)

	_, statErr := os.Stat(".gitattributes")
	assert.True(t, os.IsNotExist(statErr), "no .gitattributes file should be created outside a git repository")
}

func TestReadGitAttributesFile(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, path string)
		expected   []string
		unreadable bool
	}{
		{
			name:     "missing file",
			expected: []string{},
		},
		{
			name: "splits existing file by newline",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, []byte("first\nsecond\n"), 0o644))
			},
			expected: []string{"first", "second", ""},
		},
		{
			name: "normalizes CRLF line endings",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, []byte("first\r\nsecond\r\n"), 0o644))
			},
			expected: []string{"first", "second", ""},
		},
		{
			name: "returns an error when file is unreadable",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, []byte("existing rules\n"), 0o644))
				require.NoError(t, os.Chmod(path, 0o200))
			},
			unreadable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".gitattributes")
			if tt.setup != nil {
				tt.setup(t, path)
			}

			if tt.unreadable {
				if _, err := os.ReadFile(path); err == nil {
					t.Skip("permission bits do not prevent reads for this user")
				}
				defer func() {
					require.NoError(t, os.Chmod(path, 0o644))
				}()
			}

			lines, err := readGitAttributesFile(path)
			if tt.unreadable {
				require.ErrorContains(t, err, "failed to read .gitattributes")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, lines)
		})
	}
}

func TestEnsureRequiredGitAttributesEntry(t *testing.T) {
	entry := constants.WorkflowsLockYmlGitAttributesEntry
	tests := []struct {
		name     string
		input    []string
		expected []string
		modified bool
	}{
		{
			name:     "nil input",
			expected: []string{entry},
			modified: true,
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: []string{entry},
			modified: true,
		},
		{
			name:     "trailing empty line",
			input:    []string{"*.txt text=auto", ""},
			expected: []string{"*.txt text=auto", "", entry},
			modified: true,
		},
		{
			name:     "replaces legacy entry",
			input:    []string{"*.md linguist-documentation=true", constants.WorkflowsLockYmlGitAttributesEntryLegacy, "*.txt text=auto"},
			expected: []string{"*.md linguist-documentation=true", entry, "*.txt text=auto"},
			modified: true,
		},
		{
			name:     "whitespace-padded existing entry",
			input:    []string{"  " + entry + "  "},
			expected: []string{"  " + entry + "  "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, modified := ensureRequiredGitAttributesEntry(tt.input)
			assert.Equal(t, tt.expected, lines)
			assert.Equal(t, tt.modified, modified)
		})
	}
}

func TestStageGitAttributesIfChangedFromFreshRepo(t *testing.T) {
	tests := []struct {
		name                string
		createFile          bool
		commitExistingFile  bool
		expectedStagedFiles string
		wantErr             bool
	}{
		{
			name:                "stages gitattributes",
			createFile:          true,
			expectedStagedFiles: ".gitattributes",
		},
		{
			name:               "unchanged gitattributes is a no-op",
			createFile:         true,
			commitExistingFile: true,
		},
		{
			name:    "missing gitattributes returns an error",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupGitRepo(t)
			if tt.createFile {
				require.NoError(t, os.WriteFile(".gitattributes", []byte(constants.WorkflowsLockYmlGitAttributesEntry), 0o644))
			}
			if tt.commitExistingFile {
				for _, setting := range [][2]string{{"user.name", "Test User"}, {"user.email", "test@example.com"}} {
					require.NoError(t, exec.Command("git", "config", setting[0], setting[1]).Run())
				}
				require.NoError(t, exec.Command("git", "add", ".gitattributes").Run())
				require.NoError(t, exec.Command("git", "commit", "-m", "add gitattributes").Run())
			}

			err := stageGitAttributesIfChanged()
			if tt.wantErr {
				require.ErrorContains(t, err, "exit status")
				return
			}
			require.NoError(t, err)

			output, err := exec.Command("git", "diff", "--cached", "--name-only").Output()
			require.NoError(t, err)
			assert.Equal(t, tt.expectedStagedFiles, strings.TrimSpace(string(output)))
		})
	}
}
