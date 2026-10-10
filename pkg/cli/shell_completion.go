package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var shellCompletionLog = logger.New("cli:shell_completion")

// ShellType represents the detected shell type
type ShellType string

const (
	ShellBash       ShellType = "bash"
	ShellZsh        ShellType = "zsh"
	ShellFish       ShellType = "fish"
	ShellPowerShell ShellType = "powershell"
	ShellUnknown    ShellType = "unknown"
)

// DetectShell detects the current shell from environment variables
func DetectShell() ShellType {
	shellCompletionLog.Print("Detecting current shell")

	// Check shell-specific version variables first (most reliable)
	if os.Getenv("ZSH_VERSION") != "" { //nolint:osgetenvlibrary
		shellCompletionLog.Print("Detected zsh from ZSH_VERSION")
		return ShellZsh
	}
	if os.Getenv("BASH_VERSION") != "" { //nolint:osgetenvlibrary
		shellCompletionLog.Print("Detected bash from BASH_VERSION")
		return ShellBash
	}
	if os.Getenv("FISH_VERSION") != "" { //nolint:osgetenvlibrary
		shellCompletionLog.Print("Detected fish from FISH_VERSION")
		return ShellFish
	}

	// Fall back to $SHELL environment variable
	shell := os.Getenv("SHELL") //nolint:osgetenvlibrary
	if shell == "" {
		shellCompletionLog.Print("SHELL environment variable not set, checking platform")
		// On Windows, check for PowerShell
		if runtime.GOOS == "windows" {
			shellCompletionLog.Print("Detected Windows, assuming PowerShell")
			return ShellPowerShell
		}
		shellCompletionLog.Print("Could not detect shell")
		return ShellUnknown
	}

	shellCompletionLog.Printf("SHELL environment variable: %s", shell)

	// Extract shell name from path
	shellName := filepath.Base(shell)
	shellCompletionLog.Printf("Shell base name: %s", shellName)

	switch {
	case strings.Contains(shellName, "bash"):
		shellCompletionLog.Print("Detected bash from SHELL")
		return ShellBash
	case strings.Contains(shellName, "zsh"):
		shellCompletionLog.Print("Detected zsh from SHELL")
		return ShellZsh
	case strings.Contains(shellName, "fish"):
		shellCompletionLog.Print("Detected fish from SHELL")
		return ShellFish
	case strings.Contains(shellName, "pwsh") || strings.Contains(shellName, "powershell"):
		shellCompletionLog.Print("Detected PowerShell from SHELL")
		return ShellPowerShell
	default:
		shellCompletionLog.Printf("Unknown shell: %s", shellName)
		return ShellUnknown
	}
}

