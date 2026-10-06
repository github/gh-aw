package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const updateDriveMemoryJobName = "update_drive_memory"

func generateDriveMemorySteps(builder *strings.Builder, data *WorkflowData, pinAction func(string) string) {
	if data.DriveMemoryConfig == nil || len(data.DriveMemoryConfig.Drives) == 0 {
		return
	}

	threatDetectionEnabled := IsDetectionJobEnabled(data.SafeOutputs)
	var githubConfig *GitHubToolConfig
	if data.ParsedTools != nil {
		githubConfig = data.ParsedTools.GitHub
	}
	integrityLevel := cacheIntegrityLevel(githubConfig)
	builder.WriteString("      # Experimental drive memory file share configuration\n")
	for i, drive := range data.DriveMemoryConfig.Drives {
		mountPath := driveMemoryMountPathFor(drive.ID)
		memoryDir := driveMemoryDirFor(drive.ID)

		fmt.Fprintf(builder, "      - name: Checkout drive-memory file share (%s)\n", drive.ID)
		fmt.Fprintf(builder, "        id: checkout_drive_memory_%d\n", i)
		fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/gh-drives-preview/checkout"))
		builder.WriteString("        with:\n")
		fmt.Fprintf(builder, "          drive-name: %q\n", drive.DriveName)
		fmt.Fprintf(builder, "          path: %q\n", mountPath)
		if drive.DiskSize != "" {
			fmt.Fprintf(builder, "          disk-size: %q\n", drive.DiskSize)
		}
		if drive.Prefetch {
			builder.WriteString("          prefetch: true\n")
		}
		if drive.RestoreOnly || threatDetectionEnabled {
			builder.WriteString("          write: false\n")
		} else {
			builder.WriteString("          write: true\n")
		}
		if threatDetectionEnabled && !drive.RestoreOnly {
			generateDriveMemoryBaselineSteps(builder, data, drive, mountPath, pinAction)
		}

		fmt.Fprintf(builder, "      - name: Link drive-memory directory (%s)\n", drive.ID)
		builder.WriteString("        shell: bash\n")
		builder.WriteString("        run: |\n")
		fmt.Fprintf(builder, "          rm -rf %q\n", memoryDir)
		fmt.Fprintf(builder, "          ln -s \"$GITHUB_WORKSPACE/%s\" %q\n", mountPath, memoryDir)
		fmt.Fprintf(builder, "          if [ -d \"$GITHUB_WORKSPACE/.git/info\" ] && ! grep -qxF '/%s/' \"$GITHUB_WORKSPACE/.git/info/exclude\"; then\n", mountPath)
		fmt.Fprintf(builder, "            printf '%%s\\n' '/%s/' >> \"$GITHUB_WORKSPACE/.git/info/exclude\"\n", mountPath)
		builder.WriteString("          fi\n")

		generateDriveMemoryGitSetupStep(builder, drive, memoryDir, integrityLevel)
	}
}

func generateDriveMemoryBaselineSteps(builder *strings.Builder, data *WorkflowData, drive DriveMemoryEntry, mountPath string, pinAction func(string) string) {
	fmt.Fprintf(builder, "      - name: Capture drive-memory baseline (%s)\n", drive.ID)
	fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/github-script"))
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          MEMORY_DIR: ${{ github.workspace }}/%s\n", mountPath)
	fmt.Fprintf(builder, "          BASELINE_PATH: %s\n", driveMemoryBaselinePathFor(drive.ID))
	builder.WriteString("        with:\n")
	builder.WriteString("          script: |\n")
	builder.WriteString("            const fs = require('fs');\n")
	builder.WriteString("            const { memoryTreeDigest } = require('${{ runner.temp }}/gh-aw/actions/memory_custom_validation.cjs');\n")
	builder.WriteString("            fs.writeFileSync(process.env.BASELINE_PATH, memoryTreeDigest(process.env.MEMORY_DIR) + '\\n', 'utf8');\n")
	fmt.Fprintf(builder, "      - name: Upload drive-memory baseline (%s)\n", drive.ID)
	fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/upload-artifact"))
	builder.WriteString("        with:\n")
	fmt.Fprintf(builder, "          name: %sdrive-memory-baseline-%s\n", artifactPrefixExprForDownstreamJob(data), drive.ID)
	fmt.Fprintf(builder, "          path: %s\n", driveMemoryBaselinePathFor(drive.ID))
	builder.WriteString("          retention-days: 1\n")
}

