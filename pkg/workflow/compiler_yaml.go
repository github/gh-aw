package workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var compilerYamlLog = logger.New("workflow:compiler_yaml")

// buildJobsAndValidate builds all workflow jobs and validates their dependencies.
// It resets the job manager, builds jobs from the workflow data, and performs
// dependency and duplicate step validation.
func (c *Compiler) buildJobsAndValidate(data *WorkflowData, markdownPath string) error {
	compilerYamlLog.Printf("Building and validating jobs for workflow: %s", data.Name)

	// Reset job manager for this compilation
	c.jobManager = NewJobManager()

	// Build all jobs
	if err := c.buildJobs(data, markdownPath); err != nil {
		compilerYamlLog.Printf("Failed to build jobs: %v", err)
		return fmt.Errorf("job generation could not complete; check that each configured job has valid step fields such as run, uses, and with: %w", err)
	}

	compilerYamlLog.Printf("Built %d jobs successfully", len(c.jobManager.GetAllJobs()))

	// Validate job dependencies
	if err := c.jobManager.ValidateDependencies(); err != nil {
		return fmt.Errorf("job dependency validation expected each needs entry to reference an existing job id (for example, needs: build): %w", err)
	}

	// Validate no duplicate steps within jobs (compiler bug detection)
	if err := c.jobManager.ValidateDuplicateSteps(); err != nil {
		return fmt.Errorf("duplicate step validation found repeated step names within a job; ensure each step name is unique per job: %w", err)
	}

	return nil
}

// generateWorkflowBody generates the main workflow structure including name, triggers,
// permissions, concurrency, run-name, environment variables, cache comments, and jobs.
func (c *Compiler) generateWorkflowBody(yaml *strings.Builder, data *WorkflowData) {
	// Write basic workflow structure
	fmt.Fprintf(yaml, "name: \"%s\"\n", data.Name) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.

	// Inject on.workflow_call.outputs when workflow_call is configured and safe-outputs are present
	onSection := data.On
	if data.SafeOutputs != nil {
		onSection = c.injectWorkflowCallOutputs(onSection, data.SafeOutputs)
	}
	// Inject aw_context input into workflow_dispatch triggers so dispatched workflows
	// can receive caller metadata (repo, run_id, actor, etc.) from dispatch_workflow.
	// String-based injection preserves existing YAML comments and formatting.
	onSection = injectAwContextIntoOnYAML(onSection)
	if isWorkQueueEnabled(data) && isWorkQueueWorker(data) {
		onSection = injectWorkQueueClaimIntoOnYAML(onSection)
	}
	onSection = injectNetworkAllowedIntoOnYAML(onSection, data.NetworkPermissions)
	onSection = UnquoteYAMLTopLevelKey(onSection, "on")
	yaml.WriteString(onSection)
	yaml.WriteString("\n\n")

	// Note: GitHub Actions doesn't support workflow-level if conditions
	// The workflow_run safety check is added to individual jobs instead

	// Always write empty permissions at the top level
	// Agent permissions are applied only to the agent job
	yaml.WriteString("# Jobs receive only their explicitly declared permissions.\n")
	yaml.WriteString("permissions: {}\n\n")

	yaml.WriteString(data.Concurrency)
	yaml.WriteString("\n\n")
	yaml.WriteString(data.RunName)
	yaml.WriteString("\n\n")

	// Add env section if present
	if data.Env != "" {
		yaml.WriteString(data.Env)
		yaml.WriteString("\n\n")
	}

	// Add cache comment if cache configuration was provided
	if data.Cache != "" {
		yaml.WriteString("# Cache configuration from frontmatter was processed and added to the main job steps\n\n")
	}

	// Generate jobs section using JobManager — write directly to avoid an
	// intermediate string allocation.
	c.jobManager.WriteJobsYAML(yaml)
}

