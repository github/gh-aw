package intent

import (
	"errors"
	"slices"

	"github.com/github/gh-aw/pkg/logger"
)

var governanceLog = logger.New("intent:governance")

// ErrToolDenied is returned by Authorizer.AuthorizeTool when the tool appears in
// the policy's DeniedTools list. A deny always wins, even if the same tool is
// also present in AllowedTools.
var ErrToolDenied = errors.New("intent: tool denied by policy")

// ErrToolNotAllowed is returned by Authorizer.AuthorizeTool when the policy's
// AllowedTools is non-nil (restricted) and does not contain the requested tool.
var ErrToolNotAllowed = errors.New("intent: tool not allowed by policy")

// Authorizer authorizes individual tool calls against a compiled ExecutionPolicy.
type Authorizer struct{}

// AuthorizeTool reports whether tool may be called under policy. DeniedTools is
// checked first and always wins, even if tool also appears in AllowedTools. A
// nil AllowedTools means unrestricted (any tool not explicitly denied is
// allowed); a non-nil AllowedTools (including an empty, non-nil slice) restricts
// calls to the listed tools, so a non-nil empty slice denies every tool.
func (a Authorizer) AuthorizeTool(policy ExecutionPolicy, tool string) error {
	if slices.Contains(policy.DeniedTools, tool) {
		governanceLog.Printf("Tool denied by policy: tool=%s", tool)
		return ErrToolDenied
	}
	if policy.AllowedTools != nil && !slices.Contains(policy.AllowedTools, tool) {
		governanceLog.Printf("Tool not in restricted allow-list: tool=%s", tool)
		return ErrToolNotAllowed
	}
	return nil
}