func generateDriveMemoryGitSetupStep(builder *strings.Builder, drive DriveMemoryEntry, memoryDir, integrityLevel string) {
	fmt.Fprintf(builder, "      - name: Setup drive-memory git repository (%s)\n", drive.ID)
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          GH_AW_CACHE_DIR: %s\n", memoryDir)
	fmt.Fprintf(builder, "          GH_AW_MIN_INTEGRITY: %s\n", integrityLevel)
	if len(drive.AllowedExtensions) > 0 {
		escaped := strings.ReplaceAll(strings.Join(drive.AllowedExtensions, ":"), "'", "''")
		fmt.Fprintf(builder, "          GH_AW_ALLOWED_EXTENSIONS: '%s'\n", escaped)
	}
	// Drive and cache memory use the same git initialization contract: GH_AW_CACHE_DIR
	// points to a writable worktree and GH_AW_MIN_INTEGRITY controls the minimum level.
	builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/setup_cache_memory_git.sh\"\n")
}

func generateDriveMemoryGitCommitSteps(builder *strings.Builder, data *WorkflowData) {
	if data.DriveMemoryConfig == nil {
		return
	}
	for _, drive := range data.DriveMemoryConfig.Drives {
		if drive.RestoreOnly {
			continue
		}
		fmt.Fprintf(builder, "      - name: Commit drive-memory changes (%s)\n", drive.ID)
		builder.WriteString("        if: always()\n")
		builder.WriteString("        env:\n")
		fmt.Fprintf(builder, "          GH_AW_CACHE_DIR: %s\n", driveMemoryDirFor(drive.ID))
		builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/commit_cache_memory_git.sh\"\n")
	}
}

