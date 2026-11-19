package lint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
	"mvdan.cc/sh/v3/syntax"
)

// SafeOutputCredentialsRule flags authored safe-outputs.steps that would
// accidentally use or alter the write credentials of the safe_outputs job.
// These steps are trusted, but they run after the compiler mints write-scoped
// GitHub App tokens and configures persisted git credentials for the handlers.
// Each finding carries its own compiler threat ID (CTR-035 through CTR-038).
func SafeOutputCredentialsRule() Rule {
	return Rule{
		ID: "safe-output-credentials", Description: "Keep authored safe-output steps from using or altering the job's write credentials",
		FindingThreatRuleIDs: []string{"CTR-035", "CTR-036", "CTR-037", "CTR-038"},
		Severity:             Error, Targets: AWAST, Check: checkSafeOutputCredentials,
	}
}

var (
	safeOutputStepReferenceRE = regexp.MustCompile(`(?i)\bsteps\s*(?:\.\s*([A-Za-z0-9_-]+)|\[\s*['"]([^'"]+)['"]\s*\])`)
	safeOutputStatusCheckRE   = regexp.MustCompile(`(?i)^\s*(?:\.\s*(?:outcome|conclusion)\b|\[\s*['"](?:outcome|conclusion)['"]\s*\])`)
	safeOutputTokenStepIDRE   = regexp.MustCompile(`(?i)(?:^|-)app-token(?:-|$)`)
	safeOutputInputTargetRE   = regexp.MustCompile(`^(?:\$GH_AW_(?:AGENT_OUTPUT|SAFE_OUTPUTS)(?:/.*)?|\$\{GH_AW_(?:AGENT_OUTPUT|SAFE_OUTPUTS)\}(?:/.*)?|/tmp/gh-aw(?:/.*)?|\$RUNNER_TEMP/gh-aw(?:/.*)?|\$\{RUNNER_TEMP\}/gh-aw(?:/.*)?|\$\{\{\s*runner\.temp\s*\}\}/gh-aw(?:/.*)?)$`)
	safeOutputEnvRedirectRE   = regexp.MustCompile(`(?i)(?:>>?|>\||&>>?|&>)\s*["']?` + `(?:\$env:\s*GITHUB_(?:ENV|PATH)\b|\$\{env:GITHUB_(?:ENV|PATH)\}|\$\{?GITHUB_(?:ENV|PATH)\}?|\$\{\{\s*github\.(?:env|path)\s*\}\})`)
	safeOutputEnvCommandRE    = regexp.MustCompile(`(?i)\b(?:set-content|add-content|out-file|tee)(?:\s+-a)?\b[^;\r\n]*` + `(?:\$env:\s*GITHUB_(?:ENV|PATH)\b|\$\{env:GITHUB_(?:ENV|PATH)\}|\$\{?GITHUB_(?:ENV|PATH)\}?|\$\{\{\s*github\.(?:env|path)\s*\}\})`)
	safeOutputCoreExportRE    = regexp.MustCompile(`\bcore\.(?:exportVariable|addPath)\s*\(`)
)

func checkSafeOutputCredentials(ctx context.Context, input Input) ([]Diagnostic, error) {
	outputs, err := authoredPolicyMapping(ctx, input, "safe-outputs")
	if err != nil {
		return nil, err
	}
	raw, exists := outputs["steps"]
	if !exists {
		return nil, nil
	}
	steps, ok := raw.([]any)
	if !ok {
		return nil, errors.New("safe-output-credentials requires a step list at safe-outputs.steps")
	}
	severity, err := sourcePolicySeverity(input, "safe-output-credentials")
	if err != nil {
		return nil, err
	}
	declared := make(map[string]struct{})
	addDeclaredStepIDs(steps, declared)
	if jobs, ok := input.Document.Frontmatter["jobs"].(map[string]any); ok {
		for _, key := range []string{"safe-outputs", "safe_outputs"} {
			addDeclaredStepIDs(jobs[key], declared)
		}
	}
	for _, key := range []string{"pre-steps", "setup-steps"} {
		addDeclaredStepIDs(outputs[key], declared)
	}
	if jobs, ok := outputs["jobs"].(map[string]any); ok {
		for _, job := range jobs {
			addDeclaredStepIDs(job, declared)
		}
	}
	var diagnostics []Diagnostic
	var failures []error
	for index, raw := range steps {
		if err := ctx.Err(); err != nil {
			return diagnostics, errors.Join(append(failures, err)...)
		}
		path := fmt.Sprintf("safe-outputs.steps[%d]", index)
		step, err := policyMapping(raw, path)
		if err != nil || step == nil {
			failures = append(failures, fmt.Errorf("safe-output-credentials requires a step mapping at %s", path))
			continue
		}
		found, err := safeOutputStepCredentialDiagnostics(ctx, input, step, path, declared, severity)
		diagnostics = append(diagnostics, found...)
		failures = append(failures, err)
	}
	return diagnostics, errors.Join(failures...)
}

