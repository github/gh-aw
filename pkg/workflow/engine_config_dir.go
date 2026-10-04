package workflow

import (
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var engineConfigDirLog = logger.New("workflow:engine_config_dir")

// engineConfigBaseDir returns the base config directory for the given engine ID,
// determined by looking up the engine in the global registry and reading the first
// AgentManifestPathPrefix from the AgentFileProvider interface.
// Falls back to ".github" when the engine is not found or provides no path prefixes.
func engineConfigBaseDir(engineID string) string {
	return engineConfigBaseDirForRegistry(GetGlobalEngineRegistry(), engineID)
}

func engineConfigBaseDirForRegistry(registry *EngineRegistry, engineID string) string {
	engine, err := registry.GetEngine(strings.ToLower(engineID))
	if err == nil {
		if provider, ok := engine.(AgentFileProvider); ok {
			if prefixes := provider.GetAgentManifestPathPrefixes(); len(prefixes) > 0 {
				baseDir := strings.TrimSuffix(prefixes[0], "/")
				engineConfigDirLog.Printf("Resolved config base dir for engine %q to %q", engineID, baseDir)
				return baseDir
			}
		}
	}
	engineConfigDirLog.Printf("Falling back to .github config base dir for engine %q (lookup err=%v)", engineID, err)
	return ".github"
}

// GetEngineSkillDir returns the relative directory (from repo root / tmp base) used
// to store inline skill files for a given engine.
//
// The directory is derived from the engine's AgentManifestPathPrefixes:
//
//	claude       → .claude/skills
//	codex        → .codex/skills
//	gemini       → .gemini/skills
//	pi           → .pi/skills
//	others       → .github/skills  (Copilot default)
func GetEngineSkillDir(engineID string) string {
	return path.Join(engineConfigBaseDir(engineID), "skills")
}

// GetEngineSubAgentDir returns the relative directory (from repo root / tmp base) used
// to store inline sub-agent files for a given engine.
//
// The directory is derived from the engine's AgentManifestPathPrefixes:
//
//	claude       → .claude/agents
//	codex        → .codex/agents
//	gemini       → .gemini/agents
//	pi           → .pi/agents
//	others       → .github/agents  (Copilot default)
func GetEngineSubAgentDir(engineID string) string {
	return path.Join(engineConfigBaseDir(engineID), "agents")
}
