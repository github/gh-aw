package execcommandwithoutcontext

import (
	"context"
	"os/exec"
)

func overlapExecCommand(ctx context.Context, name string) *exec.Cmd {
	return exec.Command /* keep */ (name) // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
}
