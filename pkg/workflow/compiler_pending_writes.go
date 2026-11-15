package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
)

const pendingWritesPath = "/tmp/gh-aw/pending-writes.json"
const pendingWritesArtifactName = "pending-writes"
const pendingLaterDecisionsPath = "/tmp/gh-aw/write-decisions.normalized.json"

type pendingWriteTransport struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	Artifact   string `json:"-"`
	Job        string `json:"-"`
	Branch     string `json:"-"`
	Repository string `json:"-"`
}

type pendingWritePublication struct {
	ID         string                  `json:"id"`
	Job        string                  `json:"job"`
	Type       string                  `json:"type"`
	Kind       string                  `json:"kind"`
	BindingID  string                  `json:"binding_id,omitempty"`
	Path       string                  `json:"path,omitempty"`
	Branch     string                  `json:"branch,omitempty"`
	Repository string                  `json:"repository,omitempty"`
	Worker     string                  `json:"worker,omitempty"`
	StorageKey string                  `json:"storage_key,omitempty"`
	DriveName  string                  `json:"drive_name,omitempty"`
	Invocation *pendingWriteInvocation `json:"invocation,omitempty"`
}

type pendingWriteSpec struct {
	Bindings         []pendingWriteTransport   `json:"bindings"`
	Publications     []pendingWritePublication `json:"publications"`
	CustomTypes      []string                  `json:"customTypes"`
	DetectionEnabled bool                      `json:"detectionEnabled"`
	DetectionMode    string                    `json:"detectionMode"`
}

func buildPendingWriteSpec(data *WorkflowData) pendingWriteSpec {
	spec := pendingWriteSpec{Bindings: []pendingWriteTransport{}, Publications: []pendingWritePublication{}, CustomTypes: []string{}, DetectionEnabled: IsDetectionJobEnabled(data.SafeOutputs), DetectionMode: "block"}
	prefix := artifactPrefixExprForAgentDownstreamJob(data)
	spec.addPendingMemorySpec(data, prefix)
	spec.addPendingCacheSpec(data, prefix)
	spec.addPendingDriveSpec(data)
	if data.Evals != nil && data.Evals.HasEvals() {
		spec.addPendingDirectory(pendingWritePublication{ID: "evals", Job: pushEvalsStateJobName, Type: "evals_state", Kind: "directory", BindingID: "evals", Path: evalsStateDir, Branch: evalsBranchName(data.WorkflowID)}, prefix+constants.EvalsArtifactName.String())
	}
	if data.LedgerConfig != nil && data.LedgerConfig.Enabled() {
		spec.Publications = append(spec.Publications, pendingWritePublication{ID: "transactions", Job: pushLedgerChangesJobName, Type: "ledger", Kind: "ledger", Path: ledgerTransactionsArtifactPath})
	}
	spec.addPendingSafeOutputSpec(data, prefix)
	slices.Sort(spec.CustomTypes)
	slices.SortFunc(spec.Publications, func(a, b pendingWritePublication) int { return strings.Compare(a.Job+"\x00"+a.ID, b.Job+"\x00"+b.ID) })
	return spec
}

