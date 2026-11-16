//go:build !integration

package workflow

import (
	"slices"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/lint"
)

// TestUntrustedRolesOnSettingsMatchGatewayDispatch keeps the lint package's
// copy of the on: settings keys in sync with gatewayDispatchOnSettings. The
// lint package cannot import pkg/workflow, so the list is duplicated there.
func TestUntrustedRolesOnSettingsMatchGatewayDispatch(t *testing.T) {
	lintKeys := lint.UntrustedRolesOnSettingKeys()
	var missing, extra []string
	for _, key := range gatewayDispatchOnSettings {
		if !slices.Contains(lintKeys, key) {
			missing = append(missing, key)
		}
	}
	for _, key := range lintKeys {
		if !slices.Contains(gatewayDispatchOnSettings, key) {
			extra = append(extra, key)
		}
	}
	if len(missing) > 0 {
		t.Errorf("lint untrustedRolesOnSettings is missing gatewayDispatchOnSettings keys %v; add them in pkg/workflow/lint/untrusted_roles_detection.go", missing)
	}
	if len(extra) > 0 {
		t.Errorf("lint untrustedRolesOnSettings has keys not in gatewayDispatchOnSettings %v; remove them or add them in pkg/workflow/compiler_webhook_triggers.go", extra)
	}
}
