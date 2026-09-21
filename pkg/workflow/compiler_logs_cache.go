package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var compilerLogsCacheLog = logger.New("workflow:compiler_logs_cache")

const (
	sharedLogsCacheKey  = "agentic-logs"
	sharedLogsCachePath = ".github/aw/logs"
)

func usesSharedLogsCache(data *WorkflowData) bool {
	if !strings.Contains(data.On, "schedule") {
		return false
	}
	content := data.CustomSteps + "\n" + data.MarkdownContent
	for _, command := range []string{"gh aw logs", "./gh-aw logs", "gh aw audit", "./gh-aw audit"} {
		for line := range strings.SplitSeq(content, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if strings.Contains(line, command) && strings.Contains(line, sharedLogsCachePath) {
				compilerLogsCacheLog.Printf("Shared logs cache enabled: found %q in custom steps", command)
				return true
			}
		}
	}
	return false
}

func sharedLogsCacheRestoreSteps(data *WorkflowData) []GitHubActionStep {
	if !usesSharedLogsCache(data) {
		return nil
	}
	compilerLogsCacheLog.Print("Generating shared logs cache restore step")

	return []GitHubActionStep{
		{
			"      - name: Restore shared agentic logs cache",
			"        id: restore-agentic-logs-cache",
			"        continue-on-error: true",
			"        uses: " + getCachedActionPin("actions/cache/restore", data),
			"        with:",
			"          key: " + sharedLogsCacheKey,
			"          path: " + sharedLogsCachePath,
		},
	}
}

func generateSharedLogsCacheRestoreSteps(yaml *strings.Builder, data *WorkflowData) {
	for _, step := range sharedLogsCacheRestoreSteps(data) {
		for _, line := range step {
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
	}
}
