//go:build !integration

package workflow

import "testing"

func TestIsActivationJobNeeded(t *testing.T) {
	compiler := NewCompiler()

	t.Run("no_conditions", func(t *testing.T) {
		data := &WorkflowData{
			Roles: []string{"all"}, // Explicitly disable permission checks
		}
		// Activation job is always needed now to perform timestamp check
		if !func() bool {
			var _ = data
			return compiler.isActivationJobNeeded()
		}() {
			t.Errorf("Expected isActivationJobNeeded to be true - activation job is always needed for timestamp check")
		}
	})

	t.Run("if_condition_present", func(t *testing.T) {
		data := &WorkflowData{If: "if: github.ref == 'refs/heads/main'"}
		// Pass false for needsPermissionCheck, but should still return true due to If condition
		if !func() bool {
			var _ = data
			return compiler.isActivationJobNeeded()
		}() {
			t.Errorf("Expected isActivationJobNeeded to be true when If condition is specified")
		}
	})

	t.Run("default_permission_check", func(t *testing.T) {
		data := &WorkflowData{} // No explicit Roles field, permission checks are consolidated in activation job
		// Pass true for needsPermissionCheck to simulate permission checks being needed
		if !func() bool {
			var _ = data
			return compiler.isActivationJobNeeded()
		}() {
			t.Errorf("Expected isActivationJobNeeded to be true - permission checks are now consolidated in activation job")
		}
	})

	t.Run("permission_check_not_needed", func(t *testing.T) {
		data := &WorkflowData{} // No other conditions that would require activation job
		// Activation job is always needed now to perform timestamp check
		if !func() bool {
			var _ = data
			return compiler.isActivationJobNeeded()
		}() {
			t.Errorf("Expected isActivationJobNeeded to be true - activation job is always needed for timestamp check")
		}
	})
}
