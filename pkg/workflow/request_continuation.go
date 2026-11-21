package workflow

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

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

	// ContinuationPolicyWorkflowDispatch relays the next run through the generated
	// continuation relay workflow with workflow_dispatch.
	ContinuationPolicyWorkflowDispatch = "workflow-dispatch"
	continuationPolicyKey              = "continuation-policy"

	legacyDurableDeprecationMessage = "'engine.durable' is deprecated. Use 'safe-outputs.request-continuation' instead. Run 'gh aw fix' to automatically migrate."
)

var (
	supportedContinuationPolicies = []string{ContinuationPolicyWorkflowDispatch}
	continuationApprovalPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
)

// RequestContinuationConfig enables durable continuation. The agent must call
// request_continuation to checkpoint its native session and request the next run,
// which the generated continuation relay dispatches.
// BaseSafeOutputConfig.Max is the maximum number of continuation runs in one
// chain (default 5), not a per-run call count.
type RequestContinuationConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	// Policy is the continuation-policy type (default workflow-dispatch).
	Policy string `yaml:"-"`
	// Approval is an optional GitHub environment with required reviewers that
	// gates the relay before it dispatches the next run.
	Approval string `yaml:"-"`
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

// EffectivePolicy returns the configured continuation-policy type.
func (c *RequestContinuationConfig) EffectivePolicy() string {
	if c == nil || c.Policy == "" {
		return ContinuationPolicyWorkflowDispatch
	}
	return c.Policy
}

func parseContinuationPolicy(raw any) (string, string, error) {
	var policyType, approval string
	switch value := raw.(type) {
	case string:
		policyType = value
	case map[string]any:
		for field := range value {
			if field != "type" && field != "approval" {
				return "", "", fmt.Errorf("safe-outputs.request-continuation.continuation-policy supports only type and approval; remove %q", field)
			}
		}
		if rawType, exists := value["type"]; exists {
			typed, ok := rawType.(string)
			if !ok {
				return "", "", errors.New("safe-outputs.request-continuation.continuation-policy.type must be a string")
			}
			policyType = typed
		}
		if rawApproval, exists := value["approval"]; exists {
			typed, ok := rawApproval.(string)
			if !ok || !continuationApprovalPattern.MatchString(typed) {
				return "", "", errors.New("safe-outputs.request-continuation.continuation-policy.approval must be a literal GitHub environment name (letters, digits, '.', '_' or '-'); expressions are not supported because the relay reads it from compiled metadata")
			}
			approval = typed
		}
	default:
		return "", "", errors.New("safe-outputs.request-continuation.continuation-policy must be a string or an object with type and approval")
	}
	if policyType == "" {
		policyType = ContinuationPolicyWorkflowDispatch
	}
	if !slices.Contains(supportedContinuationPolicies, policyType) {
		return "", "", fmt.Errorf("safe-outputs.request-continuation.continuation-policy.type %q is not supported; supported values: %s", policyType, strings.Join(supportedContinuationPolicies, ", "))
	}
	return policyType, approval, nil
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
		config.parseError = errors.New("safe-outputs.request-continuation must be an object with optional max (1-100), continuation-policy and authorization")
		return config
	}
	for field := range object {
		if field != "max" && field != "authorization" && field != continuationPolicyKey {
			config.parseError = fmt.Errorf("safe-outputs.request-continuation supports only max, continuation-policy and authorization; remove %q", field)
			return config
		}
	}
	if rawPolicy, exists := object[continuationPolicyKey]; exists {
		policyType, approval, err := parseContinuationPolicy(rawPolicy)
		if err != nil {
			config.parseError = err
			return config
		}
		config.Policy, config.Approval = policyType, approval
	}
	if value, exists := object["max"]; exists {
		maxValue, ok := typeutil.ParseIntValue(value)
		if !ok || maxValue < 1 || maxValue > maxRequestContinuationMax {
			config.parseError = fmt.Errorf("safe-outputs.request-continuation.max must be an integer from 1 to %d", maxRequestContinuationMax)
			return config
		}
	}
	c.parseBaseSafeOutputConfig(object, &config.BaseSafeOutputConfig, 0)
	requestContinuationLog.Printf("Parsed request-continuation: max=%d policy=%s approval=%t", config.EffectiveMax(), config.EffectivePolicy(), config.Approval != "")
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

const (
	// continuationRequestArtifactPrefix names the artifact holding a continuation request;
	// the requesting run attempt is appended.
	continuationRequestArtifactPrefix = "continuation-request-"
	continuationRequestFilePath       = "/tmp/gh-aw/continuation/continuation-request.json"
	// ContinuationRelayWorkflowFile is the generated relay workflow dispatched by the conclusion job.
	ContinuationRelayWorkflowFile = "agentic-continuation-relay.yml"
)

func hasRequestContinuation(data *WorkflowData) bool {
	return data != nil && data.SafeOutputs != nil && data.SafeOutputs.RequestContinuation != nil
}

// continuationLockMetadata returns the policy the continuation relay trusts, or nil
// when the workflow cannot request continuation.
func continuationLockMetadata(data *WorkflowData) *ContinuationLockMetadata {
	if !hasRequestContinuation(data) {
		return nil
	}
	config := data.SafeOutputs.RequestContinuation
	return &ContinuationLockMetadata{
		Workflow: durableWorkflowID(data),
		Max:      config.EffectiveMax(),
		Policy:   config.EffectivePolicy(),
		Approval: config.Approval,
	}
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
		return errors.New("safe-outputs.request-continuation relays the next run with workflow_dispatch; add workflow_dispatch to the on: triggers")
	}
	return nil
}
