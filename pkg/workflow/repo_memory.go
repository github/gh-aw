// This file provides repository memory configuration and validation.
//
// This file handles:
//   - Repo-memory configuration structures and defaults
//   - Repo-memory tool configuration extraction and parsing
//   - Generation of per-memory GitHub token secrets
//   - Domain-specific validation for repo-memory configurations
//
// # Validation Functions
//
// This file contains domain-specific validation functions for repo-memory:
//   - validateNoDuplicateMemoryIDs() - Ensures unique memory identifiers
//
// These validation functions are co-located with repo-memory logic following the
// principle that domain-specific validation belongs in domain files. See validation.go
// for the validation architecture documentation.

package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var repoMemoryLog = logger.New("workflow:repo_memory")

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
	ID           string   `yaml:"id"`                       // memory identifier (required for array notation)
	TargetRepo   string   `yaml:"target-repo,omitempty"`    // target repository (default: current repo)
	BranchName   string   `yaml:"branch-name,omitempty"`    // branch name (default: memory/{memory-id})
	FileGlob     []string `yaml:"file-glob,omitempty"`      // file glob patterns for allowed files
	MaxFileSize  int      `yaml:"max-file-size,omitempty"`  // maximum size per file in bytes (default: 10KB)
	MaxFileCount int      `yaml:"max-file-count,omitempty"` // maximum file count per commit (default: 100)
	Description  string   `yaml:"description,omitempty"`    // optional description for this memory
	CreateOrphan bool     `yaml:"create-orphan,omitempty"`  // create orphaned branch if missing (default: true)
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

// validateBranchPrefix validates that the branch prefix meets requirements
func validateBranchPrefix(prefix string) error {
	if prefix == "" {
		return nil // Empty means use default
	}

	// Check length (4-32 characters)
	if len(prefix) < 4 {
		return fmt.Errorf("branch-prefix must be at least 4 characters long, got %d", len(prefix))
	}
	if len(prefix) > 32 {
		return fmt.Errorf("branch-prefix must be at most 32 characters long, got %d", len(prefix))
	}

	// Check for alphanumeric and branch-friendly characters (alphanumeric, hyphens, underscores)
	// Use pre-compiled regex from package level for performance
	if !branchPrefixValidPattern.MatchString(prefix) {
		return fmt.Errorf("branch-prefix must contain only alphanumeric characters, hyphens, and underscores, got '%s'", prefix)
	}

	// Cannot be "copilot"
	if strings.ToLower(prefix) == "copilot" {
		return fmt.Errorf("branch-prefix cannot be 'copilot' (reserved)")
	}

	return nil
}

