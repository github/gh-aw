package workflow

const (
	claudeNpmPackageName  = "@anthropic-ai/claude-code"
	codexNpmPackageName   = "@openai/codex"
	copilotNpmPackageName = "@github/copilot"
	geminiNpmPackageName  = "@google/gemini-cli"
	piNpmPackageName      = "@earendil-works/pi-coding-agent"
)

func builtinEngineNpmPackage(runtimeID string) string {
	switch runtimeID {
	case "copilot":
		return copilotNpmPackageName
	case "claude":
		return claudeNpmPackageName
	case "codex":
		return codexNpmPackageName
	case "gemini":
		return geminiNpmPackageName
	case "pi":
		return piNpmPackageName
	default:
		return ""
	}
}
