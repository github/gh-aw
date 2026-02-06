package cli

// Codemod represents a single code transformation that can be applied to workflow files
type Codemod struct {
	ID           string // Unique identifier for the codemod
	Name         string // Human-readable name
	Description  string // Description of what the codemod does
	IntroducedIn string // Version where this codemod was introduced
	Apply        func(content string, frontmatter map[string]any) (string, bool, error)
}

// CodemodResult represents the result of applying a codemod
type CodemodResult struct {
	Applied bool   // Whether the codemod was applied
	Message string // Description of what changed
}

// GetAllCodemods returns all available codemods in the registry
func GetAllCodemods() []Codemod {
	return []Codemod{
		getTimeoutMinutesCodemod(),
		getNetworkFirewallCodemod(),
		getCommandToSlashCommandCodemod(),
		getSafeInputsModeCodemod(),
		getUploadAssetsCodemod(),
		getWritePermissionsCodemod(),
		getPermissionsReadCodemod(), // Fix permissions: read -> permissions: read-all
		getAgentTaskToAgentSessionCodemod(),
		getSandboxAgentFalseRemovalCodemod(),
		getScheduleAtToAroundCodemod(),
		getDeleteSchemaFileCodemod(),
		getGrepToolRemovalCodemod(),
		getMCPNetworkMigrationCodemod(),
		getDiscussionFlagRemovalCodemod(),
		getMCPModeToTypeCodemod(),
		getInstallScriptURLCodemod(),
		getBashAnonymousRemovalCodemod(), // Replace bash: with bash: false
	}
}
