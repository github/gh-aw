package workflow

import "errors"

// validateProgramCheckout fails closed where the compiler cannot establish
// that the workflow repository's program is available in the workspace.
func (c *Compiler) validateProgramCheckout(data *WorkflowData) error {
	if data.Program == "" {
		return nil
	}
	if onSectionHasAnyTrigger(data.On, "workflow_call") {
		return errors.New("program: does not support workflow_call: a reusable workflow may run in a different caller repository, so the callee program's source revision cannot be guaranteed; use another trigger")
	}
	if !c.shouldAddCheckoutStep(data) {
		return errors.New("program: requires the compiler-managed workflow repository checkout at the workspace root; enable checkout and remove manual checkout steps")
	}
	manager := NewCheckoutManager(data.CheckoutConfigs)
	if root := manager.GetDefaultCheckoutOverride(); root != nil {
		if root.key.wiki {
			return errors.New("program: requires a workflow repository checkout, not a wiki checkout")
		}
		if len(root.sparsePatterns) > 0 {
			return errors.New("program: does not support sparse checkout of the workflow repository; remove checkout.sparse-checkout")
		}
	}
	for _, entry := range manager.ordered {
		if !entry.isDefault() {
			// A program can be a symlink whose target changes with the checkout ref,
			// so no additional checkout path is provably disjoint at compile time.
			return errors.New("program: additional checkouts are not supported because they may overwrite the program source or its symlink target")
		}
	}
	return nil
}
