package workflow

import "errors"

// RequestContinuationConfig records a follow-up request without dispatching work.
type RequestContinuationConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
}

func (c *Compiler) parseRequestContinuationConfig(outputMap map[string]any) *RequestContinuationConfig {
	value, exists := outputMap["request-continuation"]
	if !exists || value == false {
		return nil
	}
	config := &RequestContinuationConfig{}
	config.Max = defaultIntStr(1)
	if fields, ok := value.(map[string]any); ok {
		c.parseBaseSafeOutputConfig(fields, &config.BaseSafeOutputConfig, 1)
	}
	return config
}

func validateRequestContinuationAuthorization(config *SafeOutputsConfig) error {
	if config != nil && config.RequestContinuation != nil && config.RequestContinuation.Authorization != nil {
		return errors.New("safe-outputs.request-continuation.authorization is not supported because continuation requests are recorded in the conclusion step")
	}
	return nil
}