// extractRepoMemoryConfig extracts repo-memory configuration from tools section
func (c *Compiler) extractRepoMemoryConfig(toolsConfig *ToolsConfig) (*RepoMemoryConfig, error) {
	// Check if repo-memory tool is configured
	if toolsConfig == nil || toolsConfig.RepoMemory == nil {
		return nil, nil
	}

	repoMemoryLog.Print("Extracting repo-memory configuration from ToolsConfig")

	config := &RepoMemoryConfig{
		BranchPrefix: "memory", // Default branch prefix
	}
	repoMemoryValue := toolsConfig.RepoMemory.Raw

	// Handle nil value (simple enable with defaults) - same as true
	if repoMemoryValue == nil {
		repoMemoryLog.Print("Using default repo-memory configuration (nil value)")
		config.Memories = []RepoMemoryEntry{
			{
				ID:           "default",
				BranchName:   generateDefaultBranchName("default", config.BranchPrefix),
				MaxFileSize:  10240, // 10KB
				MaxFileCount: 100,
				CreateOrphan: true,
			},
		}
		return config, nil
	}

	// Handle boolean value (simple enable/disable)
	if boolValue, ok := repoMemoryValue.(bool); ok {
		if boolValue {
			repoMemoryLog.Print("Using default repo-memory configuration (boolean true)")
			// Create a single default memory entry
			config.Memories = []RepoMemoryEntry{
				{
					ID:           "default",
					BranchName:   generateDefaultBranchName("default", config.BranchPrefix),
					MaxFileSize:  10240, // 10KB
					MaxFileCount: 100,
					CreateOrphan: true,
				},
			}
		} else {
			repoMemoryLog.Print("Repo-memory disabled (boolean false)")
		}
		// If false, return empty config (empty array means disabled)
		return config, nil
	}

	// Handle array of memory configurations
	if memoryArray, ok := repoMemoryValue.([]any); ok {
		repoMemoryLog.Printf("Processing memory array with %d entries", len(memoryArray))
		config.Memories = make([]RepoMemoryEntry, 0, len(memoryArray))

		// Parse branch-prefix from first item if it's a map with branch-prefix key
		// This allows branch-prefix to be set at the top level for all memories
		if len(memoryArray) > 0 {
			if firstItem, ok := memoryArray[0].(map[string]any); ok {
				if branchPrefix, exists := firstItem["branch-prefix"]; exists {
					if prefixStr, ok := branchPrefix.(string); ok {
						if err := validateBranchPrefix(prefixStr); err != nil {
							return nil, err
						}
						config.BranchPrefix = prefixStr
						repoMemoryLog.Printf("Using custom branch-prefix: %s", prefixStr)
					}
				}
			}
		}

		for _, item := range memoryArray {
			if memoryMap, ok := item.(map[string]any); ok {
				entry := RepoMemoryEntry{
					MaxFileSize:  10240, // 10KB default
					MaxFileCount: 100,   // 100 files default
					CreateOrphan: true,  // create orphan by default
				}

				// ID is required for array notation
				if id, exists := memoryMap["id"]; exists {
					if idStr, ok := id.(string); ok {
						entry.ID = idStr
					}
				}
				// Use "default" if no ID specified
				if entry.ID == "" {
					entry.ID = "default"
				}

				// Parse target-repo
				if targetRepo, exists := memoryMap["target-repo"]; exists {
					if repoStr, ok := targetRepo.(string); ok {
						entry.TargetRepo = repoStr
					}
				}

				// Parse branch-name
				if branchName, exists := memoryMap["branch-name"]; exists {
					if branchStr, ok := branchName.(string); ok {
						entry.BranchName = branchStr
					}
				}
				// Set default branch name if not specified
				if entry.BranchName == "" {
					entry.BranchName = generateDefaultBranchName(entry.ID, config.BranchPrefix)
				}

				// Parse file-glob
				if fileGlob, exists := memoryMap["file-glob"]; exists {
					if globArray, ok := fileGlob.([]any); ok {
						entry.FileGlob = make([]string, 0, len(globArray))
						for _, item := range globArray {
							if str, ok := item.(string); ok {
								entry.FileGlob = append(entry.FileGlob, str)
							}
						}
					} else if globStr, ok := fileGlob.(string); ok {
						// Allow single string to be treated as array of one
						entry.FileGlob = []string{globStr}
					}
				}

				// Parse max-file-size
				if maxFileSize, exists := memoryMap["max-file-size"]; exists {
					if sizeInt, ok := maxFileSize.(int); ok {
						entry.MaxFileSize = sizeInt
					} else if sizeFloat, ok := maxFileSize.(float64); ok {
						entry.MaxFileSize = int(sizeFloat)
					} else if sizeUint64, ok := maxFileSize.(uint64); ok {
						entry.MaxFileSize = int(sizeUint64)
					}
					// Validate max-file-size bounds
					if err := validateIntRange(entry.MaxFileSize, 1, 104857600, "max-file-size"); err != nil {
						return nil, err
					}
				}

				// Parse max-file-count
				if maxFileCount, exists := memoryMap["max-file-count"]; exists {
					if countInt, ok := maxFileCount.(int); ok {
						entry.MaxFileCount = countInt
					} else if countFloat, ok := maxFileCount.(float64); ok {
						entry.MaxFileCount = int(countFloat)
					} else if countUint64, ok := maxFileCount.(uint64); ok {
						entry.MaxFileCount = int(countUint64)
					}
					// Validate max-file-count bounds
					if err := validateIntRange(entry.MaxFileCount, 1, 1000, "max-file-count"); err != nil {
						return nil, err
					}
				}

				// Parse description
				if description, exists := memoryMap["description"]; exists {
					if descStr, ok := description.(string); ok {
						entry.Description = descStr
					}
				}

				// Parse create-orphan
				if createOrphan, exists := memoryMap["create-orphan"]; exists {
					if orphanBool, ok := createOrphan.(bool); ok {
						entry.CreateOrphan = orphanBool
					}
				}

				config.Memories = append(config.Memories, entry)
			}
		}

		// Check for duplicate memory IDs
		if err := validateNoDuplicateMemoryIDs(config.Memories); err != nil {
			return nil, err
		}

		return config, nil
	}

	// Handle object configuration (single memory, backward compatible)
	// Convert to array with single entry
	if configMap, ok := repoMemoryValue.(map[string]any); ok {
		repoMemoryLog.Print("Processing object-style repo-memory configuration (backward compatible)")

		// Parse branch-prefix if provided
		if branchPrefix, exists := configMap["branch-prefix"]; exists {
			if prefixStr, ok := branchPrefix.(string); ok {
				if err := validateBranchPrefix(prefixStr); err != nil {
					return nil, err
				}
				config.BranchPrefix = prefixStr
				repoMemoryLog.Printf("Using custom branch-prefix: %s", prefixStr)
			}
		}

		entry := RepoMemoryEntry{
			ID:           "default",
			BranchName:   generateDefaultBranchName("default", config.BranchPrefix),
			MaxFileSize:  10240, // 10KB default
			MaxFileCount: 100,   // 100 files default
			CreateOrphan: true,  // create orphan by default
		}

		// Parse target-repo
		if targetRepo, exists := configMap["target-repo"]; exists {
			if repoStr, ok := targetRepo.(string); ok {
				entry.TargetRepo = repoStr
			}
		}

		// Parse branch-name
		if branchName, exists := configMap["branch-name"]; exists {
			if branchStr, ok := branchName.(string); ok {
				entry.BranchName = branchStr
			}
		}

		// Parse file-glob
		if fileGlob, exists := configMap["file-glob"]; exists {
			if globArray, ok := fileGlob.([]any); ok {
				entry.FileGlob = make([]string, 0, len(globArray))
				for _, item := range globArray {
					if str, ok := item.(string); ok {
						entry.FileGlob = append(entry.FileGlob, str)
					}
				}
			} else if globStr, ok := fileGlob.(string); ok {
				// Allow single string to be treated as array of one
				entry.FileGlob = []string{globStr}
			}
		}

		// Parse max-file-size
		if maxFileSize, exists := configMap["max-file-size"]; exists {
			if sizeInt, ok := maxFileSize.(int); ok {
				entry.MaxFileSize = sizeInt
			} else if sizeFloat, ok := maxFileSize.(float64); ok {
				entry.MaxFileSize = int(sizeFloat)
			} else if sizeUint64, ok := maxFileSize.(uint64); ok {
				entry.MaxFileSize = int(sizeUint64)
			}
			// Validate max-file-size bounds
			if err := validateIntRange(entry.MaxFileSize, 1, 104857600, "max-file-size"); err != nil {
				return nil, err
			}
		}

		// Parse max-file-count
		if maxFileCount, exists := configMap["max-file-count"]; exists {
			if countInt, ok := maxFileCount.(int); ok {
				entry.MaxFileCount = countInt
			} else if countFloat, ok := maxFileCount.(float64); ok {
				entry.MaxFileCount = int(countFloat)
			} else if countUint64, ok := maxFileCount.(uint64); ok {
				entry.MaxFileCount = int(countUint64)
			}
			// Validate max-file-count bounds
			if err := validateIntRange(entry.MaxFileCount, 1, 1000, "max-file-count"); err != nil {
				return nil, err
			}
		}

		// Parse description
		if description, exists := configMap["description"]; exists {
			if descStr, ok := description.(string); ok {
				entry.Description = descStr
			}
		}

		// Parse create-orphan
		if createOrphan, exists := configMap["create-orphan"]; exists {
			if orphanBool, ok := createOrphan.(bool); ok {
				entry.CreateOrphan = orphanBool
			}
		}

		config.Memories = []RepoMemoryEntry{entry}
		return config, nil
	}

	return nil, nil
}

