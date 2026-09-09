// This file provides repository memory configuration and generation.
//
// This file handles:
//   - Repo-memory configuration structures and defaults
//   - Repo-memory tool configuration extraction and parsing
//   - Generation of per-memory GitHub token secrets
//
// See repo_memory_validation.go for validation functions.

package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var repoMemoryLog = logger.New("workflow:repo_memory")

const (
	// defaultRepoMemoryMaxFileSize is the default maximum file size in bytes (100KB).
	defaultRepoMemoryMaxFileSize = 102400
	// defaultRepoMemoryMaxPatchSize is the default maximum total patch size in bytes (10KB).
	defaultRepoMemoryMaxPatchSize = 10240
	// maxRepoMemoryPatchSize is the maximum allowed value for max-patch-size (1MB).
	maxRepoMemoryPatchSize = 1048576
)

// Pre-compiled regexes for performance (avoid recompilation in hot paths)
var (
	// branchPrefixValidPattern matches valid branch prefix characters (alphanumeric, hyphens, underscores)
	branchPrefixValidPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// RepoMemoryConfig holds configuration for repo-memory functionality
type RepoMemoryConfig struct {
	BranchPrefix string            `yaml:"branch-prefix,omitempty"` // branch prefix (default: "memory")
	Memories     []RepoMemoryEntry `yaml:"memories,omitempty"`      // repo-memory configurations
}

// RepoMemoryEntry represents a single repo-memory configuration
type RepoMemoryEntry struct {
	ID                string                  `yaml:"id"`                           // memory identifier (required for array notation)
	TargetRepo        string                  `yaml:"target-repo,omitempty"`        // target repository (default: current repo)
	BranchName        string                  `yaml:"branch-name,omitempty"`        // branch name (default: memory/{memory-id})
	FileGlob          []string                `yaml:"file-glob,omitempty"`          // file glob patterns for allowed files
	MaxFileSize       int                     `yaml:"max-file-size,omitempty"`      // maximum size per file in bytes (default: 100KB)
	MaxFileCount      int                     `yaml:"max-file-count,omitempty"`     // maximum file count per commit (default: 100)
	MaxPatchSize      int                     `yaml:"max-patch-size,omitempty"`     // maximum total patch size in bytes (default: 10KB, max: 1MB)
	Description       string                  `yaml:"description,omitempty"`        // optional description for this memory
	CreateOrphan      bool                    `yaml:"create-orphan,omitempty"`      // create orphaned branch if missing (default: true)
	AllowedExtensions []string                `yaml:"allowed-extensions,omitempty"` // allowed file extensions (default: [".json", ".jsonl", ".txt", ".md", ".csv"])
	Wiki              bool                    `yaml:"wiki,omitempty"`               // use the GitHub Wiki git repository instead of the regular repo
	FormatJSON        bool                    `yaml:"format-json,omitempty"`        // pretty-print all .json files before committing (default: false)
	Validation        *MemoryValidationConfig `yaml:"validation,omitempty"`         // optional custom JavaScript validation hook
}

// RepoMemoryToolConfig represents the configuration for repo-memory in tools
type RepoMemoryToolConfig struct {
	// Can be boolean, object, or array - handled by this file
	Raw any `yaml:"-"`
}

// generateDefaultBranchName generates a default branch name for a given memory ID and prefix
func generateDefaultBranchName(memoryID string, branchPrefix string) string {
	if branchPrefix == "" {
		branchPrefix = "memory"
	}
	return fmt.Sprintf("%s/%s", branchPrefix, memoryID)
}

// extractRepoMemoryConfig extracts repo-memory configuration from tools section.
// workflowID is used to qualify the default branch name (e.g. "memory/{workflowID}").
func (c *Compiler) extractRepoMemoryConfig(toolsConfig *ToolsConfig, workflowID string) (*RepoMemoryConfig, error) {
	if toolsConfig == nil || toolsConfig.RepoMemory == nil {
		return nil, nil
	}
	repoMemoryLog.Print("Extracting repo-memory configuration from ToolsConfig")
	config := &RepoMemoryConfig{
		BranchPrefix: "memory",
	}
	repoMemoryValue := toolsConfig.RepoMemory.Raw
	if repoMemoryValue == nil {
		repoMemoryLog.Print("Using default repo-memory configuration (nil value)")
		config.Memories = []RepoMemoryEntry{newDefaultRepoMemoryEntry(workflowID, config.BranchPrefix)}
		return config, nil
	}
	if boolValue, ok := repoMemoryValue.(bool); ok {
		if boolValue {
			repoMemoryLog.Print("Using default repo-memory configuration (boolean true)")
			config.Memories = []RepoMemoryEntry{newDefaultRepoMemoryEntry(workflowID, config.BranchPrefix)}
		} else {
			repoMemoryLog.Print("Repo-memory disabled (boolean false)")
		}
		return config, nil
	}
	if memoryArray, ok := repoMemoryValue.([]any); ok {
		memories, err := parseRepoMemoryArray(memoryArray, workflowID, config)
		if err != nil {
			return nil, err
		}
		config.Memories = memories
		return config, nil
	}
	if configMap, ok := repoMemoryValue.(map[string]any); ok {
		if err := applyRepoMemoryBranchPrefix(configMap, config); err != nil {
			return nil, err
		}
		entry, err := parseRepoMemoryEntry(configMap, workflowID, config.BranchPrefix, false)
		if err != nil {
			return nil, err
		}
		config.Memories = []RepoMemoryEntry{entry}
		return config, nil
	}
	return nil, nil
}

func newDefaultRepoMemoryEntry(workflowID, branchPrefix string) RepoMemoryEntry {
	return RepoMemoryEntry{
		ID:                "default",
		BranchName:        generateDefaultBranchName(defaultRepoMemoryBranchID(workflowID), branchPrefix),
		MaxFileSize:       defaultRepoMemoryMaxFileSize,
		MaxFileCount:      100,
		MaxPatchSize:      defaultRepoMemoryMaxPatchSize,
		CreateOrphan:      true,
		AllowedExtensions: constants.DefaultAllowedMemoryExtensions,
	}
}

func defaultRepoMemoryBranchID(workflowID string) string {
	if workflowID != "" {
		return workflowID
	}
	return "default"
}

func parseRepoMemoryArray(memoryArray []any, workflowID string, config *RepoMemoryConfig) ([]RepoMemoryEntry, error) {
	repoMemoryLog.Printf("Processing memory array with %d entries", len(memoryArray))
	if len(memoryArray) > 0 {
		if firstItem, ok := memoryArray[0].(map[string]any); ok {
			if err := applyRepoMemoryBranchPrefix(firstItem, config); err != nil {
				return nil, err
			}
		}
	}
	memories := make([]RepoMemoryEntry, 0, len(memoryArray))
	for _, item := range memoryArray {
		memoryMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entry, err := parseRepoMemoryEntry(memoryMap, workflowID, config.BranchPrefix, true)
		if err != nil {
			return nil, err
		}
		memories = append(memories, entry)
	}
	if err := validateNoDuplicateMemoryIDs(memories); err != nil {
		return nil, err
	}
	return memories, nil
}

func applyRepoMemoryBranchPrefix(configMap map[string]any, config *RepoMemoryConfig) error {
	branchPrefix, exists := configMap["branch-prefix"]
	if !exists {
		return nil
	}
	prefixStr, ok := branchPrefix.(string)
	if !ok {
		return nil
	}
	if err := validateBranchPrefix(prefixStr); err != nil {
		return err
	}
	config.BranchPrefix = prefixStr
	repoMemoryLog.Printf("Using custom branch-prefix: %s", prefixStr)
	return nil
}

func parseRepoMemoryEntry(memoryMap map[string]any, workflowID, branchPrefix string, requireID bool) (RepoMemoryEntry, error) {
	entry := newDefaultRepoMemoryEntry(workflowID, branchPrefix)
	explicitID, explicitBranchName := applyRepoMemoryIdentityFields(&entry, memoryMap)
	if requireID && entry.ID == "" {
		entry.ID = "default"
	}
	if entry.ID == "" {
		entry.ID = "default"
	}
	if !explicitBranchName {
		entry.BranchName = "" // let applyRepoMemoryDefaultBranch derive from ID
	}
	applyRepoMemoryDefaultBranch(&entry, workflowID, branchPrefix, explicitID)
	if err := applyRepoMemoryFileFields(&entry, memoryMap); err != nil {
		return RepoMemoryEntry{}, err
	}
	if err := applyRepoMemoryLimits(&entry, memoryMap); err != nil {
		return RepoMemoryEntry{}, err
	}
	if err := applyRepoMemoryOptionalFields(&entry, memoryMap); err != nil {
		return RepoMemoryEntry{}, err
	}
	finalizeRepoMemoryEntry(&entry, explicitBranchName)
	return entry, nil
}

func applyRepoMemoryIdentityFields(entry *RepoMemoryEntry, memoryMap map[string]any) (bool, bool) {
	explicitID := false
	if id, ok := memoryMap["id"].(string); ok {
		entry.ID = id
		explicitID = true
	}
	if targetRepo, ok := memoryMap["target-repo"].(string); ok {
		entry.TargetRepo = targetRepo
	}
	explicitBranchName := false
	if branchName, ok := memoryMap["branch-name"].(string); ok {
		entry.BranchName = branchName
		explicitBranchName = true
	}
	return explicitID, explicitBranchName
}

func applyRepoMemoryDefaultBranch(entry *RepoMemoryEntry, workflowID, branchPrefix string, explicitID bool) {
	if entry.BranchName != "" {
		return
	}
	branchID := entry.ID
	if !explicitID {
		branchID = defaultRepoMemoryBranchID(workflowID)
	}
	entry.BranchName = generateDefaultBranchName(branchID, branchPrefix)
}

func applyRepoMemoryFileFields(entry *RepoMemoryEntry, memoryMap map[string]any) error {
	entry.FileGlob = parseRepoMemoryStringList(memoryMap["file-glob"])
	if len(entry.FileGlob) > 0 {
		if err := validateFileGlobPatterns(entry.FileGlob); err != nil {
			return err
		}
	}
	allowedExtensions := parseRepoMemoryStringList(memoryMap["allowed-extensions"])
	if len(allowedExtensions) > 0 {
		entry.AllowedExtensions = allowedExtensions
	}
	if len(entry.AllowedExtensions) == 0 {
		entry.AllowedExtensions = constants.DefaultAllowedMemoryExtensions
	}
	return nil
}

func applyRepoMemoryLimits(entry *RepoMemoryEntry, memoryMap map[string]any) error {
	limits := []struct {
		key   string
		min   int
		max   int
		field *int
	}{
		{key: "max-file-size", min: 1, max: 104857600, field: &entry.MaxFileSize},
		{key: "max-file-count", min: 1, max: 1000, field: &entry.MaxFileCount},
		{key: "max-patch-size", min: 1, max: maxRepoMemoryPatchSize, field: &entry.MaxPatchSize},
	}
	for _, limit := range limits {
		if value, ok := parseRepoMemoryInt(memoryMap[limit.key]); ok {
			*limit.field = value
			if err := validateIntRange(value, limit.min, limit.max, limit.key); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyRepoMemoryOptionalFields(entry *RepoMemoryEntry, memoryMap map[string]any) error {
	if description, ok := memoryMap["description"].(string); ok {
		entry.Description = description
	}
	if createOrphan, ok := memoryMap["create-orphan"].(bool); ok {
		entry.CreateOrphan = createOrphan
	}
	if wiki, ok := memoryMap["wiki"].(bool); ok {
		entry.Wiki = wiki
	}
	if formatJSON, ok := memoryMap["format-json"].(bool); ok {
		entry.FormatJSON = formatJSON
	}
	validation, err := parseMemoryValidationConfig(memoryMap, "tools.repo-memory.validation")
	if err != nil {
		return err
	}
	entry.Validation = validation
	return nil
}

func finalizeRepoMemoryEntry(entry *RepoMemoryEntry, explicitBranchName bool) {
	if !entry.Wiki {
		return
	}
	if !explicitBranchName {
		entry.BranchName = "master"
	}
	entry.CreateOrphan = false
}

func parseRepoMemoryStringList(value any) []string {
	switch v := value.(type) {
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	case string:
		return []string{v}
	default:
		return nil
	}
}

func parseRepoMemoryInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case uint64:
		return int(v), true
	default:
		return 0, false
	}
}

// generateRepoMemoryArtifactUpload generates steps to upload repo-memory directories as artifacts.
// This runs at the end of the agent job (always condition) to save the state.
// pinAction resolves the upload-artifact action reference; pass c.getActionPin from Compiler methods.
func generateRepoMemoryArtifactUpload(builder *strings.Builder, data *WorkflowData, pinAction func(string) string) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return
	}

	repoMemoryLog.Printf("Generating repo-memory artifact upload steps for %d memories", len(data.RepoMemoryConfig.Memories))

	// In workflow_call context, apply the per-invocation prefix to avoid artifact name clashes.
	prefix := artifactPrefixExprForDownstreamJob(data)

	builder.WriteString("      # Upload repo memory as artifacts for push job\n")

	for _, memory := range data.RepoMemoryConfig.Memories {
		memoryDir := constants.TmpRepoMemoryDir + memory.ID
		sanitizedID := SanitizeWorkflowIDForCacheKey(memory.ID)
		memoryLabel := "repo-memory"
		if memory.Wiki {
			memoryLabel = "wiki-memory"
		}

		generateRepoMemorySanitizeFilenamesStep(builder, memory, memoryDir, memoryLabel)
		filterStepID := generateRepoMemoryFilterFilesStep(builder, memory, memoryDir, memoryLabel)
		validationStepID := generateRepoMemoryCustomValidationStep(builder, memory, memoryDir, memoryLabel, filterStepID)
		generateRepoMemoryUploadArtifactStep(builder, repoMemoryUploadStepParams{
			memory:           memory,
			memoryDir:        memoryDir,
			memoryLabel:      memoryLabel,
			sanitizedID:      sanitizedID,
			prefix:           prefix,
			filterStepID:     filterStepID,
			validationStepID: validationStepID,
			pinAction:        pinAction,
		})
	}
}

