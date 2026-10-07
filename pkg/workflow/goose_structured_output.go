package workflow

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	gooseStructuredOutputVersion       = "1.53.0"
	gooseStructuredOutputHarnessSHA256 = "3426ab236a2262ec50d939a0f9db70953e3b5251dbec84325dc377db4fe58b92"
)

var _ StructuredOutputConfigValidator = (*BehaviorDefinedEngine)(nil)

func (e *BehaviorDefinedEngine) ValidateStructuredOutputConfig(config *EngineConfig) error {
	if e.GetID() != "goose" {
		return nil
	}
	if config != nil {
		if config.Command != "" {
			return errors.New("structured-output for engine 'goose' requires the verified native binary: remove engine.command")
		}
		if len(config.Args) > 0 {
			return errors.New("structured-output for engine 'goose' requires native recipe arguments: remove engine.args")
		}
		if config.HarnessScript != "" || config.Driver != "" || config.InlineDriver != nil {
			return errors.New("structured-output for engine 'goose' requires its native harness: remove engine.harness and engine.driver overrides")
		}
	}
	version := e.definition.Version
	if config != nil && config.Version != "" {
		version = config.Version
	}
	if version != gooseStructuredOutputVersion {
		return fmt.Errorf("structured-output for engine 'goose' requires verified version %s: remove engine.version or set it to %q", gooseStructuredOutputVersion, gooseStructuredOutputVersion)
	}
	behavior := e.behavior()
	if behavior == nil || behavior.Execution == nil || behavior.Execution.CommandName != "goose" || len(behavior.Execution.Args) > 0 {
		return errors.New("structured-output for engine 'goose' requires its native execution behavior: remove behaviors.execution command-name and args overrides")
	}
	// Imported behavior definitions can replace the entire harness without setting
	// engine.harness. Only the verified native harness may advertise this support.
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(behavior.HarnessScript)))
	if digest != gooseStructuredOutputHarnessSHA256 {
		return errors.New("structured-output for engine 'goose' requires its unmodified native harness: remove behaviors.harness-script overrides")
	}
	return nil
}
