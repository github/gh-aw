package cli

import (
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var codespaceLog = logger.New("cli:codespace")

// isRunningInCodespace checks if the current process is running in a GitHub Codespace
// by checking for the CODESPACES environment variable
func isRunningInCodespace() bool {
	// GitHub Codespaces sets CODESPACES=true environment variable
	isCodespace := strings.ToLower(os.Getenv("CODESPACES")) == "true"
	codespaceLog.Printf("Codespace detection: is_codespace=%v", isCodespace)
	return isCodespace
}

// is403PermissionError checks if an error message contains indicators of a 403 permission error
func is403PermissionError(errorMsg string) bool {
	errorLower := strings.ToLower(errorMsg)
	// Check for common 403 error patterns
	is403 := strings.Contains(errorLower, "403") ||
		strings.Contains(errorLower, "forbidden") ||
		(strings.Contains(errorLower, "permission") && strings.Contains(errorLower, "denied"))
	if is403 {
		codespaceLog.Print("Detected 403 permission error")
	}
	return is403
}

// getCodespacePermissionErrorMessage returns a helpful error message for codespace users
// experiencing 403 permission errors when running workflows
func getCodespacePermissionErrorMessage() string {
	codespaceLog.Print("Generating codespace permission error message")
	var msg strings.Builder

	msg.WriteString("\n")
	msg.WriteString(console.FormatErrorMessage("GitHub Codespace Permission Error"))
	msg.WriteString("\n\n")

	msg.WriteString("The default GitHub token in Codespaces does not have 'actions:write' and\n")
	msg.WriteString("'workflows:write' permissions, which are required to trigger GitHub Actions workflows.\n\n")

	msg.WriteString("Solutions:\n")
	msg.WriteString("1. Configure custom permissions in your devcontainer.json:\n")
	msg.WriteString("   Add 'actions:write' and 'workflows:write' to the 'customizations.codespaces.repositories' section.\n")
	msg.WriteString("   See: https://docs.github.com/en/codespaces/managing-your-codespaces/managing-repository-access-for-your-codespaces\n\n")
	msg.WriteString("2. Clear the GH_TOKEN and authenticate manually:\n")
	msg.WriteString("   Run: unset GH_TOKEN && gh auth login\n")
	msg.WriteString("   This will allow you to authenticate with a token that has the required permissions.\n\n")

	return msg.String()
}
