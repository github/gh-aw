package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func parseDiagnosticsTool(value any) ([]string, error) {
	var languages []string
	switch value := value.(type) {
	case string:
		languages = []string{value}
	case []string:
		languages = slices.Clone(value)
	case []any:
		for _, item := range value {
			language, ok := item.(string)
			if !ok {
				return nil, errors.New("tools.diagnostics must be a language or a list of languages: go, typescript, python")
			}
			languages = append(languages, language)
		}
	default:
		return nil, errors.New("tools.diagnostics must be a language or a list of languages: go, typescript, python")
	}
	if len(languages) == 0 {
		return nil, errors.New("tools.diagnostics must contain at least one language: go, typescript, python")
	}
	seen := make(map[string]struct{})
	for _, language := range languages {
		if !slices.Contains([]string{"go", "typescript", "python"}, language) {
			return nil, fmt.Errorf("tools.diagnostics has unsupported language %q; use go, typescript, or python", language)
		}
		if _, duplicate := seen[language]; duplicate {
			return nil, fmt.Errorf("tools.diagnostics contains duplicate language %q; list each language once", language)
		}
		seen[language] = struct{}{}
	}
	return languages, nil
}

func diagnosticsLanguages(data *WorkflowData) []string {
	if data == nil {
		return nil
	}
	if data.ParsedTools != nil {
		return data.ParsedTools.Diagnostics
	}
	return NewTools(data.Tools).Diagnostics
}

func diagnosticsLanguagesJSON(data *WorkflowData) string {
	languages := diagnosticsLanguages(data)
	if languages == nil {
		languages = []string{}
	}
	encoded, err := json.Marshal(languages)
	if err != nil {
		panic(fmt.Sprintf("BUG: cannot encode diagnostic languages: %v", err))
	}
	return string(encoded)
}

func validateParsedTools(data *WorkflowData) error {
	if err := data.ParsedTools.ParseError(); err != nil {
		return err
	}
	return validateDiagnosticsEngine(data)
}

func validateDiagnosticsEngine(data *WorkflowData) error {
	if len(diagnosticsLanguages(data)) == 0 {
		return nil
	}
	if data.EngineConfig != nil && data.EngineConfig.ID == "codex" {
		if !versionAtLeast(data.EngineConfig.Version, string(constants.DefaultCodexVersion), "0.159.3") {
			return errors.New("tools.diagnostics requires Codex 0.159.3 or later for post-tool hooks and per-handler trust; update engine.version")
		}
		config, err := parseCodexConfig(data.EngineConfig.Config)
		if err != nil {
			return err
		}
		if features, ok := config["features"].(map[string]any); ok && (features["hooks"] == false || features["codex_hooks"] == false) {
			return errors.New("tools.diagnostics requires Codex hooks; remove features.hooks = false from engine.config")
		}
		disableNext := false
		for _, arg := range data.EngineConfig.Args {
			disabled := arg == "--disable=hooks" || arg == "--disable=codex_hooks" ||
				(disableNext && slices.Contains([]string{"hooks", "codex_hooks"}, arg)) ||
				slices.Contains([]string{"features.hooks=false", "features.codex_hooks=false"}, strings.ReplaceAll(arg, " ", ""))
			if disabled {
				return errors.New("tools.diagnostics requires Codex hooks; remove the hook-disabling flag from engine.args")
			}
			disableNext = arg == "--disable"
		}
	}
	if data.EngineConfig != nil && (slices.Contains([]string{"pi", "claude", "codex"}, data.EngineConfig.ID) || isCopilotSDKMode(data)) {
		return nil
	}
	return errors.New("tools.diagnostics requires engine: claude, codex, pi, or Copilot SDK mode (engine: {id: copilot, copilot-sdk: true})")
}
