package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func (c *Compiler) buildWriteGateConclusionInputSteps(data *WorkflowData) ([]string, error) {
	if !hasWriteGateSteps(data) {
		return nil, nil
	}
	var steps []string
	for _, artifact := range []string{"pending-writes", "write-decisions"} {
		steps = append(steps, buildArtifactDownloadSteps(ArtifactDownloadConfig{
			ArtifactName: artifactPrefixExprForDownstreamJob(data) + artifact,
			DownloadPath: "/tmp/gh-aw/" + artifact + "/",
			StepName:     "Download write gate " + artifact,
			StepID:       "download_write_gate_" + strings.ReplaceAll(artifact, "-", "_"),
			IfCondition:  "always()",
		}, c.getActionPin)...)
	}
	check, err := ConvertStepToYAML(map[string]any{
		"name": "Check write gate enforcement",
		"id":   "check_write_gate",
		"if":   "always()",
		"uses": c.getActionPin("actions/github-script"),
		"with": map[string]any{"script": `const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
setupGlobals(core, github, context, exec, io, getOctokit);
const { checkConclusion } = require('${{ runner.temp }}/gh-aw/actions/write_gate_observability.cjs');
checkConclusion({ core });`},
	})
	if err != nil {
		return nil, fmt.Errorf("cannot render compiler-owned write gate conclusion check: %w", err)
	}
	return append(steps, check), nil
}

// Bind only compiler-owned policy/input receipts after the complete job registry
// is available. Arbitrary job outputs and user content never enter conclusion.
func (c *Compiler) finalizeWriteGateConclusionEnv(data *WorkflowData) error {
	if !hasWriteGateSteps(data) {
		return nil
	}
	conclusion, exists := c.jobManager.GetJob("conclusion")
	if !exists {
		return fmt.Errorf("write gate observations require the compiler-owned conclusion job")
	}
	if conclusion.Env == nil {
		conclusion.Env = make(map[string]string)
	}
	for name, output := range map[string]string{
		"GH_AW_WRITE_GATE":                  "write_gate",
		"GH_AW_WRITE_GATE_REASON":           "write_gate_reason",
		"GH_AW_PENDING_WRITES_SHA256":       "pending_writes_sha256",
		"GH_AW_PENDING_WRITES_COMPLETE":     "pending_writes_complete",
		"GH_AW_WRITE_GATE_DECISIONS_SHA256": "write_gate_decisions_sha256",
	} {
		conclusion.Env[name] = "${{ needs.safe_outputs.outputs." + output + " }}"
	}
	conclusion.Env["GH_AW_WRITE_GATE_JOB_RESULT"] = "${{ needs.safe_outputs.result }}"
	conclusion.Env["GH_AW_PENDING_WRITES"] = strconv.Quote("/tmp/gh-aw/pending-writes/pending-writes.json")
	conclusion.Env["GH_AW_WRITE_GATE_DECISIONS"] = strconv.Quote("/tmp/gh-aw/write-decisions/write-decisions.normalized.json")

	var names []string
	for name := range publicationJobNames(data.SafeOutputs) {
		if name == "safe_outputs" {
			continue
		}
		if _, exists := c.jobManager.GetJob(name); exists {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if names == nil {
		names = []string{}
	}
	encodedNames, err := json.Marshal(names)
	if err != nil {
		return fmt.Errorf("cannot encode write gate publication registry: %w", err)
	}
	conclusion.Env["GH_AW_WRITE_GATE_WRITE_JOB_NAMES"] = strconv.Quote(string(encodedNames))

	receipts := make(map[string]map[string]string, len(names))
	var expressions []string
	for _, name := range names {
		receipts[name] = make(map[string]string)
		receiptJob := name
		if job, exists := c.jobManager.GetJob(name); exists && job.Uses != "" {
			verification := "verify-pending-" + name
			if _, exists := c.jobManager.GetJob(verification); exists {
				receiptJob = verification
				if !slices.Contains(conclusion.Needs, verification) {
					conclusion.Needs = append(conclusion.Needs, verification)
				}
			}
		}
		for _, output := range []string{"pending_writes_verified", "pending_writes_item_sha256"} {
			expression := "${{ toJSON(needs." + receiptJob + ".outputs." + output + ") }}"
			receipts[name][output] = expression
			expressions = append(expressions, expression)
		}
	}
	encodedReceipts, err := json.Marshal(receipts)
	if err != nil {
		return fmt.Errorf("cannot encode write gate publication receipts: %w", err)
	}
	metadata := string(encodedReceipts)
	for _, expression := range expressions {
		metadata = strings.ReplaceAll(metadata, strconv.Quote(expression), expression)
	}
	conclusion.Env["GH_AW_WRITE_GATE_PUBLICATION_RESULTS"] = strconv.Quote(metadata)
	return nil
}
