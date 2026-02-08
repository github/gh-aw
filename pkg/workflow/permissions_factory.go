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
// These functions are maintained for backward compatibility.
// New code should prefer using NewPermissionsBuilder() for a more flexible API.

// NewPermissionsContentsRead creates permissions with contents: read
// Deprecated: Use NewPermissionsBuilder().WithContents(PermissionRead).Build() for new code
func NewPermissionsContentsRead() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		Build()
}

// NewPermissionsContentsReadIssuesWrite creates permissions with contents: read and issues: write
// Deprecated: Use NewPermissionsBuilder().WithContents(PermissionRead).WithIssues(PermissionWrite).Build() for new code
func NewPermissionsContentsReadIssuesWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithIssues(PermissionWrite).
		Build()
}

// NewPermissionsContentsReadIssuesWritePRWrite creates permissions with contents: read, issues: write, pull-requests: write
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsReadIssuesWritePRWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithIssues(PermissionWrite).
		WithPullRequests(PermissionWrite).
		Build()
}

// NewPermissionsContentsReadIssuesWritePRWriteDiscussionsWrite creates permissions with contents: read, issues: write, pull-requests: write, discussions: write
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsReadIssuesWritePRWriteDiscussionsWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithIssues(PermissionWrite).
		WithPullRequests(PermissionWrite).
		WithDiscussions(PermissionWrite).
		Build()
}

// NewPermissionsActionsWrite creates permissions with actions: write
// This is required for dispatching workflows via workflow_dispatch
// Deprecated: Use NewPermissionsBuilder().WithActions(PermissionWrite).Build() for new code
func NewPermissionsActionsWrite() *Permissions {
	return NewPermissionsBuilder().
		WithActions(PermissionWrite).
		Build()
}

// NewPermissionsActionsWriteContentsWriteIssuesWritePRWrite creates permissions with actions: write, contents: write, issues: write, pull-requests: write
// This is required for the replaceActorsForAssignable GraphQL mutation used to assign GitHub Copilot agents to issues
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsActionsWriteContentsWriteIssuesWritePRWrite() *Permissions {
	return NewPermissionsBuilder().
		WithActions(PermissionWrite).
		WithContents(PermissionWrite).
		WithIssues(PermissionWrite).
		WithPullRequests(PermissionWrite).
		Build()
}

// NewPermissionsContentsWrite creates permissions with contents: write
// Deprecated: Use NewPermissionsBuilder().WithContents(PermissionWrite).Build() for new code
func NewPermissionsContentsWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionWrite).
		Build()
}

// NewPermissionsContentsWriteIssuesWritePRWrite creates permissions with contents: write, issues: write, pull-requests: write
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsWriteIssuesWritePRWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionWrite).
		WithIssues(PermissionWrite).
		WithPullRequests(PermissionWrite).
		Build()
}

// NewPermissionsDiscussionsWrite creates permissions with discussions: write
// Deprecated: Use NewPermissionsBuilder().WithDiscussions(PermissionWrite).Build() for new code
func NewPermissionsDiscussionsWrite() *Permissions {
	return NewPermissionsBuilder().
		WithDiscussions(PermissionWrite).
		Build()
}

// NewPermissionsContentsReadDiscussionsWrite creates permissions with contents: read and discussions: write
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsReadDiscussionsWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithDiscussions(PermissionWrite).
		Build()
}

// NewPermissionsContentsReadIssuesWriteDiscussionsWrite creates permissions with contents: read, issues: write, discussions: write
// This is used for create-discussion jobs that support fallback-to-issue when discussion creation fails
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsReadIssuesWriteDiscussionsWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithIssues(PermissionWrite).
		WithDiscussions(PermissionWrite).
		Build()
}

// NewPermissionsContentsReadPRWrite creates permissions with contents: read and pull-requests: write
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsReadPRWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithPullRequests(PermissionWrite).
		Build()
}

// NewPermissionsContentsReadSecurityEventsWrite creates permissions with contents: read and security-events: write
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsReadSecurityEventsWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithSecurityEvents(PermissionWrite).
		Build()
}

// NewPermissionsContentsReadSecurityEventsWriteActionsRead creates permissions with contents: read, security-events: write, actions: read
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsReadSecurityEventsWriteActionsRead() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithSecurityEvents(PermissionWrite).
		WithActions(PermissionRead).
		Build()
}

// NewPermissionsContentsReadProjectsWrite creates permissions with contents: read and organization-projects: write
// Note: organization-projects is only valid for GitHub App tokens, not workflow permissions
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsReadProjectsWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithOrganizationProjects(PermissionWrite).
		Build()
}

// NewPermissionsContentsWritePRReadIssuesRead creates permissions with contents: write, pull-requests: read, issues: read
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsWritePRReadIssuesRead() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionWrite).
		WithPullRequests(PermissionRead).
		WithIssues(PermissionRead).
		Build()
}

// NewPermissionsContentsWriteIssuesWritePRWriteDiscussionsWrite creates permissions with contents: write, issues: write, pull-requests: write, discussions: write
// Deprecated: Use NewPermissionsBuilder() for new code
func NewPermissionsContentsWriteIssuesWritePRWriteDiscussionsWrite() *Permissions {
	return NewPermissionsBuilder().
		WithContents(PermissionWrite).
		WithIssues(PermissionWrite).
		WithPullRequests(PermissionWrite).
		WithDiscussions(PermissionWrite).
		Build()
}
