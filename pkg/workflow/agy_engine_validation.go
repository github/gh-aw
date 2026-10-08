package workflow

import (
	"errors"
	"fmt"
	"strings"
)

func validateAgyEngineConfig(config *EngineConfig) error {
	if config == nil {
		return nil
	}
	if config.Auth != nil {
		return errors.New("experimental agy supports GEMINI_API_KEY only; engine.auth ADC/WIF is not supported. Retain engine: gemini for Google WIF")
	}
	if config.LLMProvider != "" {
		return errors.New("experimental agy uses the Gemini API-key provider; engine.provider and engine.model-provider overrides are not supported")
	}
	if len(config.Args) > 0 || config.Bare || config.APITarget != "" {
		return errors.New("experimental agy does not support engine.args, engine.bare or engine.api-target; its verified headless profile must not be overridden")
	}
	if config.PermissionMode != "" {
		return errors.New("experimental agy cannot enforce engine.permission-mode; use the verified native profile inside the gh-aw sandbox")
	}
	if config.Config != "" || config.Cwd != "" {
		return errors.New("experimental agy does not support engine.config or engine.cwd; its native MCP configuration must remain in the repository workspace")
	}
	if config.HarnessScript != "" || config.Driver != "" || config.InlineDriver != nil ||
		config.HarnessMaxRetries != "" || config.HarnessInitialDelayMs != "" ||
		config.HarnessBackoffMultiplier != "" || config.HarnessMaxDelayMs != "" || config.HarnessWatchdogTimeoutMs != "" {
		return errors.New("experimental agy does not support engine.harness or engine.driver overrides; its verified timeout and authentication profile must not be replaced")
	}
	if config.MaxTurns != "" || config.MaxContinuations > 0 {
		return errors.New("experimental agy does not support max-turns or max-continuations; use max-turn-cache-misses, max-ai-credits and timeout-minutes to bound execution")
	}
	if config.Version != "" && config.Version != "1.3.1" && !strings.Contains(config.Version, "${{") {
		return fmt.Errorf("experimental agy has a verified native archive for version 1.3.1 only; requested engine.version %q is not supported", config.Version)
	}
	return nil
}