// generateRepoMemorySanitizeFilenamesStep emits the step that renames files with characters
// unsafe for artifact upload before validation and upload.
// GitHub Actions artifacts are stored on NTFS-compatible filesystems, so filenames
// must not contain: ? : * | < > " (among other characters).
// The agent may create files with these characters (e.g. "Can-we-have-a-PR?.md"),
// which causes the upload-artifact action to fail with a hard error.
// The script uses git commands (git mv for tracked files, mv for untracked) since
// repo-memory is backed by a git working tree.
func generateRepoMemorySanitizeFilenamesStep(builder *strings.Builder, memory RepoMemoryEntry, memoryDir, memoryLabel string) {
	fmt.Fprintf(builder, "      - name: Sanitize %s filenames (%s)\n", memoryLabel, memory.ID)
	builder.WriteString("        if: always()\n")
	builder.WriteString("        continue-on-error: true\n")
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir)
	builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/sanitize_repo_memory_filenames.sh\"\n")
}

// generateRepoMemoryFilterFilesStep emits the step that filters out files ineligible for
// persistence (disallowed extensions or non-matching file-glob patterns) before validation
// and upload. Allowed-extensions and file-glob are persistence filters: ineligible files
// are logged and removed here so that custom validation, the uploaded artifact, and the
// downstream push all see the same effective file set.
// It returns the step's ID (used to gate subsequent validation/upload steps on its
// successful outcome), or "" when no filter is configured for this memory.
func generateRepoMemoryFilterFilesStep(builder *strings.Builder, memory RepoMemoryEntry, memoryDir, memoryLabel string) string {
	if len(memory.AllowedExtensions) == 0 && len(memory.FileGlob) == 0 {
		return ""
	}
	filterStepID := repoMemoryFilterStepID(memory.ID)
	allowedExtsJSON, _ := json.Marshal(memory.AllowedExtensions) //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
	fmt.Fprintf(builder, "      - name: Filter %s files (%s)\n", memoryLabel, memory.ID)
	fmt.Fprintf(builder, "        id: %s\n", filterStepID)
	builder.WriteString("        if: always()\n")
	fmt.Fprintf(builder, "        uses: %s\n", getActionPin("actions/github-script"))
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir)
	fmt.Fprintf(builder, "          ALLOWED_EXTENSIONS: '%s'\n", allowedExtsJSON)
	if len(memory.FileGlob) > 0 {
		fmt.Fprintf(builder, "          FILE_GLOB_FILTER: \"%s\"\n", strings.Join(memory.FileGlob, " "))
	}
	builder.WriteString("        with:\n")
	builder.WriteString("          script: |\n")
	builder.WriteString(generateGitHubScriptWithRequire("memory_file_eligibility.cjs"))
	return filterStepID
}

