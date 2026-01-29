//go:build !integration

package cli

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectShell(t *testing.T) {
	// Save original environment
	originalShell := os.Getenv("SHELL")
	originalBashVersion := os.Getenv("BASH_VERSION")
	originalZshVersion := os.Getenv("ZSH_VERSION")
	originalFishVersion := os.Getenv("FISH_VERSION")

	// Restore environment after test
	defer func() {
		os.Setenv("SHELL", originalShell)
		os.Setenv("BASH_VERSION", originalBashVersion)
		os.Setenv("ZSH_VERSION", originalZshVersion)
		os.Setenv("FISH_VERSION", originalFishVersion)
	}()

	tests := []struct {
		name         string
		shellEnv     string
		bashVersion  string
		zshVersion   string
		fishVersion  string
		expectedType ShellType
	}{
		{
			name:         "detect bash from BASH_VERSION",
			shellEnv:     "/bin/bash",
			bashVersion:  "5.0.0",
			zshVersion:   "",
			fishVersion:  "",
			expectedType: ShellBash,
		},
		{
			name:         "detect zsh from ZSH_VERSION",
			shellEnv:     "/bin/zsh",
			bashVersion:  "",
			zshVersion:   "5.8",
			fishVersion:  "",
			expectedType: ShellZsh,
		},
		{
			name:         "detect fish from FISH_VERSION",
			shellEnv:     "/usr/bin/fish",
			bashVersion:  "",
			zshVersion:   "",
			fishVersion:  "3.1.2",
			expectedType: ShellFish,
		},
		{
			name:         "detect bash from SHELL path",
			shellEnv:     "/bin/bash",
			bashVersion:  "",
			zshVersion:   "",
			fishVersion:  "",
			expectedType: ShellBash,
		},
		{
			name:         "detect zsh from SHELL path",
			shellEnv:     "/usr/local/bin/zsh",
			bashVersion:  "",
			zshVersion:   "",
			fishVersion:  "",
			expectedType: ShellZsh,
		},
		{
			name:         "detect fish from SHELL path",
			shellEnv:     "/usr/bin/fish",
			bashVersion:  "",
			zshVersion:   "",
			fishVersion:  "",
			expectedType: ShellFish,
		},
		{
			name:         "detect powershell from SHELL path",
			shellEnv:     "/usr/bin/pwsh",
			bashVersion:  "",
			zshVersion:   "",
			fishVersion:  "",
			expectedType: ShellPowerShell,
		},
		{
			name:         "unknown shell",
			shellEnv:     "/bin/unknown",
			bashVersion:  "",
			zshVersion:   "",
			fishVersion:  "",
			expectedType: ShellUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment for this test
			os.Setenv("SHELL", tt.shellEnv)
			os.Setenv("BASH_VERSION", tt.bashVersion)
			os.Setenv("ZSH_VERSION", tt.zshVersion)
			os.Setenv("FISH_VERSION", tt.fishVersion)

			// Test detection
			detected := DetectShell()
			assert.Equal(t, tt.expectedType, detected)
		})
	}
}

func TestDetectShellNoShellEnv(t *testing.T) {
	// Save original environment
	originalShell := os.Getenv("SHELL")
	originalBashVersion := os.Getenv("BASH_VERSION")
	originalZshVersion := os.Getenv("ZSH_VERSION")
	originalFishVersion := os.Getenv("FISH_VERSION")

	// Restore environment after test
	defer func() {
		os.Setenv("SHELL", originalShell)
		os.Setenv("BASH_VERSION", originalBashVersion)
		os.Setenv("ZSH_VERSION", originalZshVersion)
		os.Setenv("FISH_VERSION", originalFishVersion)
	}()

	// Clear all shell environment variables
	os.Unsetenv("SHELL")
	os.Unsetenv("BASH_VERSION")
	os.Unsetenv("ZSH_VERSION")
	os.Unsetenv("FISH_VERSION")

	detected := DetectShell()

	// On Windows, should detect PowerShell
	if runtime.GOOS == "windows" {
		assert.Equal(t, ShellPowerShell, detected)
	} else {
		// On Unix-like systems, should be unknown
		assert.Equal(t, ShellUnknown, detected)
	}
}

func TestDetectShellPrioritizesVersionVariable(t *testing.T) {
	// Save original environment
	originalShell := os.Getenv("SHELL")
	originalBashVersion := os.Getenv("BASH_VERSION")
	originalZshVersion := os.Getenv("ZSH_VERSION")

	// Restore environment after test
	defer func() {
		os.Setenv("SHELL", originalShell)
		os.Setenv("BASH_VERSION", originalBashVersion)
		os.Setenv("ZSH_VERSION", originalZshVersion)
	}()

	// Set SHELL to bash but ZSH_VERSION is set (running zsh inside bash)
	os.Setenv("SHELL", "/bin/bash")
	os.Setenv("ZSH_VERSION", "5.8")
	os.Unsetenv("BASH_VERSION")

	detected := DetectShell()

	// Should prioritize ZSH_VERSION over SHELL
	assert.Equal(t, ShellZsh, detected)
}

func TestShellTypeString(t *testing.T) {
	tests := []struct {
		shellType ShellType
		expected  string
	}{
		{ShellBash, "bash"},
		{ShellZsh, "zsh"},
		{ShellFish, "fish"},
		{ShellPowerShell, "powershell"},
		{ShellUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.shellType))
		})
	}
}