func generateDriveMemoryValidation(builder *strings.Builder, data *WorkflowData) {
	if data.DriveMemoryConfig == nil {
		return
	}
	for _, drive := range data.DriveMemoryConfig.Drives {
		if drive.RestoreOnly || !driveHasValidationStep(drive) {
			continue
		}
		allowedExtensions, _ := json.Marshal(drive.AllowedExtensions) //nolint:jsonmarshalignoredeerror
		fmt.Fprintf(builder, "      - name: Validate drive-memory file types (%s)\n", drive.ID)
		fmt.Fprintf(builder, "        id: %s\n", driveMemoryValidationStepID(drive.ID))
		builder.WriteString("        if: always()\n")
		fmt.Fprintf(builder, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
		builder.WriteString("        env:\n")
		fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", driveMemoryDirFor(drive.ID))
		fmt.Fprintf(builder, "          MEMORY_ID: %s\n", drive.ID)
		fmt.Fprintf(builder, "          ALLOWED_EXTENSIONS: '%s'\n", allowedExtensions)
		if drive.Validation != nil {
			fmt.Fprintf(builder, "          VALIDATION_SCRIPT_B64: %s\n", memoryValidationScriptBase64(drive.Validation))
			fmt.Fprintf(builder, "          VALIDATION_TIMEOUT_SECONDS: %d\n", memoryValidationTimeoutSeconds(drive.Validation))
		}
		builder.WriteString("        with:\n")
		builder.WriteString("          script: |\n")
		builder.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
		builder.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
		builder.WriteString("            const { validateMemoryStep } = require('${{ runner.temp }}/gh-aw/actions/validate_memory_step.cjs');\n")
		builder.WriteString("            validateMemoryStep(core, { kind: 'drive', writeMarker: true });\n")
	}
}

func generateDriveMemoryPersistence(builder *strings.Builder, data *WorkflowData, pinAction func(string) string) {
	if data.DriveMemoryConfig == nil {
		return
	}
	threatDetectionEnabled := IsDetectionJobEnabled(data.SafeOutputs)
	prefix := artifactPrefixExprForDownstreamJob(data)
	for _, drive := range data.DriveMemoryConfig.Drives {
		if drive.RestoreOnly {
			continue
		}
		if threatDetectionEnabled {
			fmt.Fprintf(builder, "      - name: Upload drive-memory data as artifact (%s)\n", drive.ID)
			if driveHasValidationStep(drive) {
				fmt.Fprintf(builder, "        if: always() && steps.%s.outcome == 'success'\n", driveMemoryValidationStepID(drive.ID))
			} else {
				builder.WriteString("        if: always()\n")
			}
			fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/upload-artifact"))
			builder.WriteString("        with:\n")
			fmt.Fprintf(builder, "          name: %sdrive-memory-%s\n", prefix, drive.ID)
			builder.WriteString("          include-hidden-files: true\n")
			fmt.Fprintf(builder, "          path: ${{ github.workspace }}/%s\n", driveMemoryMountPathFor(drive.ID))
			builder.WriteString("          retention-days: 1\n")
			continue
		}

		fmt.Fprintf(builder, "      - name: Commit drive-memory file share (%s)\n", drive.ID)
		if driveHasValidationStep(drive) {
			fmt.Fprintf(builder, "        if: success() && steps.%s.outcome == 'success'\n", driveMemoryValidationStepID(drive.ID))
		}
		fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/gh-drives-preview/commit"))
		builder.WriteString("        with:\n")
		fmt.Fprintf(builder, "          drive-name: %q\n", drive.DriveName)
		fmt.Fprintf(builder, "          path: %q\n", driveMemoryMountPathFor(drive.ID))
	}
}

func buildDriveMemoryPromptSection(config *DriveMemoryConfig) *PromptSection {
	if config == nil || len(config.Drives) == 0 {
		return nil
	}
	var content strings.Builder
	content.WriteString("## Experimental Drive Memory\n\n")
	content.WriteString("Persistent GitHub Drive storage is available at:\n")
	for _, drive := range config.Drives {
		fmt.Fprintf(&content, "- **%s**: `%s/`", drive.ID, driveMemoryDirFor(drive.ID))
		if drive.Description != "" {
			fmt.Fprintf(&content, " - %s", drive.Description)
		}
		if drive.RestoreOnly {
			content.WriteString(" (read-only persistence; local changes are not saved)")
		}
		content.WriteByte('\n')
	}
	content.WriteString("\nRead and write memory files only in these folders. Files persist across workflow runs after validation.")
	return &PromptSection{Content: content.String()}
}

func generateDriveMemoryArtifactName(data *WorkflowData, id string) string {
	return artifactPrefixExprForAgentDownstreamJob(data) + "drive-memory-" + id
}

func generateDriveMemoryBaselineArtifactName(data *WorkflowData, id string) string {
	return artifactPrefixExprForAgentDownstreamJob(data) + "drive-memory-baseline-" + id
}

func (c *Compiler) buildUpdateDriveMemoryJob(data *WorkflowData, threatDetectionEnabled bool) (*Job, error) {
	if !threatDetectionEnabled || data.DriveMemoryConfig == nil {
		return nil, nil
	}

	var steps []string
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)
		traceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		steps = append(steps, c.generateSetupStepForJob("update_drive_memory", data, setupActionRef, SetupActionDestination, false, traceID, setupParentSpanNeedsExpr(constants.ActivationJobName), "")...)
	}

	hasWritableDrive := false
	for _, drive := range data.DriveMemoryConfig.Drives {
		if drive.RestoreOnly {
			continue
		}
		hasWritableDrive = true
		steps = append(steps, c.buildDriveMemoryUpdateSteps(data, drive)...)
	}
	if !hasWritableDrive {
		return nil, nil
	}

	agentSucceeded := BuildEquals(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
		BuildStringLiteral("success"),
	)
	condition := RenderCondition(BuildAnd(BuildAnd(BuildFunctionCall("always"), buildDetectionSuccessCondition()), agentSucceeded))
	perms := NewPermissionsEmpty()
	perms.Set(PermissionContents, PermissionRead)
	perms.Set(PermissionIdToken, PermissionWrite)
	perms.Set(PermissionDrives, PermissionWrite)
	return &Job{
		Name:        updateDriveMemoryJobName,
		RunsOn:      "runs-on: ubuntu-latest",
		If:          condition,
		Permissions: perms.RenderToYAML(),
		Needs:       []string{string(constants.AgentJobName), string(constants.DetectionJobName), string(constants.ActivationJobName)},
		Steps:       steps,
	}, nil
}