// InstallShellCompletion installs shell completion for the detected shell
func InstallShellCompletion(verbose bool, rootCmd CommandProvider) error {
	shellCompletionLog.Print("Starting shell completion installation")

	// Type assert rootCmd to *cobra.Command to access additional methods if needed
	// For now, we only use the CommandProvider interface methods
	cmd, ok := rootCmd.(*cobra.Command)
	if !ok {
		return errors.New("rootCmd should be a *cobra.Command; call ShellCompletion with the root Cobra command")
	}

	shellType := DetectShell()
	shellCompletionLog.Printf("Detected shell type: %s", shellType)

	if shellType == ShellUnknown {
		return errors.New("could not detect shell type — expected SHELL, BASH_VERSION, ZSH_VERSION, or FISH_VERSION to identify the shell; install completions manually using an explicit shell, for example: gh aw completion bash")
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Detected shell: %s", shellType)))

	switch shellType {
	case ShellBash:
		return installBashCompletion(verbose, cmd)
	case ShellZsh:
		return installZshCompletion(verbose, cmd)
	case ShellFish:
		return installFishCompletion(verbose, cmd)
	case ShellPowerShell:
		return installPowerShellCompletion(verbose, cmd)
	default:
		return fmt.Errorf("shell completion not supported for %s — expected one of bash, zsh, fish, or powershell; choose a supported shell, for example: gh aw completion bash", shellType)
	}
}

// validateRcPath cleans a shell rc file path (for example /home/user/.bashrc) and ensures it is
// absolute before it is read. It returns the cleaned path or an actionable error.
func validateRcPath(rcName string, rcPath string) (string, error) {
	cleanPath := filepath.Clean(rcPath)
	if !filepath.IsAbs(cleanPath) {
		shellCompletionLog.Printf("Invalid %s path (not absolute): %s", rcName, rcPath)
		return "", fmt.Errorf("%s path %q is not an absolute path — expected an absolute path such as '/home/user/.%s'; set $HOME to an absolute home directory and retry", rcName, rcPath, rcName)
	}
	return cleanPath, nil
}

// installBashCompletion installs bash completion
func installBashCompletion(verbose bool, cmd *cobra.Command) error {
	shellCompletionLog.Print("Installing bash completion")

	// Generate completion script using Cobra
	var buf bytes.Buffer
	if err := cmd.GenBashCompletion(&buf); err != nil {
		return fmt.Errorf("bash completion generation error: %w; check that the command is initialized and retry: gh aw completion bash", err)
	}

	completionScript := buf.String()

	// Determine installation path
	var completionPath string
	homeDir, err := gitutil.UserHomeDir()
	if err != nil {
		return err
	}

	// Try to determine the best location for bash completions
	if runtime.GOOS == "darwin" {
		// macOS with Homebrew
		brewPrefix := os.Getenv("HOMEBREW_PREFIX") //nolint:osgetenvlibrary
		if brewPrefix == "" {
			// Try common locations
			for _, prefix := range []string{constants.HomebrewPrefix, constants.UsrLocalPrefix} {
				if fileutil.DirExists(filepath.Join(prefix, "etc", "bash_completion.d")) {
					brewPrefix = prefix
					break
				}
			}
		}
		if brewPrefix != "" {
			completionPath = filepath.Join(brewPrefix, "etc", "bash_completion.d", "gh-aw")
		} else {
			completionPath = filepath.Join(homeDir, ".bash_completion.d", "gh-aw")
		}
	} else {
		// Linux
		if fileutil.DirExists(constants.BashCompletionDir) {
			completionPath = constants.BashCompletionGhAwPath
		} else {
			completionPath = filepath.Join(homeDir, ".bash_completion.d", "gh-aw")
		}
	}

	// Create directory if needed (for user-level installations)
	completionDir := filepath.Dir(completionPath)
	if strings.HasPrefix(completionDir, homeDir) {
		// Use restrictive permissions (0750) following principle of least privilege
		if err := os.MkdirAll(completionDir, constants.DirPermSensitive); err != nil {
			return fmt.Errorf("create completion directory %q error: %w; check that the parent directory exists and is writable, then retry", completionDir, err)
		}
	}

	// Try to write completion file
	// Use restrictive permissions (0600) following principle of least privilege
	err = os.WriteFile(completionPath, []byte(completionScript), constants.FilePermSensitive)
	if err != nil && strings.HasPrefix(completionPath, "/etc") {
		// If system-wide installation fails, fall back to user directory
		shellCompletionLog.Printf("Failed to install system-wide, falling back to user directory: %v", err)
		completionPath = filepath.Join(homeDir, ".bash_completion.d", "gh-aw")
		// Use restrictive permissions (0750) following principle of least privilege
		if err := os.MkdirAll(filepath.Dir(completionPath), constants.DirPermSensitive); err != nil {
			return fmt.Errorf("create user completion directory %q error: %w; check that the home directory is writable, then retry", filepath.Dir(completionPath), err)
		}
		// Use restrictive permissions (0600) following principle of least privilege
		if err := os.WriteFile(completionPath, []byte(completionScript), constants.FilePermSensitive); err != nil {
			return fmt.Errorf("write completion file %q error: %w; check that the directory is writable, then retry", completionPath, err)
		}
	} else if err != nil {
		return fmt.Errorf("write completion file %q error: %w; check that the directory is writable, then retry", completionPath, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Installed bash completion to: "+completionPath))

	// Check if .bashrc sources completions
	bashrcPath := filepath.Join(homeDir, ".bashrc")
	if strings.HasPrefix(completionPath, homeDir) {
		// For user-level installations, check if .bashrc sources the completion directory
		// Clean and validate the path to prevent path traversal
		cleanBashrcPath, err := validateRcPath("bashrc", bashrcPath)
		if err != nil {
			return err
		}
		// #nosec G304 -- bashrcPath is constructed from trusted os.UserHomeDir() and a constant filename
		bashrcContent, err := os.ReadFile(cleanBashrcPath)
		needsSourceLine := true
		if err == nil {
			if strings.Contains(string(bashrcContent), ".bash_completion.d") ||
				strings.Contains(string(bashrcContent), completionPath) {
				needsSourceLine = false
			}
		}

		if needsSourceLine {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To enable completions, add the following to your ~/.bashrc:"))
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintf(os.Stderr, "  for f in ~/.bash_completion.d/*; do [ -f \"$f\" ] && source \"$f\"; done\n")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Then restart your shell or run: source ~/.bashrc"))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Please restart your shell for completions to take effect"))
		}
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Please restart your shell for completions to take effect"))
	}

	return nil
}

// installZshCompletion installs zsh completion
func installZshCompletion(verbose bool, cmd *cobra.Command) error {
	shellCompletionLog.Print("Installing zsh completion")

	// Generate completion script using Cobra
	var buf bytes.Buffer
	if err := cmd.GenZshCompletion(&buf); err != nil {
		return fmt.Errorf("zsh completion generation error: %w; check that the command is initialized and retry: gh aw completion zsh", err)
	}

	completionScript := buf.String()

	// Determine installation path
	homeDir, err := gitutil.UserHomeDir()
	if err != nil {
		return err
	}

	// Check for fpath directories
	var completionPath string

	// Try user's local completion directory first
	userCompletionDir := filepath.Join(homeDir, ".zsh", "completions")
	// Use restrictive permissions (0750) following principle of least privilege
	if err := os.MkdirAll(userCompletionDir, constants.DirPermSensitive); err != nil {
		return fmt.Errorf("create completion directory %q error: %w; check that the home directory is writable, then retry", userCompletionDir, err)
	}
	completionPath = filepath.Join(userCompletionDir, "_gh-aw")

	// Write completion file
	// Use restrictive permissions (0600) following principle of least privilege
	if err := os.WriteFile(completionPath, []byte(completionScript), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("write completion file %q error: %w; check that the directory is writable, then retry", completionPath, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Installed zsh completion to: "+completionPath))

	// Check if .zshrc configures fpath
	zshrcPath := filepath.Join(homeDir, ".zshrc")
	// Clean and validate the path to prevent path traversal
	cleanZshrcPath, err := validateRcPath("zshrc", zshrcPath)
	if err != nil {
		return err
	}
	// #nosec G304 -- zshrcPath is constructed from trusted os.UserHomeDir() and a constant filename
	zshrcContent, err := os.ReadFile(cleanZshrcPath)
	needsFpath := true
	if err == nil {
		if strings.Contains(string(zshrcContent), userCompletionDir) {
			needsFpath = false
		}
	}

	if needsFpath {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To enable completions, add the following to your ~/.zshrc:"))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "  fpath=(~/.zsh/completions $fpath)\n")
		fmt.Fprintf(os.Stderr, "  autoload -Uz compinit && compinit\n")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Then restart your shell or run: source ~/.zshrc"))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Please restart your shell for completions to take effect"))
	}

	return nil
}

// installFishCompletion installs fish completion
func installFishCompletion(verbose bool, cmd *cobra.Command) error {
	shellCompletionLog.Print("Installing fish completion")

	// Generate completion script using Cobra
	var buf bytes.Buffer
	if err := cmd.GenFishCompletion(&buf, true); err != nil {
		return fmt.Errorf("fish completion generation error: %w; check that the command is initialized and retry: gh aw completion fish", err)
	}

	completionScript := buf.String()

	// Determine installation path
	homeDir, err := gitutil.UserHomeDir()
	if err != nil {
		return err
	}

	// Fish completion directory
	completionDir := filepath.Join(homeDir, ".config", "fish", "completions")
	// Use restrictive permissions (0750) following principle of least privilege
	if err := os.MkdirAll(completionDir, constants.DirPermSensitive); err != nil {
		return fmt.Errorf("create completion directory %q error: %w; check that the home directory is writable, then retry", completionDir, err)
	}

	completionPath := filepath.Join(completionDir, "gh-aw.fish")

	// Write completion file
	// Use restrictive permissions (0600) following principle of least privilege
	if err := os.WriteFile(completionPath, []byte(completionScript), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("write completion file %q error: %w; check that the directory is writable, then retry", completionPath, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Installed fish completion to: "+completionPath))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fish will automatically load completions on next shell start"))

	return nil
}

// installPowerShellCompletion installs PowerShell completion
func installPowerShellCompletion(verbose bool, cmd *cobra.Command) error {
	shellCompletionLog.Print("Installing PowerShell completion")

	// Determine PowerShell profile path
	var profileCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		profileCmd = exec.Command("powershell", "-NoProfile", "-Command", "echo $PROFILE")
	} else {
		profileCmd = exec.Command("pwsh", "-NoProfile", "-Command", "echo $PROFILE")
	}

	var profileBuf bytes.Buffer
	profileCmd.Stdout = &profileBuf
	if err := profileCmd.Run(); err != nil {
		return fmt.Errorf("PowerShell profile path lookup error: %w; ensure PowerShell is installed and available on PATH, then retry", err)
	}

	profilePath := strings.TrimSpace(profileBuf.String())

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("PowerShell profile path: "+profilePath))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To enable completions, add the following to your PowerShell profile:"))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  gh aw completion powershell | Out-String | Invoke-Expression")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Or run the following command to append it automatically:"))
	fmt.Fprintln(os.Stderr, "")
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "  gh aw completion powershell >> $PROFILE")
	} else {
		fmt.Fprintln(os.Stderr, "  echo 'gh aw completion powershell | Out-String | Invoke-Expression' >> $PROFILE")
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Then restart your shell or run: . $PROFILE"))

	return nil
}

