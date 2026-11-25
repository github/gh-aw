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

// warnCheckoutRefWithSteps flags potentially untrusted code running outside the sandbox.
func (c *Compiler) warnCheckoutRefWithSteps(data *WorkflowData, markdownPath string) {
	if !hasCheckoutRefWarningHostSteps(data) {
		checkoutRefWarningLog.Print("No host steps or pre-agent-steps; skipping checkout ref warning check")
		return
	}

	checkoutRefWarningLog.Printf("Checking %d checkout config(s) for untrusted refs before host steps", len(data.CheckoutConfigs))
	checkoutMgr := NewCheckoutManager(data.CheckoutConfigs)
	defaultCheckout := checkoutMgr.GetDefaultCheckoutOverride()
	entries := checkoutMgr.ordered
	if data.Program != "" && c.shouldAddCheckoutStep(data) && onSectionHasAnyTrigger(data.On, "pull_request") &&
		(defaultCheckout == nil || defaultCheckout.ref == "") {
		// actions/checkout defaults to the event's PR merge ref, which can contain
		// changes from the pull request. Programs do not restore agent config.
		implicitDefault := &resolvedCheckout{ref: "${{ github.ref }}"}
		if defaultCheckout != nil {
			*implicitDefault = *defaultCheckout
			implicitDefault.ref = "${{ github.ref }}"
		}
		entries = append(append([]*resolvedCheckout(nil), entries...), implicitDefault)
		defaultCheckout = implicitDefault
	}
	for _, entry := range entries {
		if entry.isDefault() && (entry != defaultCheckout || !c.shouldAddCheckoutStep(data)) {
			continue
		}
		if (entry.dataOnly && entry.key.path != "") || !checkoutRefMayBeUntrusted(entry.key.repository, entry.ref) {
			continue
		}
		mitigation := "For PR-head work, use compiler-managed automatic PR checkout so trusted agent configuration is restored."
		if data.Program != "" {
			mitigation = "Programs execute from the checkout without agent-config restoration; select a trusted ref for program source."
		}
		msg := fmt.Sprintf(
			"checkout step %q uses checkout.ref %q with an untrusted or unparseable expression, and host code runs after it. "+
				"Programs, imported or workflow-owned steps, and pre-agent-steps execute outside the sandbox and may run checked-out code. "+
				"Keep the agent workspace and executable host tooling on a trusted ref, inspect untrusted refs only as data, and do not check them out or run their scripts on the host. "+
				"A separate checkout consumed only as data may set data-only: true as a self-attested acknowledgement (not enforced isolation). "+
				"%s",
			checkoutStepName(entry.key),
			entry.ref,
			mitigation,
		)
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", msg))
		c.IncrementWarningCount()
		return
	}
}

func hasCheckoutRefWarningHostSteps(data *WorkflowData) bool {
	return data.Program != "" || hasCheckoutRefWarningSteps(data.CustomSteps, "steps") ||
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
