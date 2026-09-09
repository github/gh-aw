//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

// loadPermissionsSchemaDefs reads pkg/parser/schemas/main_workflow_schema.json and
// returns the property names declared in the given $defs entry's "properties" object.
func loadPermissionsSchemaDefProperties(t *testing.T, defName string) map[string]struct{} {
	t.Helper()

	schemaBytes, err := os.ReadFile("../parser/schemas/main_workflow_schema.json")
	require.NoError(t, err, "should be able to read main_workflow_schema.json")

	var schema map[string]any
	require.NoError(t, json.Unmarshal(schemaBytes, &schema), "schema should be valid JSON")

	defs, ok := schema["$defs"].(map[string]any)
	require.True(t, ok, "schema should have $defs")

	def, ok := defs[defName].(map[string]any)
	require.True(t, ok, "$defs should have %q", defName)

	properties, ok := def["properties"].(map[string]any)
	require.True(t, ok, "%q should have properties", defName)

	names := make(map[string]struct{}, len(properties))
	for name := range properties {
		names[name] = struct{}{}
	}
	return names
}

// TestPermissionConstantsMatchSchemaEnum is a regression test that guards against
// pkg/workflow/permissions.go's PermissionScope constants drifting out of sync with
// the $defs.github_actions_permissions and $defs.github_app_permissions property
// enums in pkg/parser/schemas/main_workflow_schema.json.
//
// This class of drift (e.g. a new permission constant added without a matching schema
// entry) previously caused Schema Consistency Checker findings such as the missing
// "secret-scanning-alerts" entry (see github/gh-aw#54737). If this test fails, add the
// missing permission scope(s) to the appropriate $defs entry in main_workflow_schema.json.
func TestPermissionConstantsMatchSchemaEnum(t *testing.T) {
	actionsPermProps := loadPermissionsSchemaDefProperties(t, "github_actions_permissions")
	appPermProps := loadPermissionsSchemaDefProperties(t, "github_app_permissions")

	// GITHUB_TOKEN-supported scopes (plus the special-cased copilot-requests scope,
	// which is intentionally excluded from GetAllPermissionScopes() but must still be
	// recognized) must appear under github_actions_permissions.
	tokenScopes := append([]PermissionScope{}, GetAllPermissionScopes()...)
	tokenScopes = append(tokenScopes, PermissionCopilotRequests)
	for _, scope := range tokenScopes {
		if _, ok := actionsPermProps[string(scope)]; !ok {
			t.Errorf("permission scope %q from GetAllPermissionScopes()/PermissionCopilotRequests is missing from "+
				"$defs.github_actions_permissions.properties in pkg/parser/schemas/main_workflow_schema.json", scope)
		}
	}

	// Workflow-level permissions continue to accept custom org/repo role scopes,
	// even though they are only available via GitHub App tokens when minting a
	// token for the workflow.
	for _, scope := range []PermissionScope{
		PermissionOrganizationCustomOrgRoles,
		PermissionOrganizationCustomRepositoryRoles,
	} {
		if _, ok := actionsPermProps[string(scope)]; !ok {
			t.Errorf("permission scope %q from GetAllGitHubAppOnlyScopes() is missing from "+
				"$defs.github_actions_permissions.properties in pkg/parser/schemas/main_workflow_schema.json", scope)
		}
	}

	// GitHub App-only scopes must appear under either github_actions_permissions
	// (e.g. organization-projects, which is grouped there for historical reasons) or
	// github_app_permissions.
	for _, scope := range GetAllGitHubAppOnlyScopes() {
		_, inActionsPerms := actionsPermProps[string(scope)]
		_, inAppPerms := appPermProps[string(scope)]
		if !inActionsPerms && !inAppPerms {
			t.Errorf("GitHub App-only permission scope %q from GetAllGitHubAppOnlyScopes() is missing from both "+
				"$defs.github_actions_permissions.properties and $defs.github_app_permissions.properties in "+
				"pkg/parser/schemas/main_workflow_schema.json", scope)
		}
	}
}

// TestPermissionsSchemaEnumMatchesConstants is the inverse check: every permission
// scope declared in the schema's github_actions_permissions/github_app_permissions
// $defs must correspond to a known PermissionScope constant, so the schema doesn't
// drift ahead of permissions.go either.
func TestPermissionsSchemaEnumMatchesConstants(t *testing.T) {
	actionsPermProps := loadPermissionsSchemaDefProperties(t, "github_actions_permissions")
	appPermProps := loadPermissionsSchemaDefProperties(t, "github_app_permissions")

	known := make(map[string]struct{})
	for _, scope := range GetAllPermissionScopes() {
		known[string(scope)] = struct{}{}
	}
	for _, scope := range GetAllGitHubAppOnlyScopes() {
		known[string(scope)] = struct{}{}
	}
	known[string(PermissionCopilotRequests)] = struct{}{}

	for name := range actionsPermProps {
		if name == "all" {
			continue // "all" is a meta-key, not a real permission scope
		}
		if _, ok := known[name]; !ok {
			t.Errorf("schema property %q in $defs.github_actions_permissions is not a known PermissionScope constant "+
				"in pkg/workflow/permissions.go", name)
		}
	}
	for name := range appPermProps {
		if name == "all" {
			continue // "all" is a meta-key, not a real permission scope
		}
		if _, ok := known[name]; !ok {
			t.Errorf("schema property %q in $defs.github_app_permissions is not a known PermissionScope constant "+
				"in pkg/workflow/permissions.go", name)
		}
	}
}

// TestOrganizationCustomRolePermissionsValidateAgainstSchema is a regression test that
// ensures workflow frontmatter using the organization-custom-org-roles and
// organization-custom-repository-roles permission scopes validates cleanly against the
// JSON Schema, and that invalid values for these scopes are still rejected.
func TestOrganizationCustomRolePermissionsValidateAgainstSchema(t *testing.T) {
	for _, scope := range []string{"organization-custom-org-roles", "organization-custom-repository-roles"} {
		t.Run(scope+"/valid", func(t *testing.T) {
			frontmatter := map[string]any{
				"on": "workflow_dispatch",
				"permissions": map[string]any{
					scope: "read",
				},
			}
			if err := parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "workflow.md"); err != nil {
				t.Fatalf("expected %q permission scope to validate cleanly, got error: %v", scope, err)
			}
		})

		t.Run(scope+"/invalid-value", func(t *testing.T) {
			frontmatter := map[string]any{
				"on": "workflow_dispatch",
				"permissions": map[string]any{
					scope: "not-a-real-value",
				},
			}
			if err := parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "workflow.md"); err == nil {
				t.Fatalf("expected invalid value for %q permission scope to be rejected", scope)
			}
		})
	}
}
