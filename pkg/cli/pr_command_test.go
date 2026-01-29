//go:build !integration

package cli

import (
	"testing"
)

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantPR    int
		wantErr   bool
	}{
		{
			name:      "valid GitHub PR URL",
			url:       "https://github.com/trial/repo/pull/234",
			wantOwner: "trial",
			wantRepo:  "repo",
			wantPR:    234,
			wantErr:   false,
		},
		{
			name:      "valid GitHub PR URL with hyphenated repo name",
			url:       "https://github.com/PR-OWNER/PR-REPO/pull/456",
			wantOwner: "PR-OWNER",
			wantRepo:  "PR-REPO",
			wantPR:    456,
			wantErr:   false,
		},
		{
			name:      "valid GitHub PR URL with underscores",
			url:       "https://github.com/test_owner/test_repo/pull/789",
			wantOwner: "test_owner",
			wantRepo:  "test_repo",
			wantPR:    789,
			wantErr:   false,
		},
		{
			name:    "invalid URL format",
			url:     "not-a-url",
			wantErr: true,
		},
		{
			name:      "non-GitHub URL with valid path structure",
			url:       "https://gitlab.com/owner/repo/pull/123",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantPR:    123,
			wantErr:   false,
		},
		{
			name:    "invalid GitHub URL path - missing pull",
			url:     "https://github.com/owner/repo/123",
			wantErr: true,
		},
		{
			name:    "invalid GitHub URL path - wrong format",
			url:     "https://github.com/owner/repo/pulls/123",
			wantErr: true,
		},
		{
			name:    "invalid PR number",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
		{
			name:    "missing owner",
			url:     "https://github.com//repo/pull/123",
			wantErr: true,
		},
		{
			name:    "missing repo",
			url:     "https://github.com/owner//pull/123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, prNumber, err := parsePRURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePRURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("parsePRURL() unexpected error: %v", err)
				return
			}

			if owner != tt.wantOwner {
				t.Errorf("parsePRURL() owner = %v, want %v", owner, tt.wantOwner)
			}

			if repo != tt.wantRepo {
				t.Errorf("parsePRURL() repo = %v, want %v", repo, tt.wantRepo)
			}

			if prNumber != tt.wantPR {
				t.Errorf("parsePRURL() prNumber = %v, want %v", prNumber, tt.wantPR)
			}
		})
	}
}

func TestPRInfo(t *testing.T) {
	// Test PRInfo struct can be properly initialized
	prInfo := &PRInfo{
		Number:      123,
		Title:       "Test PR",
		State:       "open",
		AuthorLogin: "test-author",
	}

	if prInfo.Number != 123 {
		t.Errorf("PRInfo.Number = %v, want %v", prInfo.Number, 123)
	}

	if prInfo.Title != "Test PR" {
		t.Errorf("PRInfo.Title = %v, want %v", prInfo.Title, "Test PR")
	}

	if prInfo.State != "open" {
		t.Errorf("PRInfo.State = %v, want %v", prInfo.State, "open")
	}

	if prInfo.AuthorLogin != "test-author" {
		t.Errorf("PRInfo.AuthorLogin = %v, want %v", prInfo.AuthorLogin, "test-author")
	}
}

// TestNewPRCommand tests that the PR command is created properly
func TestNewPRCommand(t *testing.T) {
	cmd := NewPRCommand()

	if cmd.Use != "pr" {
		t.Errorf("Expected command use to be 'pr', got %s", cmd.Use)
	}

	if cmd.Short != "Pull request utilities" {
		t.Errorf("Expected command short description to be 'Pull request utilities', got %s", cmd.Short)
	}

	// Check that transfer subcommand is added
	subcommands := cmd.Commands()
	found := false
	for _, subcmd := range subcommands {
		if subcmd.Use == "transfer <pr-url>" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected 'transfer' subcommand to be added to pr command")
	}
}

// TestNewPRTransferSubcommand tests that the transfer subcommand is created properly
func TestNewPRTransferSubcommand(t *testing.T) {
	cmd := NewPRTransferSubcommand()

	if cmd.Use != "transfer <pr-url>" {
		t.Errorf("Expected command use to be 'transfer <pr-url>', got %s", cmd.Use)
	}

	if cmd.Short != "Transfer a pull request to another repository" {
		t.Errorf("Expected command short description to match, got %s", cmd.Short)
	}

	// Check that --repo flag exists
	repoFlag := cmd.Flags().Lookup("repo")
	if repoFlag == nil {
		t.Error("Expected --repo flag to exist")
	}

	// Check that --verbose flag exists
	verboseFlag := cmd.Flags().Lookup("verbose")
	if verboseFlag == nil {
		t.Error("Expected --verbose flag to exist")
	}
}
