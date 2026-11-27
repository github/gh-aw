package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

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

		skipCondition := ""
		if memory.Validation != nil {
			checkID := memoryValidationStepID("check_repo_memory_baseline", memory.ID)
			fmt.Fprintf(builder, "      - name: Check repo-memory baseline (%s)\n", memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			fmt.Fprintf(builder, "        id: %s\n", checkID)                                  //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			builder.WriteString("        if: always()\n")
			fmt.Fprintf(builder, "        uses: %s\n", getActionPin("actions/github-script")) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			builder.WriteString("        env:\n")
			fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			fmt.Fprintf(builder, "          MEMORY_ID: %s\n", memory.ID)  //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			builder.WriteString("        with:\n")
			builder.WriteString("          script: |\n")
			builder.WriteString("            const { checkRepoMemoryBaseline } = require('${{ runner.temp }}/gh-aw/actions/validate_memory_step.cjs');\n")
			builder.WriteString("            checkRepoMemoryBaseline(core);\n")
			skipCondition = fmt.Sprintf("steps.%s.outputs.skip != 'true'", checkID)
		}
		generateRepoMemorySanitizeFilenamesStep(builder, memory, memoryDir, memoryLabel, skipCondition)
		filterStepID := generateRepoMemoryFilterFilesStep(builder, memory, memoryDir, memoryLabel, skipCondition)
		validationStepID := generateRepoMemoryCustomValidationStep(builder, memory, memoryDir, memoryLabel, filterStepID, skipCondition)
		generateRepoMemoryUploadArtifactStep(builder, repoMemoryUploadStepParams{
			data:             data,
			memory:           memory,
			memoryDir:        memoryDir,
			memoryLabel:      memoryLabel,
			sanitizedID:      sanitizedID,
			prefix:           prefix,
			filterStepID:     filterStepID,
			validationStepID: validationStepID,
			skipCondition:    skipCondition,
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
func generateRepoMemorySanitizeFilenamesStep(builder *strings.Builder, memory RepoMemoryEntry, memoryDir, memoryLabel, skipCondition string) {
	fmt.Fprintf(builder, "      - name: Sanitize %s filenames (%s)\n", memoryLabel, memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "        if: always()%s\n", repoMemorySkipClause(skipCondition))      //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("        continue-on-error: true\n")
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/sanitize_repo_memory_filenames.sh\"\n")
}

// generateRepoMemoryFilterFilesStep emits the step that filters out files ineligible for
// persistence (disallowed extensions or non-matching file-glob patterns) before validation
// and upload. Allowed-extensions and file-glob are persistence filters: ineligible files
// are logged and removed here so that custom validation, the uploaded artifact, and the
// downstream push all see the same effective file set.
// It returns the step's ID (used to gate subsequent validation/upload steps on its
// successful outcome), or "" when no filter is configured for this memory.
func generateRepoMemoryFilterFilesStep(builder *strings.Builder, memory RepoMemoryEntry, memoryDir, memoryLabel, skipCondition string) string {
	if len(memory.AllowedExtensions) == 0 && len(memory.FileGlob) == 0 {
		return ""
	}
	filterStepID := repoMemoryFilterStepID(memory.ID)
	allowedExtsJSON, _ := json.Marshal(memory.AllowedExtensions)                          //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
	fmt.Fprintf(builder, "      - name: Filter %s files (%s)\n", memoryLabel, memory.ID)  //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "        id: %s\n", filterStepID)                                //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "        if: always()%s\n", repoMemorySkipClause(skipCondition)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "        uses: %s\n", getActionPin("actions/github-script"))     //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir)                 //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "          ALLOWED_EXTENSIONS: '%s'\n", allowedExtsJSON) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	if len(memory.FileGlob) > 0 {
		fmt.Fprintf(builder, "          FILE_GLOB_FILTER: \"%s\"\n", strings.Join(memory.FileGlob, " ")) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
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
func generateRepoMemoryCustomValidationStep(builder *strings.Builder, memory RepoMemoryEntry, memoryDir, memoryLabel, filterStepID, skipCondition string) string {
	if memory.Validation == nil {
		return ""
	}
	validationStepID := repoMemoryValidationStepID(memory.ID)
	fmt.Fprintf(builder, "      - name: Validate %s domain content (%s)\n", memoryLabel, memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "        id: %s\n", validationStepID)                                      //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	if filterStepID != "" {
		fmt.Fprintf(builder, "        if: always()%s && steps.%s.outcome == 'success'\n", repoMemorySkipClause(skipCondition), filterStepID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	} else {
		fmt.Fprintf(builder, "        if: always()%s\n", repoMemorySkipClause(skipCondition)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	fmt.Fprintf(builder, "        uses: %s\n", getActionPin("actions/github-script")) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "          MEMORY_ID: %s\n", memory.ID)  //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	appendMemoryValidationEnvironment(builder, memory.Validation)
	if memory.FormatJSON {
		builder.WriteString("          FORMAT_JSON: 'true'\n")
	}
	builder.WriteString("        with:\n")
	builder.WriteString("          script: |\n")
	builder.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	builder.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	builder.WriteString("            const { validateMemoryStep } = require('${{ runner.temp }}/gh-aw/actions/validate_memory_step.cjs');\n")
	builder.WriteString("            validateMemoryStep(core, { kind: 'repo', formatJSON: process.env.FORMAT_JSON === 'true', requireValidationScript: process.env.VALIDATION_SCRIPT_REQUIRED === 'true' });\n")
	return validationStepID
}

// repoMemoryUploadStepParams bundles the parameters for generateRepoMemoryUploadArtifactStep
// (kept as a struct rather than individual parameters to stay within the repo's
// function-parameter-count lint limit).
type repoMemoryUploadStepParams struct {
	data             *WorkflowData
	memory           RepoMemoryEntry
	memoryDir        string
	memoryLabel      string
	sanitizedID      string
	prefix           string
	filterStepID     string
	validationStepID string
	skipCondition    string
	pinAction        func(string) string
}

// generateRepoMemoryUploadArtifactStep emits the step that uploads the repo-memory
// directory as an artifact, gated on the filter and custom-validation steps' outcomes
// when configured, so a filter or validation failure can never result in an unfiltered
// or unvalidated directory being uploaded.
func generateRepoMemoryUploadArtifactStep(builder *strings.Builder, p repoMemoryUploadStepParams) {
	recoveryStepID := "validate_recovery_repo_" + p.sanitizedID
	generateRecoveryEnvelopeValidationStep(builder, recoveryStepID, p.data, p.pinAction, p.memoryDir)
	fmt.Fprintf(builder, "      - name: Upload %s artifact (%s)\n", p.memoryLabel, p.memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	conditions := []string{secretRedactionSuccessCondition(p.data), "steps." + recoveryStepID + ".outcome == 'success'"}
	if p.skipCondition != "" {
		conditions = append(conditions, p.skipCondition)
	}
	if p.filterStepID != "" {
		conditions = append(conditions, fmt.Sprintf("steps.%s.outcome == 'success'", p.filterStepID))
	}
	if p.validationStepID != "" {
		conditions = append(conditions, fmt.Sprintf("steps.%s.outcome == 'success'", p.validationStepID))
	}
	if len(conditions) > 0 {
		fmt.Fprintf(builder, "        if: always() && %s\n", strings.Join(conditions, " && ")) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	} else {
		builder.WriteString("        if: always()\n")
	}
	fmt.Fprintf(builder, "        uses: %s\n", p.pinAction("actions/upload-artifact")) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("        with:\n")
	fmt.Fprintf(builder, "          name: %srepo-memory-%s\n", p.prefix, p.sanitizedID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "          path: %s\n", p.memoryDir)                           //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("          retention-days: 1\n")
	builder.WriteString("          if-no-files-found: ignore\n")
}

func repoMemorySkipClause(condition string) string {
	if condition == "" {
		return ""
	}
	return " && " + condition
}

func repoMemoryFilterStepID(memoryID string) string {
	return memoryValidationStepID("filter_repo_memory", memoryID)
}

func repoMemoryValidationStepID(memoryID string) string {
	return memoryValidationStepID("validate_repo_memory", memoryID)
}

// repoMemoryCloneStepID identifies the agent job step that clones a memory branch.
// Its head output is the commit the agent starts from.
func repoMemoryCloneStepID(memoryID string) string {
	return "clone_repo_memory_" + memoryID
}

// repoMemoryBaseCommitOutput names the agent job output that passes the cloned
// commit to push_repo_memory, so the push replays only this run's changes.
func repoMemoryBaseCommitOutput(memoryID string) string {
	return "repo_memory_base_commit_" + memoryID
}

// addRepoMemoryBaseCommitOutputs exposes each memory's cloned commit from the agent job.
func addRepoMemoryBaseCommitOutputs(outputs map[string]string, data *WorkflowData) {
	if data.RepoMemoryConfig == nil {
		return
	}
	for _, memory := range data.RepoMemoryConfig.Memories {
		outputs[repoMemoryBaseCommitOutput(memory.ID)] = fmt.Sprintf("${{ steps.%s.outputs.head }}", repoMemoryCloneStepID(memory.ID))
	}
}

// generateRepoMemorySteps generates git steps for the repo-memory configuration
func (c *Compiler) generateRepoMemorySteps(builder *strings.Builder, data *WorkflowData, jobName string) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return
	}

	repoMemoryLog.Printf("Generating repo-memory steps for %d memories", len(data.RepoMemoryConfig.Memories))

	builder.WriteString("      # Repo memory git-based storage configuration from frontmatter processed below\n")

	for _, memory := range data.RepoMemoryConfig.Memories {
		targetRepo := repoMemoryTargetRepo(memory, PermissionRead)

		// Determine the memory directory
		memoryDir := constants.TmpRepoMemoryDir + memory.ID
		token := "${{ github.token }}"
		if memory.githubApp(PermissionRead) != nil && (memory.TargetRepo != "" || memory.GitHubAppRead != nil) {
			builder.WriteString(strings.Join(c.buildRepoMemoryAppTokenMintStep(memory, PermissionRead, jobName), ""))
			token = repoMemoryAppToken(memory, PermissionRead)
		}

		// Step 1: Clone the repo-memory branch
		if memory.Wiki {
			fmt.Fprintf(builder, "      - name: Clone wiki-memory branch (%s)\n", memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		} else {
			fmt.Fprintf(builder, "      - name: Clone repo-memory branch (%s)\n", memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		if jobName == string(constants.AgentJobName) {
			fmt.Fprintf(builder, "        id: %s\n", repoMemoryCloneStepID(memory.ID)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		if condition := repoMemoryAppTargetCondition(memory, PermissionRead); condition != "" {
			fmt.Fprintf(builder, "        if: %s\n", condition) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		builder.WriteString("        env:\n")
		fmt.Fprintf(builder, "          GH_TOKEN: %s\n", token) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		builder.WriteString("          GITHUB_SERVER_URL: ${{ github.server_url }}\n")
		fmt.Fprintf(builder, "          BRANCH_NAME: %s\n", memory.BranchName)     //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		fmt.Fprintf(builder, "          TARGET_REPO: %s\n", targetRepo)            //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir)              //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		fmt.Fprintf(builder, "          CREATE_ORPHAN: %t\n", memory.CreateOrphan) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/clone_repo_memory_branch.sh\"\n")
	}
}

func generateRepoMemoryBaselineValidationSteps(builder *strings.Builder, data *WorkflowData) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return
	}

	for _, memory := range data.RepoMemoryConfig.Memories {
		if memory.Validation == nil {
			continue
		}

		memoryDir := constants.TmpRepoMemoryDir + memory.ID
		fmt.Fprintf(builder, "      - name: Validate repo-memory baseline (%s)\n", memory.ID) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		fmt.Fprintf(builder, "        uses: %s\n", getActionPin("actions/github-script"))     //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		builder.WriteString("        env:\n")
		fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", memoryDir) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		fmt.Fprintf(builder, "          MEMORY_ID: %s\n", memory.ID)  //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		appendMemoryValidationEnvironment(builder, memory.Validation)
		builder.WriteString("        with:\n")
		builder.WriteString("          script: |\n")
		builder.WriteString("            const { validateRepoMemoryBaseline } = require('${{ runner.temp }}/gh-aw/actions/validate_memory_step.cjs');\n")
		builder.WriteString("            validateRepoMemoryBaseline(core);\n")
	}
}

func (c *Compiler) generateRepoMemoryStepsForAgent(builder *strings.Builder, data *WorkflowData) {
	c.generateRepoMemorySteps(builder, data, string(constants.AgentJobName))
	generateRepoMemoryBaselineValidationSteps(builder, data)
}