func (c *Compiler) buildDriveMemoryUpdateSteps(data *WorkflowData, drive DriveMemoryEntry) []string {
	mountPath := driveMemoryMountPathFor(drive.ID)
	steps := []string{
		c.buildDriveMemoryUpdateCheckoutStep(drive, mountPath),
		c.buildDriveMemoryBaselineDownloadStep(data, drive),
		c.buildDriveMemoryConflictCheckStep(drive, mountPath),
		buildDriveMemoryClearStep(drive, mountPath),
		c.buildDriveMemoryDownloadStep(data, drive, mountPath),
	}
	if validation := buildDriveMemoryUpdateValidationStep(data, drive, mountPath); validation != "" {
		steps = append(steps, validation)
	}
	return append(steps, c.buildDriveMemoryUpdateCommitStep(drive, mountPath))
}

func (c *Compiler) buildDriveMemoryUpdateCheckoutStep(drive DriveMemoryEntry, mountPath string) string {
	var step strings.Builder
	fmt.Fprintf(&step, "      - name: Checkout drive-memory for update (%s)\n", drive.ID)
	fmt.Fprintf(&step, "        id: %s\n", memoryValidationStepID("checkout_drive", drive.ID))
	fmt.Fprintf(&step, "        uses: %s\n", c.getActionPin("actions/gh-drives-preview/checkout"))
	step.WriteString("        with:\n")
	fmt.Fprintf(&step, "          drive-name: %q\n", drive.DriveName)
	fmt.Fprintf(&step, "          path: %q\n", mountPath)
	if drive.DiskSize != "" {
		fmt.Fprintf(&step, "          disk-size: %q\n", drive.DiskSize)
	}
	step.WriteString("          write: true\n")
	return step.String()
}

func (c *Compiler) buildDriveMemoryBaselineDownloadStep(data *WorkflowData, drive DriveMemoryEntry) string {
	var step strings.Builder
	fmt.Fprintf(&step, "      - name: Download drive-memory baseline (%s)\n", drive.ID)
	fmt.Fprintf(&step, "        uses: %s\n", c.getActionPin("actions/download-artifact"))
	step.WriteString("        with:\n")
	fmt.Fprintf(&step, "          name: %s\n", generateDriveMemoryBaselineArtifactName(data, drive.ID))
	fmt.Fprintf(&step, "          path: ${{ runner.temp }}/gh-aw/drive-memory-baseline-%s\n", drive.ID)
	return step.String()
}

func (c *Compiler) buildDriveMemoryConflictCheckStep(drive DriveMemoryEntry, mountPath string) string {
	var step strings.Builder
	fmt.Fprintf(&step, "      - name: Check drive-memory for concurrent updates (%s)\n", drive.ID)
	fmt.Fprintf(&step, "        uses: %s\n", c.getActionPin("actions/github-script"))
	step.WriteString("        env:\n")
	fmt.Fprintf(&step, "          MEMORY_DIR: ${{ github.workspace }}/%s\n", mountPath)
	fmt.Fprintf(&step, "          BASELINE_PATH: ${{ runner.temp }}/gh-aw/drive-memory-baseline-%s/%s\n", drive.ID, driveMemoryBaselineFilenameFor(drive.ID))
	step.WriteString("        with:\n")
	step.WriteString("          script: |\n")
	step.WriteString("            const fs = require('fs');\n")
	step.WriteString("            const { memoryTreeDigest } = require('${{ runner.temp }}/gh-aw/actions/memory_custom_validation.cjs');\n")
	step.WriteString("            const expected = fs.readFileSync(process.env.BASELINE_PATH, 'utf8').trim();\n")
	step.WriteString("            const actual = memoryTreeDigest(process.env.MEMORY_DIR);\n")
	step.WriteString("            if (actual !== expected) core.setFailed('Drive memory changed during threat detection; refusing to overwrite a newer version');\n")
	return step.String()
}