// generateRepoMemoryCustomValidationStep emits the optional custom-validation step and
// returns its step ID (used to gate the subsequent upload step), or "" when no custom
// validation is configured for this memory. When filterStepID is non-empty, this step is
// skipped unless the filter step completed successfully, so a filter failure (e.g. an
// fs error while removing ineligible files) can never result in the unfiltered directory
// being validated.
func generateRepoMemoryCustomValidationStep(builder *strings.Builder, memory RepoMemoryEntry, memoryDir, memoryLabel, filterStepID string) string {
	if memory.Validation == nil {
		return ""
	}
	validationStepID := repoMemoryValidationStepID(memory.ID)
	fmt.Fprintf(builder, "      - name: Validate %s domain content (%s)\n", memoryLabel, memory.ID)
	fmt.Fprintf(builder, "        id: %s\n", validationStepID)
	if filterStepID != "" {
		fmt.Fprintf(builder, "        if: always() && steps.%s.outcome == 'success'\n", filterStepID)
	} else {
		builder.WriteString("        if: always()\n")
	}
	fmt.Fprintf(builder, "        uses: %s\n", getActionPin("actions/github-script"))
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir)
	fmt.Fprintf(builder, "          MEMORY_ID: %s\n", memory.ID)
	fmt.Fprintf(builder, "          VALIDATION_SCRIPT_B64: %s\n", memoryValidationScriptBase64(memory.Validation))
	fmt.Fprintf(builder, "          VALIDATION_TIMEOUT_SECONDS: %d\n", memoryValidationTimeoutSeconds(memory.Validation))
	if memory.FormatJSON {
		builder.WriteString("          FORMAT_JSON: 'true'\n")
	}
	builder.WriteString("        with:\n")
	builder.WriteString("          script: |\n")
	builder.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	builder.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	builder.WriteString("            const { validateMemoryStep } = require('${{ runner.temp }}/gh-aw/actions/validate_memory_step.cjs');\n")
	builder.WriteString("            validateMemoryStep(core, { kind: 'repo', formatJSON: process.env.FORMAT_JSON === 'true', requireValidationScript: true });\n")
	return validationStepID
}

