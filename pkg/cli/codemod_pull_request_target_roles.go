package cli

import (
	"errors"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/codemod"
)

var pullRequestTargetRolesCodemodLog = logger.New("cli:codemod_pull_request_target_roles")

func getPullRequestTargetRolesCodemod() Codemod {
	return Codemod{
		ID:           "pull-request-target-restrict-roles",
		Name:         "Restrict untrusted pull_request_target roles",
		Description:  "Removes read/all from on.roles for strict pull_request_target workflows, preserving other listed roles or using [admin, maintainer, write] when none remain.",
		IntroducedIn: "1.5.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			if isFrontmatterStrictFalse(frontmatter) || !hasPullRequestTargetTrigger(frontmatter) {
				return content, false, nil
			}
			on, ok := frontmatter["on"].(map[string]any)
			if !ok {
				return content, false, nil
			}
			raw, exists := on["roles"]
			if !exists {
				return content, false, nil
			}
			roles, err := restrictedPullRequestTargetRoles(raw)
			if err != nil || roles == nil {
				return content, false, err
			}
			pullRequestTargetRolesCodemodLog.Print("Restricting pull_request_target admission roles")
			return codemod.Apply(content, func(f *codemod.Frontmatter) error {
				return f.Set([]string{"on", "roles"}, roles)
			})
		},
	}
}

func restrictedPullRequestTargetRoles(raw any) ([]string, error) {
	var entries []any
	switch roles := raw.(type) {
	case string:
		entries = []any{roles}
	case []string:
		for _, role := range roles {
			entries = append(entries, role)
		}
	case []any:
		entries = roles
	default:
		return nil, errors.New("on.roles must be a role string or a list of role strings; correct it before restricting pull_request_target roles")
	}
	var retained []string
	unsafe := false
	for _, entry := range entries {
		role, ok := entry.(string)
		if !ok {
			return nil, errors.New("on.roles must contain only role strings; correct it before restricting pull_request_target roles")
		}
		switch role {
		case "read", "all":
			unsafe = true
		case "admin", "maintainer", "maintain", "write", "triage":
			retained = append(retained, role)
		default:
			return nil, errors.New("on.roles contains an invalid role; use supported repository roles before restricting pull_request_target roles")
		}
	}
	if !unsafe {
		return nil, nil
	}
	if len(retained) == 0 {
		retained = []string{"admin", "maintainer", "write"}
	}
	return retained, nil
}
