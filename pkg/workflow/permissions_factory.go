package workflow

import "github.com/github/gh-aw/pkg/logger"

var permissionsFactoryLog = logger.New("workflow:permissions_factory")

// NewPermissions creates a new Permissions with an empty map
func NewPermissions() *Permissions {
	return &Permissions{
		permissions: make(map[PermissionScope]PermissionLevel),
	}
}

// NewPermissionsReadAll creates a Permissions with read-all shorthand
func NewPermissionsReadAll() *Permissions {
	permissionsFactoryLog.Print("Creating permissions with read-all shorthand")
	return &Permissions{
		shorthand: "read-all",
	}
}

// NewPermissionsWriteAll creates a Permissions with write-all shorthand
func NewPermissionsWriteAll() *Permissions {
	permissionsFactoryLog.Print("Creating permissions with write-all shorthand")
	return &Permissions{
		shorthand: "write-all",
	}
}

// NewPermissionsNone creates a Permissions with none shorthand
func NewPermissionsNone() *Permissions {
	return &Permissions{
		shorthand: "none",
	}
}

// NewPermissionsEmpty creates a Permissions that explicitly renders as "permissions: {}"
func NewPermissionsEmpty() *Permissions {
	return &Permissions{
		permissions:   make(map[PermissionScope]PermissionLevel),
		explicitEmpty: true,
	}
}

// NewPermissionsFromMap creates a Permissions from a map of scopes to levels
func NewPermissionsFromMap(perms map[PermissionScope]PermissionLevel) *Permissions {
	if permissionsFactoryLog.Enabled() {
		permissionsFactoryLog.Printf("Creating permissions from map: scope_count=%d", len(perms))
	}
	p := NewPermissions()
	for scope, level := range perms {
		p.permissions[scope] = level
	}
	return p
}

// NewPermissionsAllRead creates a Permissions with all: read
func NewPermissionsAllRead() *Permissions {
	return &Permissions{
		hasAll:   true,
		allLevel: PermissionRead,
	}
}

// Helper functions for common permission patterns

// NewPermissionsContentsRead creates permissions with contents: read
func NewPermissionsContentsRead() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents: PermissionRead,
	})
}

// NewPermissionsContentsReadIssuesWrite creates permissions with contents: read and issues: write
func NewPermissionsContentsReadIssuesWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents: PermissionRead,
		PermissionIssues:   PermissionWrite,
	})
}

// NewPermissionsContentsReadIssuesWritePRWrite creates permissions with contents: read, issues: write, pull-requests: write
func NewPermissionsContentsReadIssuesWritePRWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:     PermissionRead,
		PermissionIssues:       PermissionWrite,
		PermissionPullRequests: PermissionWrite,
	})
}

// NewPermissionsContentsReadIssuesWritePRWriteDiscussionsWrite creates permissions with contents: read, issues: write, pull-requests: write, discussions: write
func NewPermissionsContentsReadIssuesWritePRWriteDiscussionsWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:     PermissionRead,
		PermissionIssues:       PermissionWrite,
		PermissionPullRequests: PermissionWrite,
		PermissionDiscussions:  PermissionWrite,
	})
}

// NewPermissionsActionsWrite creates permissions with actions: write
// This is required for dispatching workflows via workflow_dispatch
func NewPermissionsActionsWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionActions: PermissionWrite,
	})
}

// NewPermissionsActionsWriteContentsWriteIssuesWritePRWrite creates permissions with actions: write, contents: write, issues: write, pull-requests: write
// This is required for the replaceActorsForAssignable GraphQL mutation used to assign GitHub Copilot agents to issues
func NewPermissionsActionsWriteContentsWriteIssuesWritePRWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionActions:      PermissionWrite,
		PermissionContents:     PermissionWrite,
		PermissionIssues:       PermissionWrite,
		PermissionPullRequests: PermissionWrite,
	})
}

// NewPermissionsContentsWrite creates permissions with contents: write
func NewPermissionsContentsWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents: PermissionWrite,
	})
}

// NewPermissionsContentsWriteIssuesWritePRWrite creates permissions with contents: write, issues: write, pull-requests: write
func NewPermissionsContentsWriteIssuesWritePRWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:     PermissionWrite,
		PermissionIssues:       PermissionWrite,
		PermissionPullRequests: PermissionWrite,
	})
}

// NewPermissionsDiscussionsWrite creates permissions with discussions: write
func NewPermissionsDiscussionsWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionDiscussions: PermissionWrite,
	})
}

// NewPermissionsContentsReadDiscussionsWrite creates permissions with contents: read and discussions: write
func NewPermissionsContentsReadDiscussionsWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:    PermissionRead,
		PermissionDiscussions: PermissionWrite,
	})
}

// NewPermissionsContentsReadIssuesWriteDiscussionsWrite creates permissions with contents: read, issues: write, discussions: write
// This is used for create-discussion jobs that support fallback-to-issue when discussion creation fails
func NewPermissionsContentsReadIssuesWriteDiscussionsWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:    PermissionRead,
		PermissionIssues:      PermissionWrite,
		PermissionDiscussions: PermissionWrite,
	})
}

// NewPermissionsContentsReadPRWrite creates permissions with contents: read and pull-requests: write
func NewPermissionsContentsReadPRWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:     PermissionRead,
		PermissionPullRequests: PermissionWrite,
	})
}

// NewPermissionsContentsReadSecurityEventsWrite creates permissions with contents: read and security-events: write
func NewPermissionsContentsReadSecurityEventsWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:       PermissionRead,
		PermissionSecurityEvents: PermissionWrite,
	})
}

// NewPermissionsContentsReadSecurityEventsWriteActionsRead creates permissions with contents: read, security-events: write, actions: read
func NewPermissionsContentsReadSecurityEventsWriteActionsRead() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:       PermissionRead,
		PermissionSecurityEvents: PermissionWrite,
		PermissionActions:        PermissionRead,
	})
}

// NewPermissionsContentsReadProjectsWrite creates permissions with contents: read and organization-projects: write
// Note: organization-projects is only valid for GitHub App tokens, not workflow permissions
func NewPermissionsContentsReadProjectsWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:         PermissionRead,
		PermissionOrganizationProj: PermissionWrite,
	})
}

// NewPermissionsContentsWritePRReadIssuesRead creates permissions with contents: write, pull-requests: read, issues: read
func NewPermissionsContentsWritePRReadIssuesRead() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:     PermissionWrite,
		PermissionPullRequests: PermissionRead,
		PermissionIssues:       PermissionRead,
	})
}

// NewPermissionsContentsWriteIssuesWritePRWriteDiscussionsWrite creates permissions with contents: write, issues: write, pull-requests: write, discussions: write
func NewPermissionsContentsWriteIssuesWritePRWriteDiscussionsWrite() *Permissions {
	return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
		PermissionContents:     PermissionWrite,
		PermissionIssues:       PermissionWrite,
		PermissionPullRequests: PermissionWrite,
		PermissionDiscussions:  PermissionWrite,
	})
}