// repoMemoryUploadStepParams bundles the parameters for generateRepoMemoryUploadArtifactStep
// (kept as a struct rather than individual parameters to stay within the repo's
// function-parameter-count lint limit).
type repoMemoryUploadStepParams struct {
	memory           RepoMemoryEntry
	memoryDir        string
	memoryLabel      string
	sanitizedID      string
	prefix           string
	filterStepID     string
	validationStepID string
	pinAction        func(string) string
}

// generateRepoMemoryUploadArtifactStep emits the step that uploads the repo-memory
// directory as an artifact, gated on the filter and custom-validation steps' outcomes
// when configured, so a filter or validation failure can never result in an unfiltered
// or unvalidated directory being uploaded.
func generateRepoMemoryUploadArtifactStep(builder *strings.Builder, p repoMemoryUploadStepParams) {
	fmt.Fprintf(builder, "      - name: Upload %s artifact (%s)\n", p.memoryLabel, p.memory.ID)
	var conditions []string
	if p.filterStepID != "" {
		conditions = append(conditions, fmt.Sprintf("steps.%s.outcome == 'success'", p.filterStepID))
	}
	if p.validationStepID != "" {
		conditions = append(conditions, fmt.Sprintf("steps.%s.outcome == 'success'", p.validationStepID))
	}
	if len(conditions) > 0 {
		fmt.Fprintf(builder, "        if: always() && %s\n", strings.Join(conditions, " && "))
	} else {
		builder.WriteString("        if: always()\n")
	}
	fmt.Fprintf(builder, "        uses: %s\n", p.pinAction("actions/upload-artifact"))
	builder.WriteString("        with:\n")
	fmt.Fprintf(builder, "          name: %srepo-memory-%s\n", p.prefix, p.sanitizedID)
	fmt.Fprintf(builder, "          path: %s\n", p.memoryDir)
	builder.WriteString("          retention-days: 1\n")
	builder.WriteString("          if-no-files-found: ignore\n")
}

