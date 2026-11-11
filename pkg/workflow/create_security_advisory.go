package workflow

// CreateSecurityAdvisoryConfig enables draft advisories in the workflow repository.
type CreateSecurityAdvisoryConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
}

func (c *Compiler) parseCreateSecurityAdvisoryConfig(outputMap map[string]any) *CreateSecurityAdvisoryConfig {
	value, exists := outputMap["create-security-advisory"]
	if !exists {
		return nil
	}
	config := &CreateSecurityAdvisoryConfig{}
	config.Max = defaultIntStr(1)
	if configMap, ok := value.(map[string]any); ok {
		c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 1)
	}
	return config
}

func validateSafeOutputsCreateSecurityAdvisory(config *SafeOutputsConfig) error {
	if config == nil || config.CreateSecurityAdvisory == nil {
		return nil
	}
	return validateSecurityAdvisoryAuthentication(config, "create-security-advisory", &config.CreateSecurityAdvisory.BaseSafeOutputConfig)
}
