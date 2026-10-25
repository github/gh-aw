package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var bootstrapLog = logger.New("cli:bootstrap")

const (
	bootstrapMCPConfigPath    = ".github/mcp.json"
	bootstrapCopilotSetupPath = ".github/workflows/copilot-setup-steps.yml"
	bootstrapAgenticSkillPath = ".github/skills/agentic-workflows/SKILL.md"
	bootstrapAgenticAgentPath = ".github/agents/agentic-workflows.md"
)

func missingBootstrapInitMarkers(baseDir string, engineOverride string) ([]string, error) {
	markers := expectedBootstrapInitMarkers(engineOverride)
	bootstrapLog.Printf("Checking %d bootstrap init markers in %s (engine=%q)", len(markers), baseDir, engineOverride)
	missing := make([]string, 0)
	for _, marker := range markers {
		ok, err := isBootstrapInitMarkerSatisfied(baseDir, marker)
		if err != nil {
			bootstrapLog.Printf("Failed to inspect bootstrap marker %s: %v", marker, err)
			return nil, err
		}
		if !ok {
			bootstrapLog.Printf("Bootstrap marker not satisfied: %s", marker)
			missing = append(missing, marker)
		}
	}
	bootstrapLog.Printf("Bootstrap marker check complete: %d of %d markers missing", len(missing), len(markers))
	return missing, nil
}

func isBootstrapInitMarkerSatisfied(baseDir string, marker string) (bool, error) {
	markerPath := filepath.Join(baseDir, filepath.FromSlash(marker))
	info, err := os.Stat(markerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to inspect %s: %w", marker, err)
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}

	switch marker {
	case ".gitattributes":
		content, err := os.ReadFile(markerPath)
		if err != nil {
			return false, fmt.Errorf("failed to inspect %s: %w", marker, err)
		}
		return strings.Contains(string(content), constants.WorkflowsLockYmlGitAttributesEntry), nil
	case bootstrapMCPConfigPath:
		return isBootstrapMCPMarkerSatisfied(markerPath, marker)
	case bootstrapCopilotSetupPath:
		content, err := os.ReadFile(markerPath)
		if err != nil {
			return false, fmt.Errorf("failed to inspect %s: %w", marker, err)
		}
		steps := string(content)
		hasLegacyInstall := strings.Contains(steps, "install-gh-aw.sh") ||
			(strings.Contains(steps, "Install gh-aw extension") && strings.Contains(steps, "curl -fsSL"))
		hasActionInstall := strings.Contains(steps, "actions/setup-cli")
		return hasLegacyInstall || hasActionInstall, nil
	case bootstrapAgenticSkillPath:
		expected, err := buildAgenticWorkflowsSkillContent()
		if err != nil {
			return false, fmt.Errorf("failed to inspect %s: %w", marker, err)
		}
		content, err := os.ReadFile(markerPath)
		if err != nil {
			return false, fmt.Errorf("failed to inspect %s: %w", marker, err)
		}
		return strings.TrimSpace(string(content)) == strings.TrimSpace(expected), nil
	case bootstrapAgenticAgentPath:
		expected, err := buildAgenticWorkflowsAgentContent(baseDir)
		if err != nil {
			return false, fmt.Errorf("failed to inspect %s: %w", marker, err)
		}
		content, err := os.ReadFile(markerPath)
		if err != nil {
			return false, fmt.Errorf("failed to inspect %s: %w", marker, err)
		}
		return strings.TrimSpace(string(content)) == strings.TrimSpace(expected), nil
	default:
		return info.Size() > 0, nil
	}
}

func isBootstrapMCPMarkerSatisfied(markerPath, marker string) (bool, error) {
	content, err := os.ReadFile(markerPath)
	if err != nil {
		return false, fmt.Errorf("failed to inspect %s: %w", marker, err)
	}
	var config MCPConfig
	if err := json.Unmarshal(content, &config); err != nil {
		return false, nil
	}
	servers := config.MCPServers
	if len(servers) == 0 {
		servers = config.Servers
	}
	server, ok := servers["github-agentic-workflows"]
	if !ok {
		return false, nil
	}
	if strings.TrimSpace(server.Command) != "gh" {
		return false, nil
	}
	return len(server.Args) >= 2 && server.Args[0] == "aw" && server.Args[1] == "mcp-server", nil
}

func expectedBootstrapInitMarkers(engineOverride string) []string {
	markers := []string{
		".gitattributes",
		".vscode/settings.json",
	}
	if engineOverride == "" || engineOverride == "copilot" {
		markers = append(markers,
			bootstrapAgenticSkillPath,
			bootstrapAgenticAgentPath,
			bootstrapMCPConfigPath,
			bootstrapCopilotSetupPath,
		)
	}
	return markers
}

func withWorkingDir(dir string, fn func() error) error {
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to read current directory: %w", err)
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("failed to change directory to %s: %w", dir, err)
	}
	bootstrapLog.Printf("Changed working directory to %s (will restore %s)", dir, originalDir)
	defer func() {
		_ = os.Chdir(originalDir)
	}()
	return fn()
}