func repoMemoryFilterStepID(memoryID string) string {
	return memoryValidationStepID("filter_repo_memory", memoryID)
}

func repoMemoryValidationStepID(memoryID string) string {
	return memoryValidationStepID("validate_repo_memory", memoryID)
}

// generateRepoMemorySteps generates git steps for the repo-memory configuration
func generateRepoMemorySteps(builder *strings.Builder, data *WorkflowData) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return
	}

	repoMemoryLog.Printf("Generating repo-memory steps for %d memories", len(data.RepoMemoryConfig.Memories))

	builder.WriteString("      # Repo memory git-based storage configuration from frontmatter processed below\n")

	for _, memory := range data.RepoMemoryConfig.Memories {
		// Determine the target repository
		targetRepo := memory.TargetRepo
		if targetRepo == "" {
			targetRepo = "${{ github.repository }}"
		}
		// For wiki mode, append .wiki to the repo path so the clone script uses the wiki git URL
		if memory.Wiki {
			targetRepo = targetRepo + ".wiki"
		}

		// Determine the memory directory
		memoryDir := constants.TmpRepoMemoryDir + memory.ID

		// Step 1: Clone the repo-memory branch
		if memory.Wiki {
			fmt.Fprintf(builder, "      - name: Clone wiki-memory branch (%s)\n", memory.ID)
		} else {
			fmt.Fprintf(builder, "      - name: Clone repo-memory branch (%s)\n", memory.ID)
		}
		builder.WriteString("        env:\n")
		builder.WriteString("          GH_TOKEN: ${{ github.token }}\n")
		builder.WriteString("          GITHUB_SERVER_URL: ${{ github.server_url }}\n")
		fmt.Fprintf(builder, "          BRANCH_NAME: %s\n", memory.BranchName)
		fmt.Fprintf(builder, "          TARGET_REPO: %s\n", targetRepo)
		fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir)
		fmt.Fprintf(builder, "          CREATE_ORPHAN: %t\n", memory.CreateOrphan)
		builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/clone_repo_memory_branch.sh\"\n")
	}
}

