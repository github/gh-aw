package workflow

import (
	"context"
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workflow/gradershell"
)

// Freeze repository-owned Bash into the downstream job, not a runtime checkout.
func prepareOperationalValueGrader(data *WorkflowData, markdownPath string) error {
	if data == nil || data.Graders == nil {
		return nil
	}
	grader := data.Graders.Graders["operational-value"]
	if grader == nil || grader.Run == "" {
		return nil
	}
	return grader.PrepareShellScript(markdownPath)
}

func (c *Compiler) warnLegacyOperationalValueRun(data *WorkflowData) {
	if data == nil || data.Graders == nil {
		return
	}
	grader := data.Graders.Graders["operational-value"]
	if grader == nil || grader.Run == "" {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		"graders.operational-value.run is deprecated. Migrate manually to graders.operational-value.script; automatic shell-to-JavaScript translation is not supported.",
	))
	c.IncrementWarningCount()
}

// PrepareShellScript validates and freezes a repository shell evaluator for compilation or replay.
func (grader *GraderDefinition) PrepareShellScript(markdownPath string) error {
	if grader.RunSourcePath != "" {
		markdownPath = grader.RunSourcePath
	}
	source, err := gradershell.Load(markdownPath, grader.Run)
	if err != nil {
		return fmt.Errorf("graders.operational-value.run: %w", err)
	}
	findings, err := gradershell.Check(context.Background(), source)
	if err != nil {
		return fmt.Errorf("graders.operational-value.run: %w", err)
	}
	if len(findings) > 0 {
		return fmt.Errorf("graders.operational-value.run line %d: %s", findings[0].Line, findings[0].Message)
	}
	grader.ShellScript = source
	return nil
}
