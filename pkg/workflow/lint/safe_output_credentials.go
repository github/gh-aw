package lint

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// SafeOutputCredentialsRule flags authored safe-outputs.steps that would
// accidentally use or alter the write credentials of the safe_outputs job.
// These steps are trusted, but they run after the compiler mints write-scoped
// GitHub App tokens and configures persisted git credentials for the handlers.
func SafeOutputCredentialsRule() Rule {
	return Rule{
		ID: "safe-output-credentials", Description: "Keep authored safe-output steps from using or altering the job's write credentials",
		Severity: Error, Targets: AWAST, Check: checkSafeOutputCredentials,
	}
}

const safeOutputInputPath = `["']?(?:\$\{?GH_AW_AGENT_OUTPUT\}?|\$\{?GH_AW_SAFE_OUTPUTS\}?|/tmp/gh-aw/|\$\{?RUNNER_TEMP\}?/gh-aw/|\$\{\{\s*runner\.temp\s*\}\}/gh-aw/)`

var (
	safeOutputMintedTokenRE = regexp.MustCompile(`(?i)\bsteps\s*(?:\.\s*([A-Za-z0-9_-]+-app-token)\b|\[\s*['"]([A-Za-z0-9_-]+-app-token)['"]\s*\])`)
	safeOutputEnvFileRE     = regexp.MustCompile(`\$\{?(?:GITHUB_ENV|GITHUB_PATH)\b`)
	safeOutputCoreExportRE  = regexp.MustCompile(`\bcore\.(?:exportVariable|addPath)\s*\(`)
	safeOutputTamperRE      = regexp.MustCompile(
		`(?:>>?|\btee(?:\s+-a)?|\bsed\s+-[a-zA-Z]*i[a-zA-Z]*(?:\s+(?:'[^']*'|"[^"]*"|\S+))?|\brm(?:\s+-[a-zA-Z]+)*|\btruncate(?:\s+-\S+(?:\s+\S+)?)*)\s*` + safeOutputInputPath,
	)
	safeOutputMoveCopyRE    = regexp.MustCompile(`^\s*(?:mv|cp)\s`)
	safeOutputInputTargetRE = regexp.MustCompile(`^` + safeOutputInputPath)
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
	for _, raw := range steps {
		if step, ok := raw.(map[string]any); ok {
			if id, ok := step["id"].(string); ok {
				declared[strings.ToLower(id)] = struct{}{}
			}
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
		diagnostics = append(diagnostics, safeOutputStepCredentialDiagnostics(step, path, declared, severity)...)
	}
	return diagnostics, errors.Join(failures...)
}

func safeOutputStepCredentialDiagnostics(step map[string]any, path string, declared map[string]struct{}, severity Severity) []Diagnostic {
	var diagnostics []Diagnostic
	report := func(path, message string) {
		diagnostics = append(diagnostics, Diagnostic{Severity: severity, Path: path, Message: message})
	}
	keys := make([]string, 0, len(step))
	for key := range step {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if referencesMintedToken(step[key], declared) {
			report(path+"."+key, "Safe-output step uses a compiler-minted GitHub App token scoped for the safe-output handlers; mint a dedicated least-privilege token in an authored step instead")
			break
		}
	}
	script, scriptPath := safeOutputStepScript(step, path)
	if script != "" && (safeOutputEnvFileRE.MatchString(script) || safeOutputCoreExportRE.MatchString(script)) {
		report(scriptPath, "Safe-output step writes GITHUB_ENV or GITHUB_PATH, changing the environment of later handler steps that hold write tokens; use step-level env or GITHUB_OUTPUT instead")
	}
	if checkoutPath, persists := safeOutputCheckoutPersists(step, path); persists {
		report(checkoutPath, "Safe-output step checks out with persisted credentials, replacing the workspace and git credentials prepared for the handlers; set persist-credentials: false and a dedicated path")
	}
	if run, ok := step["run"].(string); ok && tampersWithSafeOutputInputs(run) {
		report(path+".run", "Safe-output step modifies agent output or patch files under /tmp/gh-aw that handlers apply after threat detection; write scratch files to RUNNER_TEMP or the workspace instead")
	}
	return diagnostics
}

func referencesMintedToken(value any, declared map[string]struct{}) bool {
	switch v := value.(type) {
	case string:
		for _, match := range safeOutputMintedTokenRE.FindAllStringSubmatch(v, -1) {
			id := strings.ToLower(strings.Join(match[1:], ""))
			if _, ok := declared[id]; !ok {
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

func safeOutputCheckoutPersists(step map[string]any, path string) (string, bool) {
	uses, ok := step["uses"].(string)
	if !ok {
		return "", false
	}
	uses = strings.TrimSpace(uses)
	if uses != "actions/checkout" && !strings.HasPrefix(uses, "actions/checkout@") {
		return "", false
	}
	with, ok := step["with"].(map[string]any)
	if !ok {
		return path + ".uses", true
	}
	switch value := with["persist-credentials"].(type) {
	case bool:
		return path + ".with.persist-credentials", value
	case string:
		return path + ".with.persist-credentials", value != "false"
	case nil:
		return path + ".uses", true
	}
	return path + ".with.persist-credentials", true
}

func tampersWithSafeOutputInputs(script string) bool {
	for line := range strings.Lines(script) {
		line = strings.TrimRight(line, "\r\n")
		for _, command := range strings.FieldsFunc(line, func(r rune) bool { return r == ';' || r == '|' || r == '&' }) {
			if safeOutputTamperRE.MatchString(command) {
				return true
			}
			if safeOutputMoveCopyRE.MatchString(command) {
				fields := strings.Fields(command)
				if len(fields) > 2 && slices.ContainsFunc(fields[len(fields)-1:], safeOutputInputTargetRE.MatchString) {
					return true
				}
			}
		}
	}
	return false
}