// buildPushRepoMemoryConcurrencyGroup builds a concurrency group key that is scoped to the
// specific (target-repo, branch) pairs being written by this push job.  Using the actual
// write targets—rather than a single repo-wide key—ensures that workflows pushing to
// different memory branches do not unnecessarily serialise or cancel each other.
//
// Key format: "push-repo-memory-${{ github.repository }}|<key1>[|<key2>…]"
//
// Each key component is percent-encoded (only `%` and `|` are encoded) before joining
// with "|", so the separator is always unambiguous even if a user-supplied branch name
// or target-repo contains a literal "|".  For memories that target a non-default
// repository, the target repo is prepended to the branch name
// (e.g., "other-owner%2Fother-repo:memory%2Fbranch" would be encoded if needed) so that
// distinct targets produce distinct concurrency groups.  The branches are sorted for a
// deterministic key regardless of the order memories are declared in the frontmatter.
func buildPushRepoMemoryConcurrencyGroup(memories []RepoMemoryEntry) string {
	branchKeys := make([]string, 0, len(memories))
	for _, m := range memories {
		key := encodeConcurrencyKeyPart(m.BranchName)
		if m.TargetRepo != "" {
			key = encodeConcurrencyKeyPart(m.TargetRepo) + ":" + key
		}
		branchKeys = append(branchKeys, key)
	}
	sort.Strings(branchKeys)
	return "push-repo-memory-${{ github.repository }}|" + strings.Join(branchKeys, "|")
}

// encodeConcurrencyKeyPart percent-encodes the characters that would otherwise make the
// concurrency group key ambiguous: "%" (to avoid double-encoding) and "|" (the separator).
// All other characters are left as-is so the key remains human-readable in workflow UIs.
func encodeConcurrencyKeyPart(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "|", "%7C")
	return s
}