// validateNoDuplicateMemoryIDs checks for duplicate memory IDs and returns an error if found
func validateNoDuplicateMemoryIDs(memories []RepoMemoryEntry) error {
	seen := make(map[string]bool)
	for _, memory := range memories {
		if seen[memory.ID] {
			return fmt.Errorf("duplicate memory ID found: '%s'. Each memory must have a unique ID", memory.ID)
		}
		seen[memory.ID] = true
	}
	return nil
}

// generateRepoMemoryArtifactUpload generates steps to upload repo-memory directories as artifacts
// This runs at the end of the agent job (always condition) to save the state
func generateRepoMemoryArtifactUpload(builder *strings.Builder, data *WorkflowData) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return
	}

	repoMemoryLog.Printf("Generating repo-memory artifact upload steps for %d memories", len(data.RepoMemoryConfig.Memories))

	builder.WriteString("      # Upload repo memory as artifacts for push job\n")

	for _, memory := range data.RepoMemoryConfig.Memories {
		// Determine the memory directory
		memoryDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s", memory.ID)

		// Step: Upload repo-memory directory as artifact
		fmt.Fprintf(builder, "      - name: Upload repo-memory artifact (%s)\n", memory.ID)
		builder.WriteString("        if: always()\n")
		fmt.Fprintf(builder, "        uses: %s\n", GetActionPin("actions/upload-artifact"))
		builder.WriteString("        with:\n")
		fmt.Fprintf(builder, "          name: repo-memory-%s\n", memory.ID)
		fmt.Fprintf(builder, "          path: %s\n", memoryDir)
		builder.WriteString("          retention-days: 1\n")
		builder.WriteString("          if-no-files-found: ignore\n")
	}
}