func buildDriveMemoryClearStep(drive DriveMemoryEntry, mountPath string) string {
	var step strings.Builder
	fmt.Fprintf(&step, "      - name: Clear drive-memory before update (%s)\n", drive.ID)
	step.WriteString("        shell: bash\n")
	step.WriteString("        run: |\n")
	fmt.Fprintf(&step, "          find \"$GITHUB_WORKSPACE/%s\" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +\n", mountPath)
	return step.String()
}

func (c *Compiler) buildDriveMemoryDownloadStep(data *WorkflowData, drive DriveMemoryEntry, mountPath string) string {
	var step strings.Builder
	fmt.Fprintf(&step, "      - name: Download drive-memory artifact (%s)\n", drive.ID)
	fmt.Fprintf(&step, "        uses: %s\n", c.getActionPin("actions/download-artifact"))
	step.WriteString("        with:\n")
	fmt.Fprintf(&step, "          name: %s\n", generateDriveMemoryArtifactName(data, drive.ID))
	fmt.Fprintf(&step, "          path: ${{ github.workspace }}/%s\n", mountPath)
	return step.String()
}

func buildDriveMemoryUpdateValidationStep(data *WorkflowData, drive DriveMemoryEntry, mountPath string) string {
	if !driveHasValidationStep(drive) {
		return ""
	}
	allowedExtensions, _ := json.Marshal(drive.AllowedExtensions) //nolint:jsonmarshalignoredeerror
	var step strings.Builder
	fmt.Fprintf(&step, "      - name: Validate drive-memory before commit (%s)\n", drive.ID)
	fmt.Fprintf(&step, "        id: %s\n", driveMemoryValidationStepID(drive.ID))
	fmt.Fprintf(&step, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	step.WriteString("        env:\n")
	fmt.Fprintf(&step, "          MEMORY_DIR: ${{ github.workspace }}/%s\n", mountPath)
	fmt.Fprintf(&step, "          MEMORY_ID: %s\n", drive.ID)
	fmt.Fprintf(&step, "          ALLOWED_EXTENSIONS: '%s'\n", allowedExtensions)
	if drive.Validation != nil {
		fmt.Fprintf(&step, "          VALIDATION_SCRIPT_B64: %s\n", memoryValidationScriptBase64(drive.Validation))
		fmt.Fprintf(&step, "          VALIDATION_TIMEOUT_SECONDS: %d\n", memoryValidationTimeoutSeconds(drive.Validation))
	}
	step.WriteString("        with:\n")
	step.WriteString("          script: |\n")
	step.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	step.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	step.WriteString("            const { validateMemoryStep } = require('${{ runner.temp }}/gh-aw/actions/validate_memory_step.cjs');\n")
	step.WriteString("            validateMemoryStep(core, { kind: 'drive' });\n")
	return step.String()
}

func (c *Compiler) buildDriveMemoryUpdateCommitStep(drive DriveMemoryEntry, mountPath string) string {
	var step strings.Builder
	fmt.Fprintf(&step, "      - name: Commit drive-memory file share (%s)\n", drive.ID)
	if driveHasValidationStep(drive) {
		fmt.Fprintf(&step, "        if: steps.%s.outcome == 'success'\n", driveMemoryValidationStepID(drive.ID))
	}
	fmt.Fprintf(&step, "        uses: %s\n", c.getActionPin("actions/gh-drives-preview/commit"))
	step.WriteString("        with:\n")
	fmt.Fprintf(&step, "          drive-name: %q\n", drive.DriveName)
	fmt.Fprintf(&step, "          path: %q\n", mountPath)
	return step.String()
}