// buildPushRepoMemoryJob creates a job that downloads repo-memory artifacts and pushes them to git branches
// This job runs after the agent job completes (even if it fails) and requires contents: write permission
// If threat detection is enabled, only runs if no threats were detected
func (c *Compiler) buildPushRepoMemoryJob(data *WorkflowData, threatDetectionEnabled bool) (*Job, error) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return nil, nil
	}

	repoMemoryLog.Printf("Building push_repo_memory job for %d memories (threatDetectionEnabled=%v)", len(data.RepoMemoryConfig.Memories), threatDetectionEnabled)

	setupActionRef := c.resolveActionReference("./actions/setup", data)
	steps := c.buildPushRepoMemorySetupAndCheckoutSteps(data, setupActionRef)
	steps = append(steps, c.buildPushRepoMemoryDownloadSteps(data)...)

	useRequire := setupActionRef != ""
	for _, memory := range data.RepoMemoryConfig.Memories {
		steps = append(steps, c.buildSinglePushRepoMemoryStep(data, memory, useRequire))
	}
	if c.actionMode.IsDev() {
		steps = append(steps, c.generateRestoreActionsSetupStep())
	}

	jobCondition, jobNeeds := c.buildPushRepoMemoryJobCondition(threatDetectionEnabled)
	outputs := buildPushRepoMemoryOutputs(data.RepoMemoryConfig.Memories)
	concurrencyGroup := buildPushRepoMemoryConcurrencyGroup(data.RepoMemoryConfig.Memories)
	concurrency := c.indentYAMLLines(fmt.Sprintf("concurrency:\n  group: %q\n  cancel-in-progress: false", concurrencyGroup), "    ")

	return &Job{
		Name:        pushRepoMemoryJobName,
		DisplayName: "",
		RunsOn:      c.formatFrameworkJobRunsOn(data),
		If:          jobCondition,
		Permissions: "permissions:\n      contents: write",
		Concurrency: concurrency,
		Needs:       jobNeeds,
		Steps:       steps,
		Outputs:     outputs,
	}, nil
}

// buildPushRepoMemorySetupAndCheckoutSteps builds setup, checkout, and git configuration steps.
func (c *Compiler) buildPushRepoMemorySetupAndCheckoutSteps(data *WorkflowData, setupActionRef string) []string {
	var steps []string
	if setupActionRef != "" || c.actionMode.IsScript() {
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)
		repoMemoryTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		repoMemoryParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		steps = append(steps, c.generateSetupStep(data, setupActionRef, SetupActionDestination, false, repoMemoryTraceID, repoMemoryParentSpanID)...)
	}
	var checkoutStep strings.Builder
	checkoutStep.WriteString("      - name: Checkout repository\n")
	fmt.Fprintf(&checkoutStep, "        uses: %s\n", getActionPin("actions/checkout"))
	checkoutStep.WriteString("        with:\n")
	checkoutStep.WriteString("          persist-credentials: false\n")
	checkoutStep.WriteString("          sparse-checkout: .\n")
	steps = append(steps, checkoutStep.String())
	return append(steps, c.generateGitConfigurationSteps()...)
}

// buildPushRepoMemoryDownloadSteps builds download-artifact steps for all memory entries.
func (c *Compiler) buildPushRepoMemoryDownloadSteps(data *WorkflowData) []string {
	repoMemoryPrefix := artifactPrefixExprForAgentDownstreamJob(data)
	var steps []string
	for _, memory := range data.RepoMemoryConfig.Memories {
		sanitizedID := SanitizeWorkflowIDForCacheKey(memory.ID)
		var step strings.Builder
		if memory.Wiki {
			fmt.Fprintf(&step, "      - name: Download wiki-memory artifact (%s)\n", memory.ID)
		} else {
			fmt.Fprintf(&step, "      - name: Download repo-memory artifact (%s)\n", memory.ID)
		}
		fmt.Fprintf(&step, "        uses: %s\n", c.getActionPin("actions/download-artifact"))
		step.WriteString("        continue-on-error: true\n")
		step.WriteString("        with:\n")
		fmt.Fprintf(&step, "          name: %srepo-memory-%s\n", repoMemoryPrefix, sanitizedID)
		fmt.Fprintf(&step, "          path: /tmp/gh-aw/repo-memory/%s\n", memory.ID)
		steps = append(steps, step.String())
	}
	return steps
}

