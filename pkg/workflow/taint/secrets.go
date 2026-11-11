package taint

import (
	"strings"

	"github.com/rhysd/actionlint"
)

// SecretLeakRule reports disclosure, not ordinary credential consumption.
// Encoding, formatting, masking registration, and env indirection do not remove
// taint. Boolean predicates deliberately declassify presence/equality results.
func SecretLeakRule() Rule {
	return Rule{
		ID: "secret-leak",
		IsSource: func(ref string) bool {
			return ref == "secrets" || strings.HasPrefix(ref, "secrets.") ||
				ref == "github" || ref == "github.token" || ref == "github.*"
		},
		IsFilter: func(node actionlint.ExprNode) bool {
			switch n := node.(type) {
			case *actionlint.CompareOpNode, *actionlint.NotOpNode:
				return true
			case *actionlint.FuncCallNode:
				switch strings.ToLower(n.Callee) {
				case "contains", "startswith", "endswith":
					return true
				}
			}
			return false
		},
		IsSink: secretSink,
		ActionSources: func(uses string) []string {
			switch actionName(uses) {
			case "actions/create-github-app-token", "tibdex/github-app-token":
				return []string{"token"}
			default:
				return nil
			}
		},
	}
}

func actionName(uses string) string {
	name, _, _ := strings.Cut(strings.ToLower(uses), "@")
	return name
}

func secretSink(site Site) bool {
	if site.Kind != ActionInput {
		return true
	}
	name := actionName(site.Uses)
	switch name {
	case "actions/cache", "actions/cache/restore", "actions/cache/save":
		return site.Input == "key" || site.Input == "restore-keys"
	case "actions/upload-artifact":
		return site.Input == "name" || site.Input == "path"
	case "peter-evans/create-or-update-comment":
		return site.Input == "body"
	case "peter-evans/create-pull-request":
		return site.Input == "body" || site.Input == "title" || site.Input == "commit-message"
	case "softprops/action-gh-release":
		return site.Input == "body" || site.Input == "name"
	default:
		return false
	}
}