func TestUninstallBashCompletion(t *testing.T) {
	// Create a temporary home directory
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	// Create completion directory and file
	completionDir := tmpDir + "/.bash_completion.d"
	err := os.MkdirAll(completionDir, 0750)
	require.NoError(t, err)

	completionFile := completionDir + "/gh-aw"
	err = os.WriteFile(completionFile, []byte("# test completion"), 0600)
	require.NoError(t, err)

	// Verify file exists before uninstall
	_, err = os.Stat(completionFile)
	require.NoError(t, err)

	// Uninstall
	err = uninstallBashCompletion(false)
	require.NoError(t, err)

	// Verify file is removed
	_, err = os.Stat(completionFile)
	assert.True(t, os.IsNotExist(err), "Completion file should be removed")
}

func TestUninstallBashCompletionNotFound(t *testing.T) {
	// Create a temporary home directory without completion file
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	// Uninstall should fail when no file found
	err := uninstallBashCompletion(false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no bash completion file found")
}

func TestUninstallZshCompletion(t *testing.T) {
	// Create a temporary home directory
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	// Create completion directory and file
	completionDir := tmpDir + "/.zsh/completions"
	err := os.MkdirAll(completionDir, 0750)
	require.NoError(t, err)

	completionFile := completionDir + "/_gh-aw"
	err = os.WriteFile(completionFile, []byte("# test completion"), 0600)
	require.NoError(t, err)

	// Verify file exists before uninstall
	_, err = os.Stat(completionFile)
	require.NoError(t, err)

	// Uninstall
	err = uninstallZshCompletion(false)
	require.NoError(t, err)

	// Verify file is removed
	_, err = os.Stat(completionFile)
	assert.True(t, os.IsNotExist(err), "Completion file should be removed")
}

func TestUninstallZshCompletionNotFound(t *testing.T) {
	// Create a temporary home directory without completion file
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	// Uninstall should fail when no file found
	err := uninstallZshCompletion(false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no zsh completion file found")
}

func TestUninstallFishCompletion(t *testing.T) {
	// Create a temporary home directory
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	// Create completion directory and file
	completionDir := tmpDir + "/.config/fish/completions"
	err := os.MkdirAll(completionDir, 0750)
	require.NoError(t, err)

	completionFile := completionDir + "/gh-aw.fish"
	err = os.WriteFile(completionFile, []byte("# test completion"), 0600)
	require.NoError(t, err)

	// Verify file exists before uninstall
	_, err = os.Stat(completionFile)
	require.NoError(t, err)

	// Uninstall
	err = uninstallFishCompletion(false)
	require.NoError(t, err)

	// Verify file is removed
	_, err = os.Stat(completionFile)
	assert.True(t, os.IsNotExist(err), "Completion file should be removed")
}

func TestUninstallFishCompletionNotFound(t *testing.T) {
	// Create a temporary home directory without completion file
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	// Uninstall should fail when no file found
	err := uninstallFishCompletion(false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no fish completion file found")
}

func TestUninstallShellCompletion(t *testing.T) {
	// Save original environment
	originalShell := os.Getenv("SHELL")
	originalHome := os.Getenv("HOME")

	// Restore environment after test
	defer func() {
		os.Setenv("SHELL", originalShell)
		os.Setenv("HOME", originalHome)
	}()

	// Create a temporary home directory
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	tests := []struct {
		name         string
		shellEnv     string
		setupFunc    func(t *testing.T, tmpDir string)
		expectError  bool
		errorMessage string
	}{
		{
			name:     "bash uninstall success",
			shellEnv: "/bin/bash",
			setupFunc: func(t *testing.T, tmpDir string) {
				completionDir := tmpDir + "/.bash_completion.d"
				err := os.MkdirAll(completionDir, 0750)
				require.NoError(t, err)
				err = os.WriteFile(completionDir+"/gh-aw", []byte("# test"), 0600)
				require.NoError(t, err)
			},
			expectError: false,
		},
		{
			name:     "zsh uninstall success",
			shellEnv: "/bin/zsh",
			setupFunc: func(t *testing.T, tmpDir string) {
				completionDir := tmpDir + "/.zsh/completions"
				err := os.MkdirAll(completionDir, 0750)
				require.NoError(t, err)
				err = os.WriteFile(completionDir+"/_gh-aw", []byte("# test"), 0600)
				require.NoError(t, err)
			},
			expectError: false,
		},
		{
			name:     "fish uninstall success",
			shellEnv: "/usr/bin/fish",
			setupFunc: func(t *testing.T, tmpDir string) {
				completionDir := tmpDir + "/.config/fish/completions"
				err := os.MkdirAll(completionDir, 0750)
				require.NoError(t, err)
				err = os.WriteFile(completionDir+"/gh-aw.fish", []byte("# test"), 0600)
				require.NoError(t, err)
			},
			expectError: false,
		},
		{
			name:         "unknown shell fails",
			shellEnv:     "",
			setupFunc:    func(t *testing.T, tmpDir string) {},
			expectError:  true,
			errorMessage: "could not detect shell type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all shell-specific env vars
			os.Unsetenv("BASH_VERSION")
			os.Unsetenv("ZSH_VERSION")
			os.Unsetenv("FISH_VERSION")

			// Set shell environment
			os.Setenv("SHELL", tt.shellEnv)

			// Setup test
			if tt.setupFunc != nil {
				tt.setupFunc(t, tmpDir)
			}

			// Run uninstall
			err := UninstallShellCompletion(false)

			// Verify results
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMessage != "" {
					assert.Contains(t, err.Error(), tt.errorMessage)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