func (c *Compiler) pendingWritesRuntimeEnv(data *WorkflowData) ([]string, error) {
	specJSON, err := json.Marshal(buildPendingWriteSpec(data))
	if err != nil {
		return nil, fmt.Errorf("encoding pending-write specification: %w", err)
	}
	steps := []string{
		"          GH_AW_AGENT_OUTPUT: /tmp/gh-aw/agent_output.json\n",
		"          GH_AW_PENDING_WRITES: " + pendingWritesPath + "\n",
		"          GH_AW_PENDING_WRITE_INPUTS: /tmp/gh-aw/pending-write-inputs\n",
		fmt.Sprintf("          GH_AW_PENDING_WRITES_SPEC: %q\n", specJSON),
		"          GITHUB_REPOSITORY: ${{ github.repository }}\n",
		"          GITHUB_WORKFLOW_REF: ${{ github.workflow_ref }}\n",
		"          GITHUB_WORKFLOW_SHA: ${{ github.workflow_sha }}\n",
		"          GITHUB_RUN_ID: ${{ github.run_id }}\n",
		"          GITHUB_RUN_ATTEMPT: ${{ github.run_attempt }}\n",
		"          GITHUB_EVENT_NAME: ${{ github.event_name }}\n",
		"          GITHUB_TOKEN: ${{ github.token }}\n",
		fmt.Sprintf("          GH_AW_WORKFLOW_ID: %q\n", data.WorkflowID),
	}
	readTokens := make(map[string]string)
	for _, checkout := range data.CheckoutConfigs {
		if checkout.GitHubToken == "" {
			continue
		}
		repository := checkout.Repository
		if repository == "" {
			repository = "${{ github.repository }}"
		}
		if _, exists := readTokens[repository]; !exists {
			readTokens[repository] = checkout.GitHubToken
		}
	}
	if len(readTokens) > 0 {
		tokensJSON, err := json.Marshal(readTokens)
		if err != nil {
			return nil, fmt.Errorf("encoding existing checkout read credentials: %w", err)
		}
		steps = append(steps, fmt.Sprintf("          GH_AW_PENDING_READ_TOKENS: %q\n", tokensJSON))
	}
	if data.SafeOutputs != nil && data.SafeOutputs.GitHubToken != "" {
		steps = append(steps, fmt.Sprintf("          GH_AW_PENDING_DEFAULT_READ_TOKEN: %s\n", data.SafeOutputs.GitHubToken))
	}
	if IsDetectionJobEnabled(data.SafeOutputs) {
		steps = append(steps,
			"          GH_AW_DETECTION_CONCLUSION: ${{ needs.detection.outputs.detection_conclusion }}\n",
			"          GH_AW_DETECTION_REASON: ${{ needs.detection.outputs.detection_reason }}\n",
			"          GH_AW_DETECTION_THREAT_KINDS: ${{ needs.detection.outputs.detection_threat_kinds }}\n")
	}
	c.addHandlerManagerConfigEnvVar(&steps, data)
	if data.SafeOutputs != nil {
		if staged := resolveSafeOutputsStagedValue(c.trialMode, data.SafeOutputs.Staged); staged != nil {
			steps = append(steps, fmt.Sprintf("          GH_AW_SAFE_OUTPUTS_STAGED: %q\n", *staged))
		}
	}
	return steps, nil
}

// Called immediately before authored gate steps, never for ungated workflows.
func (c *Compiler) buildPendingWritesPreparationSteps(data *WorkflowData, enabled bool) ([]string, error) {
	if !enabled {
		return nil, nil
	}
	var steps []string
	for _, binding := range buildPendingWriteSpec(data).Bindings {
		if binding.Artifact == "" {
			continue
		}
		steps = append(steps,
			fmt.Sprintf("      - name: Download pending-write input %s\n", binding.ID),
			fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
			"        with:\n",
			fmt.Sprintf("          name: %s\n", binding.Artifact),
			fmt.Sprintf("          path: %s\n", binding.Path))
	}
	steps = append(steps, "      - name: Prepare pending writes\n", "        id: pending_writes\n", fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)), "        env:\n", "          GH_AW_PENDING_WRITES_REQUIRED: 'false'\n")
	env, err := c.pendingWritesRuntimeEnv(data)
	if err != nil {
		return nil, err
	}
	steps = append(steps, env...)
	steps = append(steps, "        with:\n", "          script: |\n")
	steps = append(steps, generateGitHubScriptWithRequire("pending_writes_runtime.cjs"))
	return steps, nil
}

func (c *Compiler) buildPendingWritesUploadSteps(data *WorkflowData) []string {
	prefix := artifactPrefixExprForAgentDownstreamJob(data)
	return []string{
		"      - name: Upload pending-write manifest\n",
		"        if: always() && steps.verify_pending_writes.outputs.allowed == 'true'\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		"          name: " + prefix + pendingWritesArtifactName + "\n",
		"          path: /tmp/gh-aw/pending-write-inputs/pending-writes.json\n",
		"          retention-days: 7\n",
		"          if-no-files-found: error\n",
		"      - name: Upload frozen pending-write inputs\n",
		"        if: always() && steps.verify_pending_writes.outputs.allowed == 'true'\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		"          name: " + prefix + "pending-write-inputs\n",
		"          path: /tmp/gh-aw/pending-write-inputs/\n",
		"          retention-days: 7\n",
		"          if-no-files-found: error\n",
	}
}

func (c *Compiler) pendingWritesDownloadSteps(data *WorkflowData) []string {
	return []string{
		"      - name: Download authorized pending writes\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
		"        with:\n",
		"          name: " + artifactPrefixExprForDownstreamJob(data) + pendingWritesArtifactName + "\n",
		"          path: /tmp/gh-aw/\n",
		"      - name: Download safe-output prerequisite receipts\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
		"        with:\n",
		"          name: " + artifactPrefixExprForDownstreamJob(data) + "safe-outputs-items\n",
		"          path: /tmp/gh-aw/write-prerequisites/\n",
	}
}