// generateRepoMemoryPushSteps generates steps to push changes back to the repo-memory branches
// This runs at the end of the workflow (always condition) to persist any changes made
func generateRepoMemoryPushSteps(builder *strings.Builder, data *WorkflowData) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return
	}

	repoMemoryLog.Printf("Generating repo-memory push steps for %d memories", len(data.RepoMemoryConfig.Memories))

	builder.WriteString("      # Push repo memory changes back to git branches\n")

	for _, memory := range data.RepoMemoryConfig.Memories {
		// Determine the target repository
		targetRepo := memory.TargetRepo
		if targetRepo == "" {
			targetRepo = "${{ github.repository }}"
		}

		// Determine the memory directory
		memoryDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s", memory.ID)

		// Step: Push changes to repo-memory branch
		fmt.Fprintf(builder, "      - name: Push repo-memory changes (%s)\n", memory.ID)
		builder.WriteString("        if: always()\n")
		builder.WriteString("        env:\n")
		builder.WriteString("          GH_TOKEN: ${{ github.token }}\n")
		builder.WriteString("        run: |\n")
		builder.WriteString("          set -e\n")
		fmt.Fprintf(builder, "          cd \"%s\" || exit 0\n", memoryDir)
		builder.WriteString("          \n")
		builder.WriteString("          # Check if we have any changes to commit\n")
		builder.WriteString("          if [ -n \"$(git status --porcelain)\" ]; then\n")
		builder.WriteString("            echo \"Changes detected in repo memory, committing and pushing...\"\n")
		builder.WriteString("            \n")

		// Add file validation if constraints are specified
		if len(memory.FileGlob) > 0 || memory.MaxFileSize > 0 || memory.MaxFileCount > 0 {
			builder.WriteString("            # Validate files before committing\n")

			if memory.MaxFileSize > 0 {
				fmt.Fprintf(builder, "            # Check file sizes (max: %d bytes)\n", memory.MaxFileSize)
				fmt.Fprintf(builder, "            if find . -type f -size +%dc | grep -q .; then\n", memory.MaxFileSize)
				builder.WriteString("              echo \"Error: Files exceed maximum size limit\"\n")
				fmt.Fprintf(builder, "              find . -type f -size +%dc -exec ls -lh {} \\;\n", memory.MaxFileSize)
				builder.WriteString("              exit 1\n")
				builder.WriteString("            fi\n")
				builder.WriteString("            \n")
			}

			if memory.MaxFileCount > 0 {
				fmt.Fprintf(builder, "            # Check file count (max: %d files)\n", memory.MaxFileCount)
				builder.WriteString("            FILE_COUNT=$(git status --porcelain | wc -l)\n")
				fmt.Fprintf(builder, "            if [ \"$FILE_COUNT\" -gt %d ]; then\n", memory.MaxFileCount)
				fmt.Fprintf(builder, "              echo \"Error: Too many files to commit ($FILE_COUNT > %d)\"\n", memory.MaxFileCount)
				builder.WriteString("              exit 1\n")
				builder.WriteString("            fi\n")
				builder.WriteString("            \n")
			}
		}

		builder.WriteString("            # Add all changes\n")
		builder.WriteString("            git add -A\n")
		builder.WriteString("            \n")
		builder.WriteString("            # Commit changes\n")
		builder.WriteString("            git commit -m \"Update memory from workflow run ${{ github.run_id }}\"\n")
		builder.WriteString("            \n")
		builder.WriteString("            # Pull with ours merge strategy (our changes win in conflicts)\n")
		builder.WriteString("            set +e\n")
		fmt.Fprintf(builder, "            git pull --no-rebase -s recursive -X ours \"https://x-access-token:${GH_TOKEN}@github.com/%s.git\" \"%s\" 2>&1\n",
			targetRepo, memory.BranchName)
		builder.WriteString("            PULL_EXIT_CODE=$?\n")
		builder.WriteString("            set -e\n")
		builder.WriteString("            \n")
		builder.WriteString("            # Push changes (force push if needed due to conflict resolution)\n")
		fmt.Fprintf(builder, "            git push \"https://x-access-token:${GH_TOKEN}@github.com/%s.git\" \"HEAD:%s\"\n",
			targetRepo, memory.BranchName)
		builder.WriteString("            \n")
		builder.WriteString("            echo \"Successfully pushed changes to repo memory\"\n")
		builder.WriteString("          else\n")
		builder.WriteString("            echo \"No changes in repo memory, skipping push\"\n")
		builder.WriteString("          fi\n")
	}
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

		// Determine the memory directory
		memoryDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s", memory.ID)

		// Step 1: Clone the repo-memory branch
		fmt.Fprintf(builder, "      - name: Clone repo-memory branch (%s)\n", memory.ID)
		builder.WriteString("        env:\n")
		builder.WriteString("          GH_TOKEN: ${{ github.token }}\n")
		fmt.Fprintf(builder, "          BRANCH_NAME: %s\n", memory.BranchName)
		fmt.Fprintf(builder, "          TARGET_REPO: %s\n", targetRepo)
		fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir)
		fmt.Fprintf(builder, "          CREATE_ORPHAN: %t\n", memory.CreateOrphan)
		builder.WriteString("        run: bash /opt/gh-aw/actions/clone_repo_memory_branch.sh\n")
	}
}