func addDeclaredStepIDs(value any, declared map[string]struct{}) {
	switch value := value.(type) {
	case map[string]any:
		if id, ok := value["id"].(string); ok {
			declared[strings.ToLower(id)] = struct{}{}
		}
		for _, item := range value {
			addDeclaredStepIDs(item, declared)
		}
	case []any:
		for _, item := range value {
			addDeclaredStepIDs(item, declared)
		}
	}
}

func safeOutputStepCredentialDiagnostics(ctx context.Context, input Input, step map[string]any, path string, declared map[string]struct{}, severity Severity) ([]Diagnostic, error) {
	var diagnostics []Diagnostic
	var failures []error
	report := func(threatRuleID, path, message string) {
		diagnostics = append(diagnostics, Diagnostic{ThreatRuleID: threatRuleID, Severity: severity, Path: path, Message: message})
	}
	keys := make([]string, 0, len(step))
	for key := range step {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if referencesMintedToken(step[key], declared) {
			report("CTR-035", path+"."+key, "Safe-output step uses a compiler-minted GitHub App token scoped for the safe-output handlers; mint a dedicated least-privilege token in an authored step instead")
			break
		}
	}
	script, scriptPath := safeOutputStepScript(step, path)
	if script != "" && (safeOutputWritesEnvironmentFile(script) || safeOutputCoreExportRE.MatchString(script)) {
		report("CTR-036", scriptPath, "Safe-output step writes GITHUB_ENV or GITHUB_PATH, changing the environment of later handler steps that hold write tokens; use step-level env or GITHUB_OUTPUT instead")
	}
	checkoutPath, persists, err := safeOutputCheckoutPersists(ctx, step, path)
	failures = append(failures, err)
	if persists {
		report("CTR-037", checkoutPath, "Safe-output step checks out with persisted credentials, replacing the workspace and git credentials prepared for the handlers; set persist-credentials: false and a dedicated path")
	}
	if run, ok := step["run"].(string); ok && tampersWithSafeOutputInputs(ctx, input, step, path, run) {
		report("CTR-038", path+".run", "Safe-output step modifies agent output or patch files under /tmp/gh-aw that handlers apply after threat detection; write scratch files to RUNNER_TEMP or the workspace instead")
	}
	return diagnostics, errors.Join(failures...)
}