func pendingPublicationForStep(publications []pendingWritePublication, step map[string]any, job string) (pendingWritePublication, bool) {
	name := pendingStepString(step, "name")
	uses := pendingStepString(step, "uses")
	for _, publication := range publications {
		if publication.Job != job {
			continue
		}
		switch job {
		case pushRepoMemoryJobName:
			if env, ok := step["env"].(map[string]any); ok && env["MEMORY_ID"] == publication.ID {
				return publication, true
			}
		case updateCacheMemoryJobName:
			if strings.Contains(uses, "actions/cache/save@") && strings.Contains(name, "("+strings.TrimPrefix(publication.ID, "cache_memory:")+")") {
				return publication, true
			}
		case updateDriveMemoryJobName:
			if strings.Contains(uses, "actions/gh-drives-preview/commit@") && strings.Contains(name, "("+strings.TrimPrefix(publication.ID, "drive_memory:")+")") {
				publication.Path = "${{ github.workspace }}/" + driveMemoryMountPathFor(strings.TrimPrefix(publication.ID, "drive_memory:"))
				return publication, true
			}
		default:
			env := pendingStepEnv(step)
			if step["id"] == job || strings.Contains(name, "Upload") || strings.Contains(name, "Push evals") || strings.Contains(name, "Reconcile and push ledger") || (job == publication.ID && env["GH_AW_AGENT_OUTPUT"] != nil) {
				return publication, true
			}
		}
	}
	return pendingWritePublication{}, false
}

