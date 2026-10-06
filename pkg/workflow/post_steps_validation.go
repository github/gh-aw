package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	postStepIssueCreateRE = regexp.MustCompile(`(?i)(?:^|[\s|;&])gh\s+issue\s+create\b`)
	postStepIssueAPIRE    = regexp.MustCompile(`(?i)(?:^|[/\s"'=])(?:https://api\.github\.com/)?repos/(?:[^/\s"'?]+/[^/\s"'?]+|\$\{\{\s*github\.repository\s*\}\}|\$\{?GITHUB_REPOSITORY\}?)/issues(?:[?"'\s]|$)`)
	postStepAPIPostRE     = regexp.MustCompile(`(?i)(?:^|\s)(?:-X\s+POST|--method(?:=|\s+)POST|-f(?:\s|=)|-F(?:\s|=)|--(?:raw-)?field(?:\s|=))`)
	postStepAPIGetRE      = regexp.MustCompile(`(?i)(?:^|\s)(?:-X\s+GET\b|--method(?:=|\s+)GET\b)`)
	postStepCurlPostRE    = regexp.MustCompile(`(?i)\bcurl\b[^\n]*\s(?:-X\s+POST\b|--request(?:=|\s+)POST\b|(?:-d|--data(?:-raw|-binary|-urlencode)?)(?:\s|=))`)
	postStepOctokitRE     = regexp.MustCompile(`\b(?:github|octokit)(?:\.rest)?\.issues\.create\s*\(`)
	postStepOutputRE      = regexp.MustCompile(`(?i)(?:\bagent_output\.json\b|(?:safeoutputs|safe-outputs)/output\.json\b|\boutputs\.jsonl\b|\bsafeoutputs\.jsonl\b|\bGH_AW_AGENT_OUTPUT\b)`)
)

// validatePostStepsSafeOutputs checks only user-provided run scripts, not compiler-generated steps.
func validatePostStepsSafeOutputs(steps []any) error {
	for i, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		scripts := []string{}
		if run, ok := step["run"].(string); ok {
			scripts = append(scripts, run)
		}
		if with, ok := step["with"].(map[string]any); ok {
			if script, ok := with["script"].(string); ok {
				scripts = append(scripts, script)
			}
		}
		location := fmt.Sprintf("post-steps step %d", i+1)
		if name, ok := step["name"].(string); ok && name != "" {
			location += fmt.Sprintf(" (%q)", name)
		}

		for _, script := range scripts {
			createsIssue := postStepIssueCreateRE.MatchString(script) || postStepOctokitRE.MatchString(script)
			for _, command := range ghAPICmdRE.FindAllStringSubmatch(script, -1) {
				for _, args := range command {
					if postStepIssueAPIRE.MatchString(parseGHAPIEndpoint(args)) &&
						postStepAPIPostRE.MatchString(args) && !postStepAPIGetRE.MatchString(args) {
						createsIssue = true
						break
					}
				}
			}
			for line := range strings.SplitSeq(script, "\n") {
				if postStepCurlPostRE.MatchString(line) && postStepIssueAPIRE.MatchString(line) {
					createsIssue = true
				}
			}
			if createsIssue {
				return fmt.Errorf("%s attempts to create an issue directly; use safe-outputs.create-issue instead. See: https://github.github.com/gh-aw/reference/safe-outputs/", location)
			}
			if postStepOutputRE.MatchString(script) {
				return fmt.Errorf("%s accesses the safe outputs output file directly; use safe-outputs instead. See: https://github.github.com/gh-aw/reference/safe-outputs/", location)
			}
		}
	}
	return nil
}
