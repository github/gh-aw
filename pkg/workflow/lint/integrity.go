package lint

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/workflow/actions"
)

const integrityWorkflowPrefix = ".github/workflows/"

var (
	integrityCommitPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
	integrityRemotePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*$`)
	integrityDockerPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?$`)
	integrityDigestPattern = regexp.MustCompile(`^sha(?:256:[a-f0-9]{64}|384:[a-f0-9]{96}|512:[a-f0-9]{128})$`)
)

// ActionPinRule checks reference integrity, not action provenance or existence.
// AW documents establish the authored strict policy; native Actions has no
// strict setting, so mutable references default to warnings. Malformed references
// are errors for either AST regardless of strict policy.
func ActionPinRule() Rule {
	return Rule{
		ID:           "action-pin",
		ThreatRuleID: "CTR-002",
		Description:  "Require well-formed action pins and immutable remote references",
		Severity:     Warning,
		Targets:      ActionsAST | AWAST,
		Check:        checkActionPins,
	}
}

func checkActionPins(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	if input.Workflow == nil {
		return checkAuthoredActionPins(ctx, input)
	}
	severity, err := integrityPinSeverity(input)
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	var failures []error
	for _, job := range input.Workflow.Jobs {
		if err := ctx.Err(); err != nil {
			return diagnostics, errors.Join(append(failures, err)...)
		}
		path := "jobs." + string(job.ID)
		if job.Uses != "" {
			if diagnostic := integrityPinDiagnostic(job.Uses, true, severity, path+".uses"); diagnostic != nil {
				diagnostics = append(diagnostics, *diagnostic)
			}
		}
		for index, step := range job.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			stepPath := fmt.Sprintf("%s.steps[%d]", path, index)
			fields, err := actions.LowerStep(step, job.ID, input.Workflow)
			if err != nil {
				// Lowering errors can contain authored values; expose only the
				// location and remediation at the lint boundary.
				failures = append(failures, fmt.Errorf("cannot inspect action reference at %s: invalid Actions step; correct the step before linting", stepPath))
				continue
			}
			if value, exists := fields["uses"]; exists {
				uses, ok := value.(string)
				if !ok {
					failures = append(failures, fmt.Errorf("cannot inspect action reference at %s.uses: expected a string", stepPath))
					continue
				}
				if diagnostic := integrityPinDiagnostic(uses, false, severity, stepPath+".uses"); diagnostic != nil {
					diagnostics = append(diagnostics, *diagnostic)
				}
			}
		}
	}
	return diagnostics, errors.Join(append(failures, ctx.Err())...)
}

func integrityPinSeverity(input Input) (Severity, error) {
	severity, err := sourcePolicySeverity(input, "action-pin")
	if err == nil && input.AllowActionRefs {
		severity = Warning
	}
	return severity, err
}

func integrityPinDiagnostic(uses string, reusable bool, severity Severity, path string) *Diagnostic {
	message, malformed := integrityReference(uses, reusable)
	if message == "" {
		return nil
	}
	if malformed {
		severity = Error
	}
	return &Diagnostic{Severity: severity, Path: path, Message: message}
}

