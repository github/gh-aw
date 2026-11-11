package workflow

import "strconv"

// AddSecurityAdvisoryCommentConfig enables comments on advisories in the workflow repository.
type AddSecurityAdvisoryCommentConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
}

func (c *Compiler) parseAddSecurityAdvisoryCommentConfig(outputMap map[string]any) *AddSecurityAdvisoryCommentConfig {
	value, exists := outputMap["add-security-advisory-comment"]
	if !exists {
		return nil
	}
	config := &AddSecurityAdvisoryCommentConfig{}
	config.Max = defaultIntStr(1)
	if configMap, ok := value.(map[string]any); ok {
		c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 1)
		if footer, ok := configMap["footer"].(bool); ok {
			value := strconv.FormatBool(footer)
			config.Footer = &value
		}
	}
	return config
}

func validateSafeOutputsAddSecurityAdvisoryComment(config *SafeOutputsConfig) error {
	if config == nil || config.AddSecurityAdvisoryComment == nil {
		return nil
	}
	return validateSecurityAdvisoryAuthentication(config, "add-security-advisory-comment", &config.AddSecurityAdvisoryComment.BaseSafeOutputConfig)
}
