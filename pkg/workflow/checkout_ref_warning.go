package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// warnCheckoutRefWithSteps flags potentially untrusted code running before the sandbox.
func (c *Compiler) warnCheckoutRefWithSteps(data *WorkflowData, markdownPath string) {
	if data.CustomSteps == "" {
		return
	}
	var section struct {
		Steps []any `yaml:"steps"`
	}
	if err := yaml.Unmarshal([]byte(data.CustomSteps), &section); err != nil || len(section.Steps) == 0 {
		return
	}

	checkoutMgr := NewCheckoutManager(data.CheckoutConfigs)
	for _, entry := range checkoutMgr.ordered {
		if entry.isDefault() && !c.shouldAddCheckoutStep(data) {
			continue
		}
		if expressionReferencePattern(true, []string{"github", "event", "repository", "default_branch"}, 4, -1).MatchString(strings.TrimSpace(entry.ref)) {
			continue
		}
		for _, path := range interpolationReferences(entry.ref) {
			if !referenceHasPrefix(path, []string{"needs"}) &&
				!referenceHasPrefix(path, []string{"github", "event"}) &&
				!referenceHasPrefix(path, []string{"inputs"}) {
				continue
			}
			msg := "checkout.ref is set to an expression and imported or custom steps run after it. " +
				"Those steps execute the checked-out ref's code on the host, outside the sandbox. " +
				"Keep checkout on the default branch and switch branches after setup in steps:, for example: " +
				"git checkout -B \"$BRANCH\" --track \"origin/$BRANCH\". " +
				"Fetch the branch at checkout time with checkout.fetch and pass BRANCH through the step's env."
			fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", msg))
			c.IncrementWarningCount()
			return
		}
	}
}
