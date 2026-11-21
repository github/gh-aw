package workflow

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/github/gh-aw/pkg/console"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var requestContinuationLog = logger.New("workflow:request_continuation")

const (
	requestContinuationKey        = "request-continuation"
	requestContinuationToolName   = "request_continuation"
	defaultRequestContinuationMax = 5
	maxRequestContinuationMax     = 100

	legacyDurableDeprecationMessage = "'engine.durable' is deprecated. Use 'safe-outputs.request-continuation' instead. Run 'gh aw fix' to automatically migrate."
)

// RequestContinuationConfig enables durable continuation. The agent must call
// request_continuation to checkpoint its native session and dispatch the next run.
// BaseSafeOutputConfig.Max is the maximum number of continuation runs in one
// chain (default 5), not a per-run call count.
type RequestContinuationConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	// LegacyDurable records that deprecated engine.durable was also configured.
	LegacyDurable bool `yaml:"-"`
	// parseError holds an invalid user configuration reported during validation.
	parseError error
}

// EffectiveMax returns the configured continuation chain limit.
func (c *RequestContinuationConfig) EffectiveMax() int {
	if c == nil || c.Max == nil {
		return defaultRequestContinuationMax
	}
	if value, err := strconv.Atoi(*c.Max); err == nil {
		return value
	}
	return defaultRequestContinuationMax
}

func (c *Compiler) parseRequestContinuationConfig(outputMap map[string]any) *RequestContinuationConfig {
	raw, exists := outputMap[requestContinuationKey]
	if !exists {
		return nil
	}
	if enabled, ok := raw.(bool); ok && !enabled {
		return nil
	}
	config := &RequestContinuationConfig{}
	if raw == nil {
		return config
	}
	if enabled, ok := raw.(bool); ok && enabled {
		return config
	}
	object, ok := raw.(map[string]any)
	if !ok {
		config.parseError = errors.New("safe-outputs.request-continuation must be an object with optional max (1-100) and authorization")
		return config
	}
	for field := range object {
		if field != "max" && field != "authorization" {
			config.parseError = fmt.Errorf("safe-outputs.request-continuation supports only max and authorization; remove %q", field)
			return config
		}
	}
	if value, exists := object["max"]; exists {
		maxValue, ok := typeutil.ParseIntValue(value)
		if !ok || maxValue < 1 || maxValue > maxRequestContinuationMax {
			config.parseError = fmt.Errorf("safe-outputs.request-continuation.max must be an integer from 1 to %d", maxRequestContinuationMax)
			return config
		}
	}
	c.parseBaseSafeOutputConfig(object, &config.BaseSafeOutputConfig, 0)
	requestContinuationLog.Printf("Parsed request-continuation: max=%d", config.EffectiveMax())
	return config
}

// applyRequestContinuationDurable links request-continuation and the durable
// execution machinery. Deprecated engine.durable implies request-continuation.
func applyRequestContinuationDurable(data *WorkflowData) {
	if data == nil {
		return
	}
	if data.EngineConfig != nil && data.EngineConfig.Durable != nil {
		if data.SafeOutputs == nil {
			data.SafeOutputs = &SafeOutputsConfig{}
		}
		if data.SafeOutputs.RequestContinuation == nil {
			requestContinuationLog.Print("Deprecated engine.durable implies safe-outputs.request-continuation")
			data.SafeOutputs.RequestContinuation = &RequestContinuationConfig{}
		}
		data.SafeOutputs.RequestContinuation.LegacyDurable = true
		return
	}
	if data.SafeOutputs == nil || data.SafeOutputs.RequestContinuation == nil {
		return
	}
	if data.EngineConfig == nil {
		data.EngineConfig = &EngineConfig{ID: ResolveEngineID(data)}
	}
	data.EngineConfig.Durable = &DurableConfig{RetentionDays: defaultDurableRetentionDays}
}

func hasRequestContinuation(data *WorkflowData) bool {
	return data != nil && data.SafeOutputs != nil && data.SafeOutputs.RequestContinuation != nil
}

// validateRequestContinuation runs after safe outputs are merged and before
// defaults generate the durable concurrency group.
func (c *Compiler) validateRequestContinuation(data *WorkflowData) error {
	if hasRequestContinuation(data) && data.SafeOutputs.RequestContinuation.parseError != nil {
		return data.SafeOutputs.RequestContinuation.parseError
	}
	if err := c.validateEngineDurable(data); err != nil {
		return err
	}
	if !hasRequestContinuation(data) {
		return nil
	}
	if data.SafeOutputs.RequestContinuation.LegacyDurable {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(legacyDurableDeprecationMessage))
		c.IncrementWarningCount()
	}
	return nil
}

// validateRequestContinuationTrigger runs after the on: section is finalized.
func validateRequestContinuationTrigger(data *WorkflowData) error {
	if !hasRequestContinuation(data) {
		return nil
	}
	if !onSectionHasAnyTrigger(data.On, "workflow_dispatch") {
		return errors.New("safe-outputs.request-continuation dispatches the next run with workflow_dispatch; add workflow_dispatch to the on: triggers")
	}
	return nil
}