func referencesMintedToken(value any, declared map[string]struct{}) bool {
	switch v := value.(type) {
	case string:
		for _, match := range safeOutputStepReferenceRE.FindAllStringSubmatchIndex(v, -1) {
			var id string
			if match[2] >= 0 {
				id = v[match[2]:match[3]]
			} else {
				id = v[match[4]:match[5]]
			}
			if !safeOutputTokenStepIDRE.MatchString(id) || safeOutputStatusCheckRE.MatchString(v[match[1]:]) {
				continue
			}
			if _, ok := declared[strings.ToLower(id)]; !ok {
				return true
			}
		}
	case map[string]any:
		for _, item := range v {
			if referencesMintedToken(item, declared) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(v, func(item any) bool { return referencesMintedToken(item, declared) })
	}
	return false
}

func safeOutputStepScript(step map[string]any, path string) (string, string) {
	if run, ok := step["run"].(string); ok {
		return run, path + ".run"
	}
	if with, ok := step["with"].(map[string]any); ok {
		if script, ok := with["script"].(string); ok {
			return script, path + ".with.script"
		}
	}
	return "", ""
}

func safeOutputCheckoutPersists(ctx context.Context, step map[string]any, path string) (string, bool, error) {
	uses, ok := step["uses"].(string)
	if !ok || actionName(uses) != "actions/checkout" {
		return "", false, nil
	}
	with, ok := step["with"].(map[string]any)
	if !ok {
		return path + ".uses", true, nil
	}
	value, present := with["persist-credentials"]
	if !present {
		return path + ".uses", true, nil
	}
	enabled, known, err := actionInputBoolean(ctx, value, path+".with.persist-credentials")
	return path + ".with.persist-credentials", !known || enabled, err
}

func safeOutputWritesEnvironmentFile(script string) bool {
	return safeOutputEnvRedirectRE.MatchString(script) || safeOutputEnvCommandRE.MatchString(script)
}

func tampersWithSafeOutputInputs(ctx context.Context, input Input, step map[string]any, path, script string) bool {
	if safeOutputRedirectsToInput(ctx, script, path) {
		return true
	}
	block := aw.ExecutionBlock{Path: "safe-outputs"}
	shellStep := aw.ExecutionStep{Fields: step, Path: path}
	found := false
	_, _ = policyShellCalls(ctx, input, block, shellStep, func(call *syntax.CallExpr, source string) ([]Diagnostic, error) {
		if safeOutputCallWritesInput(call, source) {
			found = true
		}
		return nil, nil
	})
	return found
}

func safeOutputRedirectsToInput(ctx context.Context, script, path string) bool {
	rendered := []byte(script)
	if err := taint.VisitInterpolations(ctx, script, path, func(start, end, _ int, _ actionlint.ExprNode) error {
		for index := start; index < end; index++ {
			rendered[index] = 'x'
		}
		return ctx.Err()
	}); err != nil {
		return false
	}
	tree, err := syntax.NewParser().Parse(bytes.NewReader(rendered), "")
	if err != nil {
		return false
	}
	found := false
	syntax.Walk(tree, func(node syntax.Node) bool {
		redirect, ok := node.(*syntax.Redirect)
		if !ok || (redirect.Op != syntax.RdrOut && redirect.Op != syntax.AppOut &&
			redirect.Op != syntax.RdrClob && redirect.Op != syntax.AppClob &&
			redirect.Op != syntax.RdrAll && redirect.Op != syntax.RdrAllClob) {
			return true
		}
		start, end := redirect.Word.Pos().Offset(), redirect.Word.End().Offset()
		found = safeOutputInputTarget(script[start:end])
		return !found
	})
	return found
}

func safeOutputCallWritesInput(call *syntax.CallExpr, script string) bool {
	if len(call.Args) < 2 {
		return false
	}
	command := strings.ToLower(policyShellArgument(call, 0))
	args := call.Args[1:]
	operands := make([]string, 0, len(args))
	var targetDirectory string
	endOptions := false
	for index := 0; index < len(args); index++ {
		value := policyShellLiteral(args[index])
		if value == "--" {
			endOptions = true
			continue
		}
		if !endOptions && strings.HasPrefix(value, "-") {
			if (command == "cp" || command == "mv") && (value == "-t" || value == "--target-directory") && index+1 < len(args) {
				index++
				targetDirectory = shellWordSource(args[index], script)
				continue
			}
			if (command == "cp" || command == "mv") && strings.HasPrefix(value, "--target-directory=") {
				targetDirectory = strings.TrimPrefix(shellWordSource(args[index], script), "--target-directory=")
				continue
			}
			continue
		}
		operands = append(operands, shellWordSource(args[index], script))
	}

	switch command {
	case "rm", "truncate", "tee":
		return slices.ContainsFunc(operands, safeOutputInputTarget)
	case "sed":
		inPlace := slices.ContainsFunc(args, func(word *syntax.Word) bool {
			value := policyShellLiteral(word)
			return value == "-i" || value == "--in-place" || strings.HasPrefix(value, "-i.")
		})
		if !inPlace {
			return false
		}
		return len(operands) > 1 && slices.ContainsFunc(operands[1:], safeOutputInputTarget)
	case "cp", "mv":
		if targetDirectory != "" {
			return safeOutputInputTarget(targetDirectory)
		}
		return len(operands) > 1 && safeOutputInputTarget(operands[len(operands)-1])
	default:
		return false
	}
}

func shellWordSource(word *syntax.Word, script string) string {
	start, end := word.Pos().Offset(), word.End().Offset()
	return strings.Trim(script[start:end], `"'`)
}

func safeOutputInputTarget(target string) bool {
	return safeOutputInputTargetRE.MatchString(strings.Trim(strings.TrimSpace(target), `"'`))
}
