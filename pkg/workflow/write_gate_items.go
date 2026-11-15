package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

var writeGateOutputReference = regexp.MustCompile(`steps\.([A-Za-z0-9_-]+)\.outputs\.[A-Za-z0-9_-]+`)

func (c *Compiler) addWriteGateItemDecisionSteps(data *WorkflowData) error {
	if !hasWriteGateSteps(data) {
		if data.SafeOutputs != nil && data.SafeOutputs.Gate != nil {
			return NewValidationError("safe-outputs.gate", "", "requires a compiler-managed write gate", "Add jobs.safe_outputs.gate-steps, for example: jobs:\n  safe_outputs:\n    gate-steps:\n      - run: ./scripts/write-gate.sh")
		}
		return nil
	}
	job, exists := c.jobManager.GetJob("safe_outputs")
	if !exists {
		return fmt.Errorf("per-item write decisions require the safe_outputs job")
	}
	gateExpression := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(job.Outputs["write_gate"], "${{"), "}}"))
	reference := writeGateOutputReference.FindStringSubmatch(gateExpression)
	if len(reference) != 2 {
		return fmt.Errorf("write gate outputs require a compiler-owned verdict step before per-item normalization")
	}
	text := strings.Join(job.Steps, "")
	marker := "        id: " + reference[1] + "\n"
	verdict := strings.Index(text, marker)
	if verdict < 0 {
		return fmt.Errorf("write gate verdict step %q is missing from safe_outputs", reference[1])
	}
	boundary := len(text)
	if next := strings.Index(text[verdict+len(marker):], "\n      - "); next >= 0 {
		boundary = verdict + len(marker) + next + 1
	}
	const normalizedReference = "steps.normalize_write_decisions.outputs.decision"
	// Retain the original validated gate as normalization input; all subsequent
	// compiler-owned guards consume the narrower normalized verdict.
	tail := strings.ReplaceAll(text[boundary:], reference[0], normalizedReference)
	rawSuccess := "steps." + reference[1] + ".outcome == 'success'"
	tail = strings.ReplaceAll(tail, rawSuccess, rawSuccess+" && steps.normalize_write_decisions.outcome == 'success'")
	normalization := `      - name: Normalize write item decisions
        id: normalize_write_decisions
        if: always()
        uses: ` + c.getActionPin("actions/github-script") + `
        env:
          GH_AW_VALIDATED_WRITE_GATE: ${{ ` + gateExpression + ` }}
          GH_AW_VALIDATED_WRITE_GATE_REASON: ` + job.Outputs["write_gate_reason"] + `
          GH_AW_VALIDATED_WRITE_DECISIONS_SHA256: ${{ steps.` + reference[1] + `.outputs.decisions_sha256 }}
        with:
          script: |
            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io, getOctokit);
            const { publishWriteDecisionInventory } = require('${{ runner.temp }}/gh-aw/actions/write_gate_runtime.cjs');
            publishWriteDecisionInventory();
`
	reviewSteps, err := c.writeGateReviewCredentialSteps(data)
	if err != nil {
		return err
	}
	upload := `      - name: Upload write item decisions
        id: upload_write_decisions
        if: always() && steps.normalize_write_decisions.outcome == 'success'
        uses: ` + c.getActionPin("actions/upload-artifact") + `
        with:
          name: ` + artifactPrefixExprForDownstreamJob(data) + `write-decisions
          path: /tmp/gh-aw/write-decisions.normalized.json
          if-no-files-found: error
`
	job.Steps = []string{text[:boundary] + normalization + reviewSteps + tail + upload}
	job.Outputs["write_gate"] = "${{ steps.normalize_write_decisions.outcome == 'success' && " + normalizedReference + " || 'deny' }}"
	job.Outputs["write_gate_reason"] = "${{ steps.normalize_write_decisions.outputs.reason || 'gate_unavailable' }}"
	job.Outputs["write_gate_decisions_sha256"] = "${{ steps.normalize_write_decisions.outputs.sha256 }}"
	job.Outputs["write_gate_items_sha256"] = "${{ steps.process_safe_outputs.outputs.write_gate_items_sha256 || steps.normalize_write_decisions.outputs.items_sha256 }}"
	return nil
}

func reviewGitHubApp(config *SafeOutputsConfig) (*GitHubAppConfig, error) {
	if config.GitHubApp != nil {
		return config.GitHubApp, nil
	}
	if config.GitHubToken != "" {
		return nil, nil
	}
	var app *GitHubAppConfig
	for _, handler := range safeOutputHandlers {
		candidate := getHandlerGitHubApp(config, handler.StructField)
		if candidate == nil {
			continue
		}
		if app != nil && (app.AppID != candidate.AppID || app.PrivateKey != candidate.PrivateKey || app.Owner != candidate.Owner) {
			return nil, NewValidationError("safe-outputs.gate.review-repo", config.Gate.ReviewRepo, "has multiple possible per-handler GitHub Apps", "Select review credentials explicitly, for example: safe-outputs:\n  github-token: ${{ secrets.REVIEW_TOKEN }}")
		}
		app = candidate
	}
	return app, nil
}

func (c *Compiler) writeGateReviewCredentialSteps(data *WorkflowData) (string, error) {
	if data.SafeOutputs.Gate == nil || data.SafeOutputs.Gate.ReviewRepo == "" || templatableBoolIsTrue(data.SafeOutputs.Staged) {
		return "", nil
	}
	app, err := reviewGitHubApp(data.SafeOutputs)
	if err != nil || app == nil {
		return "", err
	}
	scoped := *app
	scoped.Repositories = nil
	scoped.Permissions = nil
	permissions := NewPermissions()
	permissions.Set(PermissionIssues, PermissionWrite)
	condition := "${{ success() && steps.normalize_write_decisions.outputs.decision != 'deny' && steps.normalize_write_decisions.outputs.review_repo_usable == 'true' }}"
	steps := c.buildGitHubAppTokenMintStepWithMeta(&scoped, permissions, "${{ env.GH_AW_WRITE_GATE_REVIEW_REPO }}", data.SafeOutputs.Gate.ReviewRepo, "Generate gate review GitHub App token", "write-gate-review-token", condition)
	steps = append(steps, `      - name: Select gate review credential
        if: `+condition+`
        uses: `+c.getActionPin("actions/github-script")+`
        env:
          GH_AW_WRITE_GATE_REVIEW_TOKEN: ${{ steps.write-gate-review-token.outputs.token }}
        with:
          script: |
            core.exportVariable('GH_AW_WRITE_GATE_REVIEW_TOKEN', process.env.GH_AW_WRITE_GATE_REVIEW_TOKEN || '');
`)
	job, exists := c.jobManager.GetJob("safe_outputs")
	if exists {
		if job.Env == nil {
			job.Env = make(map[string]string)
		}
		job.Env["GH_AW_WRITE_GATE_REVIEW_USE_APP"] = `"true"`
	}
	return strings.Join(steps, ""), nil
}