func integrityReference(uses string, reusable bool) (string, bool) {
	const malformed = "Malformed action or reusable-workflow reference; use a repository-relative path, a remote reference pinned to a full commit SHA, or an action image pinned to a digest"
	if uses == "" || !utf8.ValidString(uses) || strings.Contains(uses, "${{") ||
		strings.Contains(uses, "\\") || strings.IndexFunc(uses, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) >= 0 {
		return malformed, true
	}
	if local, ok := strings.CutPrefix(uses, "./"); ok {
		if strings.Contains(local, "@") || !integrityPath(local, true) {
			return malformed, true
		}
		if reusable && !integrityWorkflowPath(local) {
			return malformed, true
		}
		return "", false
	}
	if strings.HasPrefix(uses, "docker://") {
		if reusable {
			return malformed, true
		}
		image, digest, pinned := strings.Cut(strings.TrimPrefix(uses, "docker://"), "@")
		if !integrityDockerPattern.MatchString(image) || pinned && !integrityDigestPattern.MatchString(digest) {
			return "Malformed Docker action reference; use a valid image followed by an immutable sha256, sha384, or sha512 digest", true
		}
		if !pinned {
			return "Docker action reference is mutable; pin the image to an immutable digest", false
		}
		return "", false
	}
	repository, ref, hasRef := strings.Cut(uses, "@")
	if !hasRef || !integrityRemotePattern.MatchString(repository) ||
		!integrityPath(repository, false) || !integrityGitRef(ref) {
		return malformed, true
	}
	if reusable {
		_, repositoryPath, _ := strings.Cut(repository, "/")
		_, workflowPath, found := strings.Cut(repositoryPath, "/")
		if !found || !integrityWorkflowPath(workflowPath) {
			return malformed, true
		}
	}
	if !integrityCommitPattern.MatchString(ref) {
		return "Remote action or reusable-workflow reference is mutable or abbreviated; pin it to a full 40-character commit SHA", false
	}
	return "", false
}

func integrityPath(value string, rootAllowed bool) bool {
	if value == "" {
		return rootAllowed
	}
	for component := range strings.SplitSeq(value, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

func integrityWorkflowPath(value string) bool {
	file, found := strings.CutPrefix(value, integrityWorkflowPrefix)
	return found && !strings.Contains(file, "/") &&
		(strings.HasSuffix(file, ".yml") || strings.HasSuffix(file, ".yaml"))
}

func integrityGitRef(value string) bool {
	if value == "" || value == "@" || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		strings.HasSuffix(value, ".") || strings.Contains(value, "..") || strings.Contains(value, "//") ||
		strings.ContainsAny(value, "@~^:?*[\\") {
		return false
	}
	for _, r := range value {
		if r <= ' ' || r == 127 {
			return false
		}
	}
	for component := range strings.SplitSeq(value, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

// JobTimeoutRule checks explicit timeouts only. An absent native Actions
// timeout retains Actions' default and is not a generated-timeout violation.
func JobTimeoutRule() Rule {
	return Rule{
		ID:           "job-timeout",
		ThreatRuleID: "CTR-026",
		Description:  "Require explicit job timeouts to be literal positive integers",
		Severity:     Error,
		Targets:      ActionsAST | AWAST,
		Check: func(ctx context.Context, input Input) ([]Diagnostic, error) {
			if err := integrityInput(ctx, input); err != nil {
				return nil, err
			}
			if input.Workflow == nil {
				return checkAuthoredTimeouts(ctx, input)
			}
			var diagnostics []Diagnostic
			for _, job := range input.Workflow.Jobs {
				if err := ctx.Err(); err != nil {
					return diagnostics, err
				}
				var message string
				switch {
				case job.TimeoutMinutesExpression != nil:
					message = "Job timeout is an expression or non-integer value; replace it with a literal positive integer"
				case job.TimeoutMinutes < 0 || job.TimeoutMinutesSet && job.TimeoutMinutes == 0:
					message = "Job timeout must be a literal positive integer; choose a value greater than zero"
				}
				if message != "" {
					diagnostics = append(diagnostics, Diagnostic{
						Path: "jobs." + string(job.ID) + ".timeout-minutes", Message: message,
					})
				}
			}
			return diagnostics, ctx.Err()
		},
	}
}

func integrityInput(ctx context.Context, input Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if input.Workflow == nil {
		if input.Document != nil {
			return nil
		}
		return errors.New("integrity lint requires an Actions or AW AST")
	}
	for _, job := range input.Workflow.Jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if job == nil {
			return errors.New("integrity lint cannot inspect a nil Actions job; correct the workflow before linting")
		}
	}
	return nil
}