// Resolve Actions first, then bind every emitted publication step to its input.
func (c *Compiler) guardPendingWritePublications(data *WorkflowData, enabled bool, publications map[string]struct{}) error { //nolint:largefunc // Publication lowering preserves step order and performs one coherent guarded rewrite per job.
	if !enabled {
		return nil
	}
	safeJob, _ := c.jobManager.GetJob("safe_outputs")
	if safeJob != nil && data.Evals != nil && data.Evals.HasEvals() && !slices.Contains(safeJob.Needs, "evals") {
		safeJob.Needs = append(safeJob.Needs, "evals")
	}
	spec := buildPendingWriteSpec(data)
	if err := c.freezePendingInvocations(&spec); err != nil {
		return err
	}
	names := make([]string, 0, len(publications))
	for name := range publications {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if name == "safe_outputs" {
			continue
		}
		job, exists := c.jobManager.GetJob(name)
		if !exists {
			continue
		}
		if hasWorkflowCallTrigger(data.On) && !slices.Contains(job.Needs, "activation") {
			job.Needs = append(job.Needs, "activation")
		}
		if job.Env == nil {
			job.Env = map[string]string{}
		}
		job.Env["GH_AW_PENDING_WRITES_REQUIRED"] = "true"
		job.Env["GH_AW_PENDING_WRITES"] = pendingWritesPath
		job.Env["GH_AW_PENDING_WRITES_SHA256"] = "${{ needs.safe_outputs.outputs.pending_writes_sha256 }}"
		job.Env["GH_AW_WRITE_GATE_DECISIONS"] = pendingLaterDecisionsPath
		job.Env["GH_AW_WRITE_GATE_DECISIONS_SHA256"] = "${{ needs.safe_outputs.outputs.write_gate_decisions_sha256 }}"
		job.Env["GH_AW_WRITE_GATE_ITEMS"] = "/tmp/gh-aw/write-prerequisites/safe-output-items.jsonl"
		job.Env["GH_AW_WRITE_GATE_ITEMS_SHA256"] = "${{ needs.safe_outputs.outputs.write_gate_items_sha256 }}"
		if job.Uses != "" {
			var publication *pendingWritePublication
			for index := range spec.Publications {
				if spec.Publications[index].Job == name && spec.Publications[index].Kind == "call" {
					publication = &spec.Publications[index]
					break
				}
			}
			if publication == nil {
				return fmt.Errorf("publication job %s has no verified call-input transport", name)
			}
			if err := c.addPendingCallVerificationJob(data, job, *publication); err != nil {
				return err
			}
			continue
		}

		var parsed struct {
			Steps []map[string]any `yaml:"steps"`
		}
		if err := yaml.Unmarshal([]byte("steps:\n"+strings.Join(job.Steps, "")), &parsed); err != nil {
			return fmt.Errorf("parsing publication steps for %s: %w", name, err)
		}
		var steps []string
		hasSetup := false
		for _, step := range parsed.Steps {
			uses := pendingStepString(step, "uses")
			run := pendingStepString(step, "run")
			if step["id"] == "setup" && (uses == c.resolveActionReference("./actions/setup", data) || strings.Contains(run, "actions/setup/setup.sh")) {
				hasSetup = true
			}
		}
		if !hasSetup {
			steps = append(steps, c.generateCheckoutActionsFolder(data)...)
			setup := c.generateSetupStepForJob(name, data, c.resolveActionReference("./actions/setup", data), SetupActionDestination, false, "", "", "")
			for _, line := range setup {
				steps = append(steps, strings.ReplaceAll(line, "id: setup\n", "id: pending_writes_setup\n"))
			}
		}
		steps = append(steps, c.pendingWritesDownloadSteps(data)...)
		steps = append(steps, "      - name: Download authorized write decisions\n", fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")), "        with:\n", "          name: "+artifactPrefixExprForDownstreamJob(data)+"write-decisions\n", "          path: /tmp/gh-aw/\n")
		checked := false
		var checkIDs []string
		for index, step := range parsed.Steps {
			if publication, writes := pendingPublicationForStep(spec.Publications, step, name); writes {
				checked = true
				id := fmt.Sprintf("verify_pending_writes_%d", index)
				checkIDs = append(checkIDs, id)
				publicationJSON, err := json.Marshal(publication)
				if err != nil {
					return fmt.Errorf("encoding pending publication %s: %w", publication.ID, err)
				}
				steps = append(steps, "      - name: Verify pending-write publication inputs\n", "        id: "+id+"\n", fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)), "        env:\n", fmt.Sprintf("          GH_AW_PENDING_PUBLICATION: %q\n", publicationJSON))
				if publication.Invocation != nil {
					envNames := make([]string, 0, len(publication.Invocation.Env))
					for name := range publication.Invocation.Env {
						envNames = append(envNames, name)
					}
					slices.Sort(envNames)
					for _, name := range envNames {
						steps = append(steps, fmt.Sprintf("          %s: %q\n", name, publication.Invocation.Env[name]))
					}
				}
				if inputs, ok := step["with"].(map[string]any); ok {
					if key, exists := inputs["key"]; exists {
						steps = append(steps, fmt.Sprintf("          GH_AW_PENDING_STORAGE_KEY: %q\n", fmt.Sprint(key)))
					}
					if drive, exists := inputs["drive-name"]; exists {
						steps = append(steps, fmt.Sprintf("          GH_AW_PENDING_DRIVE_NAME: %q\n", fmt.Sprint(drive)))
					}
					for _, input := range []string{"file", "sarif_file", "path"} {
						if selected, exists := inputs[input]; exists {
							steps = append(steps, fmt.Sprintf("          GH_AW_PENDING_SELECTED_PATH: %q\n", fmt.Sprint(selected)))
							break
						}
					}
				}
				steps = append(steps, "        with:\n", "          script: |\n", "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n", "            setupGlobals(core, github, context, exec, io, getOctokit);\n", "            require('"+SetupActionDestination+"/pending_writes_runtime.cjs').checkPublicationFromEnv();\n")
				condition := fmt.Sprintf("steps.%s.outcome == 'success' && steps.%s.outputs.pending_writes_verified == 'true'", id, id)
				if original, exists := step["if"]; exists {
					condition += " && (" + c.extractExpressionFromIfString(fmt.Sprint(original)) + ")"
				}
				step["if"] = condition
				if name == publication.ID {
					env := pendingStepEnv(step)
					env["GH_AW_AGENT_OUTPUT"] = publication.Path
				}
				if job.Outputs == nil {
					job.Outputs = map[string]string{}
				}
			}
			rendered, err := ConvertStepToYAML(step)
			if err != nil {
				return fmt.Errorf("rendering bound publication step for %s: %w", name, err)
			}
			steps = append(steps, rendered)
		}
		if !checked {
			return fmt.Errorf("publication job %s has no handler-derived pending-write entry", name)
		}
		idsJSON, err := json.Marshal(checkIDs)
		if err != nil {
			return fmt.Errorf("encoding publication verification IDs: %w", err)
		}
		steps = append(steps, "      - name: Record pending-write input verification\n", "        id: pending_write_integrity\n", "        if: always()\n", fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)), "        env:\n", fmt.Sprintf("          GH_AW_PENDING_JOB: %q\n", name), fmt.Sprintf("          GH_AW_PENDING_CHECK_NAMES: %q\n", idsJSON))
		for _, id := range checkIDs {
			steps = append(steps, fmt.Sprintf("          GH_AW_PENDING_CHECK_%s: ${{ steps.%s.outputs.pending_writes_item_sha256 }}\n", id, id))
		}
		steps = append(steps, "        with:\n", "          script: |\n", "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n", "            setupGlobals(core, github, context, exec, io, getOctokit);\n", "            require('"+SetupActionDestination+"/pending_writes_runtime.cjs').collectPublicationChecks();\n")
		job.Outputs["pending_writes_verified"] = "${{ steps.pending_write_integrity.outputs.pending_writes_verified }}"
		job.Outputs["pending_writes_item_sha256"] = "${{ steps.pending_write_integrity.outputs.pending_writes_item_sha256 }}"
		job.Steps = steps
	}
	return c.jobManager.ValidateDependencies()
}