// buildPushRepoMemoryJob creates a job that downloads repo-memory artifacts and pushes them to git branches
// This job runs after the agent job completes (even if it fails) and requires contents: write permission
// If threat detection is enabled, only runs if no threats were detected
func (c *Compiler) buildPushRepoMemoryJob(data *WorkflowData, threatDetectionEnabled bool) (*Job, error) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return nil, nil
	}

	repoMemoryLog.Printf("Building push_repo_memory job for %d memories (threatDetectionEnabled=%v)", len(data.RepoMemoryConfig.Memories), threatDetectionEnabled)

	var steps []string

	// Add setup step to copy scripts
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)

		// Repo memory job doesn't need project support
		steps = append(steps, c.generateSetupStep(setupActionRef, SetupActionDestination, false)...)
	}

	// Add checkout step to configure git (without checking out files)
	// We use sparse-checkout to avoid downloading files since we'll checkout the memory branch
	var checkoutStep strings.Builder
	checkoutStep.WriteString("      - name: Checkout repository\n")
	fmt.Fprintf(&checkoutStep, "        uses: %s\n", GetActionPin("actions/checkout"))
	checkoutStep.WriteString("        with:\n")
	checkoutStep.WriteString("          persist-credentials: false\n")
	checkoutStep.WriteString("          sparse-checkout: .\n")
	steps = append(steps, checkoutStep.String())

	// Add git configuration step
	gitConfigSteps := c.generateGitConfigurationSteps()
	steps = append(steps, gitConfigSteps...)

	// Build steps as complete YAML strings
	for _, memory := range data.RepoMemoryConfig.Memories {
		// Download artifact step
		var step strings.Builder
		fmt.Fprintf(&step, "      - name: Download repo-memory artifact (%s)\n", memory.ID)
		fmt.Fprintf(&step, "        uses: %s\n", GetActionPin("actions/download-artifact"))
		step.WriteString("        continue-on-error: true\n")
		step.WriteString("        with:\n")
		fmt.Fprintf(&step, "          name: repo-memory-%s\n", memory.ID)
		fmt.Fprintf(&step, "          path: /tmp/gh-aw/repo-memory/%s\n", memory.ID)
		steps = append(steps, step.String())
	}

	// Determine script loading method based on action mode
	useRequire := setupActionRef != ""

	// Add push steps for each memory
	for _, memory := range data.RepoMemoryConfig.Memories {
		targetRepo := memory.TargetRepo
		if targetRepo == "" {
			targetRepo = "${{ github.repository }}"
		}

		artifactDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s", memory.ID)

		// Build file glob filter string
		fileGlobFilter := ""
		if len(memory.FileGlob) > 0 {
			fileGlobFilter = strings.Join(memory.FileGlob, " ")
		}

		// Build step with github-script action
		var step strings.Builder
		fmt.Fprintf(&step, "      - name: Push repo-memory changes (%s)\n", memory.ID)
		step.WriteString("        if: always()\n")
		fmt.Fprintf(&step, "        uses: %s\n", GetActionPin("actions/github-script"))
		step.WriteString("        env:\n")
		step.WriteString("          GH_TOKEN: ${{ github.token }}\n")
		step.WriteString("          GITHUB_RUN_ID: ${{ github.run_id }}\n")
		fmt.Fprintf(&step, "          ARTIFACT_DIR: %s\n", artifactDir)
		fmt.Fprintf(&step, "          MEMORY_ID: %s\n", memory.ID)
		fmt.Fprintf(&step, "          TARGET_REPO: %s\n", targetRepo)
		fmt.Fprintf(&step, "          BRANCH_NAME: %s\n", memory.BranchName)
		fmt.Fprintf(&step, "          MAX_FILE_SIZE: %d\n", memory.MaxFileSize)
		fmt.Fprintf(&step, "          MAX_FILE_COUNT: %d\n", memory.MaxFileCount)
		if fileGlobFilter != "" {
			// Quote the value to prevent YAML alias interpretation of patterns like *.md
			fmt.Fprintf(&step, "          FILE_GLOB_FILTER: \"%s\"\n", fileGlobFilter)
		}
		step.WriteString("        with:\n")
		step.WriteString("          script: |\n")

		if useRequire {
			// Use require() to load script from copied files using setup_globals helper
			step.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
			step.WriteString("            setupGlobals(core, github, context, exec, io);\n")
			step.WriteString("            const { main } = require('" + SetupActionDestination + "/push_repo_memory.cjs');\n")
			step.WriteString("            await main();\n")
		} else {
			// Inline JavaScript: Attach GitHub Actions builtin objects to global scope before script execution
			step.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
			step.WriteString("            setupGlobals(core, github, context, exec, io);\n")
			// Add the JavaScript script with proper indentation
			formattedScript := FormatJavaScriptForYAML("const { main } = require('/opt/gh-aw/actions/push_repo_memory.cjs'); await main();")
			for _, line := range formattedScript {
				step.WriteString(line)
			}
		}

		steps = append(steps, step.String())
	}

	// Set job condition based on threat detection
	// If threat detection is enabled, only run if detection passed
	// Otherwise, always run (even if agent job failed)
	jobCondition := "always()"
	if threatDetectionEnabled {
		jobCondition = "always() && needs.detection.outputs.success == 'true'"
	}

	job := &Job{
		Name:        "push_repo_memory",
		DisplayName: "", // No display name - job ID is sufficient
		RunsOn:      "runs-on: ubuntu-latest",
		If:          jobCondition,
		Permissions: "permissions:\n      contents: write",
		Needs:       []string{"agent"}, // Detection dependency added by caller if needed
		Steps:       steps,
	}

	return job, nil
}
