package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidateEnvironmentOverride validates a literal GitHub Actions environment name.
// An empty name means that no override was requested.
func ValidateEnvironmentOverride(name string) error {
	if name == "" {
		return nil
	}
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 255 {
		return errors.New("--environment requires a non-blank name of at most 255 characters")
	}
	if !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) || strings.Contains(name, "${{") {
		return errors.New("--environment requires a literal name without control characters or GitHub Actions expressions")
	}
	return nil
}

func (c *Compiler) applyEnvironmentOverride() error {
	if err := ValidateEnvironmentOverride(c.environmentOverride); err != nil {
		return err
	}
	if c.environmentOverride == "" {
		return nil
	}

	jobs := c.jobManager.GetAllJobs()
	names := make([]string, 0, len(jobs))
	for name := range jobs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if jobs[name].Uses != "" {
			return fmt.Errorf("--environment cannot override job '%s': GitHub Actions reusable-workflow callers cannot declare an environment; replace the caller with a steps-based job or configure environments in the called workflow and compile the caller without --environment", name)
		}
	}

	environment := "environment: " + strconv.Quote(c.environmentOverride)
	for _, job := range jobs {
		job.Environment = environment
	}
	return nil
}
