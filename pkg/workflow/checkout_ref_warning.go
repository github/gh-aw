package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/github/gh-aw/pkg/workflow/checkoutpolicy"
	"github.com/goccy/go-yaml"
)

var checkoutRefWarningLog = logger.New("workflow:checkout_ref_warning")

// warnCheckoutRefWithSteps flags potentially untrusted code running before the sandbox.
func (c *Compiler) warnCheckoutRefWithSteps(data *WorkflowData, markdownPath string) {
	if !hasCheckoutRefWarningHostSteps(data) {
		checkoutRefWarningLog.Print("No host steps or pre-agent-steps; skipping checkout ref warning check")
		return
	}

	checkoutRefWarningLog.Printf("Checking %d checkout config(s) for untrusted refs before host steps", len(data.CheckoutConfigs))
	checkoutMgr := NewCheckoutManager(data.CheckoutConfigs)
	defaultCheckout := checkoutMgr.GetDefaultCheckoutOverride()
	for _, entry := range checkoutMgr.ordered {
		if entry.isDefault() && (entry != defaultCheckout || !c.shouldAddCheckoutStep(data)) {
			continue
		}
		if !checkoutRefMayBeUntrusted(entry.key.repository, entry.ref) {
			continue
		}
		msg := fmt.Sprintf(
			"checkout step %q uses checkout.ref %q with an untrusted or unparseable expression, and host setup steps run after it. "+
				"Imported or workflow-owned steps and pre-agent-steps execute outside the sandbox and may run checked-out code. "+
				"Keep the agent workspace on a trusted ref and inspect fetched untrusted refs only as data; do not check them out or run their scripts on the host. "+
				"For PR-head work, use compiler-managed automatic PR checkout so trusted agent configuration is restored.",
			checkoutStepName(entry.key),
			entry.ref,
		)
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", msg))
		c.IncrementWarningCount()
		return
	}
}

func hasCheckoutRefWarningHostSteps(data *WorkflowData) bool {
	return hasCheckoutRefWarningSteps(data.CustomSteps, "steps") ||
		hasCheckoutRefWarningSteps(data.PreAgentSteps, "pre-agent-steps")
}

func hasCheckoutRefWarningSteps(sectionYAML, sectionName string) bool {
	if strings.TrimSpace(sectionYAML) == "" {
		return false
	}
	var section map[string]any
	if err := yaml.Unmarshal([]byte(sectionYAML), &section); err != nil {
		checkoutRefWarningLog.Printf("Failed to parse %s YAML, treating as present: %v", sectionName, err)
		return true
	}
	value, ok := section[sectionName]
	if !ok || value == nil {
		return false
	}
	steps, ok := value.([]any)
	return !ok || len(steps) > 0
}

func checkoutRefMayBeUntrusted(repository, ref string) bool {
	if checkoutpolicy.Base(repository, ref) {
		return false
	}
	if (strings.TrimSpace(repository) == "" || checkoutpolicy.MatchesExpression(repository, "github.repository")) &&
		checkoutpolicy.MatchesExpression(ref, "github.event.repository.default_branch") {
		return false
	}

	expressions, err := actionslanguageservice.ExtractExpressions(ref)
	if err != nil {
		checkoutRefWarningLog.Printf("Cannot extract expressions from checkout ref %q, treating as untrusted: %v", ref, err)
		return true
	}
	for _, span := range expressions {
		expression, err := actionslanguageservice.ParseSyntax(span.Content)
		if err != nil {
			return true
		}
		for _, path := range actionslanguageservice.References(expression) {
			if referenceHasPrefix(path, []string{"needs"}) ||
				referenceHasPrefix(path, []string{"github", "event"}) ||
				referenceHasPrefix(path, []string{"github", "head_ref"}) ||
				referenceHasPrefix(path, []string{"github", "base_ref"}) ||
				referenceHasPrefix(path, []string{"github", "ref"}) ||
				referenceHasPrefix(path, []string{"github", "ref_name"}) ||
				referenceHasPrefix(path, []string{"inputs"}) {
				return true
			}
		}
	}
	return false
}