// UninstallShellCompletion uninstalls shell completion for the detected shell
func UninstallShellCompletion(verbose bool) error {
	shellCompletionLog.Print("Starting shell completion uninstallation")

	shellType := DetectShell()
	shellCompletionLog.Printf("Detected shell type: %s", shellType)

	if shellType == ShellUnknown {
		return errors.New("could not detect shell type — expected SHELL, BASH_VERSION, ZSH_VERSION, or FISH_VERSION to identify the shell; uninstall completions manually for your shell")
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Detected shell: %s", shellType)))

	switch shellType {
	case ShellBash:
		return uninstallBashCompletion(verbose)
	case ShellZsh:
		return uninstallZshCompletion(verbose)
	case ShellFish:
		return uninstallFishCompletion(verbose)
	case ShellPowerShell:
		return uninstallPowerShellCompletion(verbose)
	default:
		return fmt.Errorf("shell completion not supported for %s — expected one of bash, zsh, fish, or powershell; choose a supported shell, for example: gh aw completion uninstall --shell bash", shellType)
	}
}

// uninstallBashCompletion uninstalls bash completion
func uninstallBashCompletion(verbose bool) error {
	shellCompletionLog.Print("Uninstalling bash completion")

	homeDir, err := gitutil.UserHomeDir()
	if err != nil {
		return err
	}

	// Check all possible locations where completion might be installed
	var possiblePaths []string

	// User-level installations
	possiblePaths = append(possiblePaths, filepath.Join(homeDir, ".bash_completion.d", "gh-aw"))

	// macOS with Homebrew
	if runtime.GOOS == "darwin" {
		brewPrefix := os.Getenv("HOMEBREW_PREFIX") //nolint:osgetenvlibrary
		if brewPrefix == "" {
			for _, prefix := range []string{constants.HomebrewPrefix, constants.UsrLocalPrefix} {
				if fileutil.DirExists(filepath.Join(prefix, "etc", "bash_completion.d")) {
					possiblePaths = append(possiblePaths, filepath.Join(prefix, "etc", "bash_completion.d", "gh-aw"))
				}
			}
		} else {
			possiblePaths = append(possiblePaths, filepath.Join(brewPrefix, "etc", "bash_completion.d", "gh-aw"))
		}
	}

	// System-wide installations (Linux)
	if runtime.GOOS == "linux" {
		possiblePaths = append(possiblePaths, constants.BashCompletionGhAwPath)
	}

	removed := false
	var lastErr error

	for _, path := range possiblePaths {
		if fileutil.FileExists(path) {
			shellCompletionLog.Printf("Found completion file at: %s", path)
			if err := os.Remove(path); err != nil {
				shellCompletionLog.Printf("Failed to remove %s: %v", path, err)
				lastErr = err
				continue
			}
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Removed bash completion from: "+path))
			removed = true
		}
	}

	if !removed {
		return errors.New("no bash completion file found to remove")
	}

	if lastErr != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Some completion files could not be removed (may require elevated permissions)"))
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Please restart your shell for changes to take effect"))

	return nil
}

// uninstallZshCompletion uninstalls zsh completion
func uninstallZshCompletion(verbose bool) error {
	shellCompletionLog.Print("Uninstalling zsh completion")

	homeDir, err := gitutil.UserHomeDir()
	if err != nil {
		return err
	}

	// Check possible locations
	completionPath := filepath.Join(homeDir, ".zsh", "completions", "_gh-aw")

	if _, err := os.Stat(completionPath); err != nil {
		return fmt.Errorf("no zsh completion file found at: %s", completionPath)
	}

	shellCompletionLog.Printf("Found completion file at: %s", completionPath)

	if err := os.Remove(completionPath); err != nil {
		return fmt.Errorf("remove completion file %q error: %w; check that the file is writable, then retry", completionPath, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Removed zsh completion from: "+completionPath))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Please restart your shell for changes to take effect"))

	return nil
}

// uninstallFishCompletion uninstalls fish completion
func uninstallFishCompletion(verbose bool) error {
	shellCompletionLog.Print("Uninstalling fish completion")

	homeDir, err := gitutil.UserHomeDir()
	if err != nil {
		return err
	}

	completionPath := filepath.Join(homeDir, ".config", "fish", "completions", "gh-aw.fish")

	if _, err := os.Stat(completionPath); err != nil {
		return fmt.Errorf("no fish completion file found at: %s", completionPath)
	}

	shellCompletionLog.Printf("Found completion file at: %s", completionPath)

	if err := os.Remove(completionPath); err != nil {
		return fmt.Errorf("remove completion file %q error: %w; check that the file is writable, then retry", completionPath, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Removed fish completion from: "+completionPath))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fish will automatically detect the removal on next shell start"))

	return nil
}

// uninstallPowerShellCompletion uninstalls PowerShell completion
func uninstallPowerShellCompletion(verbose bool) error {
	shellCompletionLog.Print("Uninstalling PowerShell completion")

	// Determine PowerShell profile path
	var profileCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		profileCmd = exec.Command("powershell", "-NoProfile", "-Command", "echo $PROFILE")
	} else {
		profileCmd = exec.Command("pwsh", "-NoProfile", "-Command", "echo $PROFILE")
	}

	var profileBuf bytes.Buffer
	profileCmd.Stdout = &profileBuf
	if err := profileCmd.Run(); err != nil {
		return fmt.Errorf("PowerShell profile path lookup error: %w; ensure PowerShell is installed and available on PATH, then retry", err)
	}

	profilePath := strings.TrimSpace(profileBuf.String())

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("PowerShell profile path: "+profilePath))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To uninstall completions, remove the following line from your PowerShell profile:"))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  gh aw completion powershell | Out-String | Invoke-Expression")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Then restart your shell or run: . $PROFILE"))

	return nil
}