// buildSinglePushRepoMemoryStep builds a single push-repo-memory step for one memory entry.
func (c *Compiler) buildSinglePushRepoMemoryStep(data *WorkflowData, memory RepoMemoryEntry, useRequire bool) string {
	targetRepo := memory.TargetRepo
	if targetRepo == "" {
		targetRepo = "${{ github.repository }}"
	}
	if memory.Wiki {
		targetRepo = targetRepo + ".wiki"
	}
	artifactDir := constants.TmpRepoMemoryDir + memory.ID
	fileGlobFilter := ""
	if len(memory.FileGlob) > 0 {
		fileGlobFilter = strings.Join(memory.FileGlob, " ")
	}
	var step strings.Builder
	if memory.Wiki {
		fmt.Fprintf(&step, "      - name: Push wiki-memory changes (%s)\n", memory.ID)
	} else {
		fmt.Fprintf(&step, "      - name: Push repo-memory changes (%s)\n", memory.ID)
	}
	fmt.Fprintf(&step, "        id: push_repo_memory_%s\n", memory.ID)
	step.WriteString("        if: always()\n")
	fmt.Fprintf(&step, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	step.WriteString("        env:\n")
	step.WriteString("          GH_TOKEN: ${{ github.token }}\n")
	step.WriteString("          GITHUB_RUN_ID: ${{ github.run_id }}\n")
	step.WriteString("          GITHUB_SERVER_URL: ${{ github.server_url }}\n")
	fmt.Fprintf(&step, "          ARTIFACT_DIR: %s\n", artifactDir)
	fmt.Fprintf(&step, "          MEMORY_ID: %s\n", memory.ID)
	fmt.Fprintf(&step, "          TARGET_REPO: %s\n", targetRepo)
	fmt.Fprintf(&step, "          BRANCH_NAME: %s\n", memory.BranchName)
	if memory.Wiki {
		fmt.Fprintf(&step, "          REPO_MEMORY_ALLOWED_REPOS: %s\n", targetRepo)
	}
	fmt.Fprintf(&step, "          MAX_FILE_SIZE: %d\n", memory.MaxFileSize)
	fmt.Fprintf(&step, "          MAX_FILE_COUNT: %d\n", memory.MaxFileCount)
	fmt.Fprintf(&step, "          MAX_PATCH_SIZE: %d\n", memory.MaxPatchSize)
	allowedExtsJSON, _ := json.Marshal(memory.AllowedExtensions) //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
	fmt.Fprintf(&step, "          ALLOWED_EXTENSIONS: '%s'\n", allowedExtsJSON)
	if fileGlobFilter != "" {
		fmt.Fprintf(&step, "          FILE_GLOB_FILTER: \"%s\"\n", fileGlobFilter)
	}
	if memory.FormatJSON {
		step.WriteString("          FORMAT_JSON: 'true'\n")
	}
	if memory.Validation != nil {
		fmt.Fprintf(&step, "          VALIDATION_SCRIPT_B64: %s\n", memoryValidationScriptBase64(memory.Validation))
		fmt.Fprintf(&step, "          VALIDATION_TIMEOUT_SECONDS: %d\n", memoryValidationTimeoutSeconds(memory.Validation))
	}
	step.WriteString("        with:\n")
	step.WriteString("          script: |\n")
	step.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	step.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	if useRequire {
		step.WriteString("            const { main } = require('" + SetupActionDestination + "/push_repo_memory.cjs');\n")
		step.WriteString("            await main();\n")
	} else {
		for _, line := range FormatJavaScriptForYAML("const { main } = require('${{ runner.temp }}/gh-aw/actions/push_repo_memory.cjs'); await main();") {
			step.WriteString(line)
		}
	}
	return step.String()
}

// buildPushRepoMemoryJobCondition computes the job condition and needs list.
func (c *Compiler) buildPushRepoMemoryJobCondition(threatDetectionEnabled bool) (string, []string) {
	agentSucceeded := BuildEquals(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
		BuildStringLiteral("success"),
	)
	notCancelled := &NotNode{Child: BuildFunctionCall("cancelled")}
	jobNeeds := []string{string(constants.AgentJobName), string(constants.ActivationJobName)}
	var jobCondition string
	if threatDetectionEnabled {
		jobCondition = RenderCondition(BuildAnd(BuildAnd(BuildAnd(BuildFunctionCall("always"), notCancelled), buildDetectionPassedCondition()), agentSucceeded))
		jobNeeds = append(jobNeeds, string(constants.DetectionJobName))
	} else {
		jobCondition = RenderCondition(BuildAnd(BuildAnd(BuildFunctionCall("always"), notCancelled), agentSucceeded))
	}
	return jobCondition, jobNeeds
}

// buildPushRepoMemoryOutputs builds the outputs map for validation failures from all memory steps.
func buildPushRepoMemoryOutputs(memories []RepoMemoryEntry) map[string]string {
	outputs := make(map[string]string)
	for _, memory := range memories {
		stepID := "push_repo_memory_" + memory.ID
		outputs["validation_failed_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outputs.validation_failed }}", stepID)
		outputs["validation_error_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outputs.validation_error }}", stepID)
		outputs["patch_size_exceeded_"+memory.ID] = fmt.Sprintf("${{ steps.%s.outputs.patch_size_exceeded }}", stepID)
	}
	return outputs
}