func (c *Compiler) generateYAML(data *WorkflowData, markdownPath string) (string, []string, []string, error) { //nolint:largefunc // Existing workflow YAML assembly keeps generation steps in order.
	compilerYamlLog.Printf("Generating YAML for workflow: %s", data.Name)

	repoConfig, err := c.loadRepoConfig()
	if err != nil {
		return "", nil, nil, err
	}
	// Compute frontmatter hash BEFORE building jobs so that the stable hash is
	// available to heredoc-delimiter generation throughout job construction.
	// Using the hex-encoded SHA-256 frontmatter hash string as an HMAC key keeps
	// the compiled lock file identical across repeated compilations of the same workflow.
	var frontmatterHash string
	var bodyHash string
	if markdownPath != "" {
		baseDir := filepath.Dir(markdownPath)
		cache := parser.NewImportCache(baseDir)

		// computeWorkflowHash calls the parsed-content path when RawMarkdown is
		// available (fast path), falling back to a disk read otherwise.
		computeWorkflowHash := func(
			fromParsed func() (string, error),
			fromFile func() (string, error),
		) (string, error) {
			if data.RawMarkdown != "" {
				return fromParsed()
			}
			compilerYamlLog.Printf("RawMarkdown not set; falling back to reading file from disk: %s", markdownPath)
			return fromFile()
		}

		hash, err := computeWorkflowHash(
			func() (string, error) {
				return parser.ComputeFrontmatterHashFromParsedContent(data.FrontmatterYAML, data.RawMarkdown, data.RawFrontmatter, baseDir, cache, parser.DefaultFileReader)
			},
			func() (string, error) {
				return parser.ComputeFrontmatterHashFromFileWithParsedFrontmatter(markdownPath, data.RawFrontmatter, cache, parser.DefaultFileReader)
			},
		)
		if err != nil {
			return "", nil, nil, fmt.Errorf("workflow YAML generation requires a stable frontmatter hash for %q; ensure the workflow file is readable and frontmatter parses correctly: %w", markdownPath, err)
		}
		frontmatterHash = hash
		compilerYamlLog.Printf("Computed frontmatter hash: %s", hash)

		// Compute body hash to cover changes to the markdown body that are not captured
		// by the frontmatter hash. This enables stale-check: full detection.
		bHash, bErr := computeWorkflowHash(
			func() (string, error) {
				return parser.ComputeBodyHashFromParsedContent(data.RawMarkdown, data.FrontmatterYAML, baseDir, parser.DefaultFileReader)
			},
			func() (string, error) {
				return parser.ComputeBodyHashFromFile(markdownPath)
			},
		)
		if bErr != nil {
			compilerYamlLog.Printf("Warning: could not compute body hash for %q: %v", markdownPath, bErr)
			// Non-fatal: continue without body hash
		} else {
			bodyHash = bHash
			compilerYamlLog.Printf("Computed body hash: %s", bodyHash)
		}
	}
	// Store hash on WorkflowData so job-building helpers (MCP renderers, prompt
	// step generators, etc.) can derive stable heredoc delimiters from it.
	data.FrontmatterHash = frontmatterHash
	data.BodyHash = bodyHash
	data = workflowDataWithArtifactRetention(data, repoConfig)

	// Build all jobs and validate dependencies
	if err := c.buildJobsAndValidate(data, markdownPath); err != nil {
		return "", nil, nil, fmt.Errorf("workflow compilation requires valid jobs and dependencies; check job definitions and needs references: %w", err)
	}
	repoConfig, err = resolvedArtifactRetentionConfig(repoConfig, data)
	if err != nil {
		return "", nil, nil, err
	}

	// Pre-allocate builder capacity based on estimated workflow size.
	// Copilot/Claude workflows with safe-outputs typically compile to ~70–90 KB.
	// 96 KB avoids the first reallocation for the common case. The performance
	// benefit of this function comes from eliminating the intermediate copies
	// that RenderToYAML + WriteString used to incur, not from capacity reduction.
	const initialBuilderCapacity = 96 * 1024
	var yaml strings.Builder
	yaml.Grow(initialBuilderCapacity)

	// Generate workflow body first so we can collect secrets and custom actions
	// for inclusion in the header comment.
	var body strings.Builder
	body.Grow(initialBuilderCapacity)
	c.generateWorkflowBody(&body, data)
	bodyContent, err := applyArtifactRetention(body.String(), repoConfig)
	if err != nil {
		return "", nil, nil, err
	}

	// Collect secrets and external action references from the generated body.
	// These are returned to the caller so they can be used for safe update enforcement
	// without requiring a second scan of the full YAML content.
	secrets := CollectSecretReferences(bodyContent)
	actions := CollectActionReferences(bodyContent)

	// If this workflow has a workflow_call trigger, inject on.workflow_call.secrets:
	// declarations so callers can map secrets explicitly instead of using secrets: inherit.
	// We update data.On and regenerate the body so the compiled output includes the
	// declarations. The set of secrets does not change between the two passes (the
	// injected declarations do not add new ${{ secrets.* }} references).
	if hasWorkflowCallTrigger(data.On) && len(secrets) > 0 {
		updatedOn := injectWorkflowCallSecretsSection(data.On, secrets)
		if updatedOn != data.On {
			data.On = updatedOn
			body.Reset()
			body.Grow(initialBuilderCapacity)
			c.generateWorkflowBody(&body, data)
			bodyContent, err = applyArtifactRetention(body.String(), repoConfig)
			if err != nil {
				return "", nil, nil, err
			}
			compilerYamlLog.Printf("Regenerated workflow body with on.workflow_call.secrets declarations")
		}
	}

	// Generate workflow header comments (including metadata as first line, plus secrets/actions lists)
	if err := c.generateWorkflowHeader(&yaml, data, frontmatterHash, bodyHash, secrets, actions); err != nil {
		return "", nil, nil, fmt.Errorf("workflow header rendering could not complete; ensure lockfile metadata inputs are valid: %w", err)
	}

	// Append the workflow body
	yaml.WriteString(bodyContent)

	yamlContent := yaml.String()

	// If we're in non-cloning trial mode and this workflow has issue triggers,
	// replace github.event.issue.number with inputs.issue_number
	if c.trialMode && c.hasIssueTrigger(data.On) {
		compilerYamlLog.Print("Trial mode enabled, replacing issue number references")
		yamlContent = c.replaceIssueNumberReferences(yamlContent)
	}

	yamlContent, err = finalizeRunnerTempSafety(yamlContent)
	if err != nil {
		return "", nil, nil, fmt.Errorf("runner temp safety rewriting could not complete; ensure generated run/script steps are valid shell or JavaScript commands: %w", err)
	}

	// Normalize assembled YAML whitespace. This clears indentation-only blank lines
	// everywhere, trims trailing whitespace on structural YAML lines, preserves
	// block-scalar payload content, caps over-long structural blank runs, and
	// ensures the file ends with exactly one trailing newline.
	yamlContent = normalizeBlankLines(yamlContent)

	compilerYamlLog.Printf("Successfully generated YAML for workflow: %s (%d bytes)", data.Name, len(yamlContent))
	return yamlContent, secrets, actions, nil
}
