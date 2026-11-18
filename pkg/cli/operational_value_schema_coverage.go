package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

type schemaCoverageMeasurement struct{}

const schemaCoverageWorkflowPath = logsWorkflowsPathSegment + "schema-feature-coverage.lock.yml"

type valueCoverageSnapshot struct {
	Covered int      `json:"covered"`
	Fields  []string `json:"fields"`
	Missing []string `json:"missing"`
}

type valueCoverageChange struct {
	Closed  []string `json:"closed"`
	Removed []string `json:"removed"`
	Added   []string `json:"added"`
}

type valueCohort struct {
	Fields []string `json:"fields"`
	Before int      `json:"before"`
	After  int      `json:"after"`
}

func (schemaCoverageMeasurement) definition() valueMeasurementDefinition {
	return valueMeasurementDefinition{
		ID: "schema-textual-coverage-v1", Name: "Schema fields with an example", Unit: "fields",
		Host: "github.com", Repository: "github/gh-aw", Workflow: "schema-feature-coverage",
		WorkflowPath: schemaCoverageWorkflowPath,
		Direction:    "higher_is_better",
	}
}

func (schemaCoverageMeasurement) measure(git valueGit, sha string) (valueSnapshot, error) {
	coverage := &valueCoverageSnapshot{Fields: []string{}, Missing: []string{}}
	snapshot := valueSnapshot{SHA: sha, Coverage: coverage}
	schema, err := git.output("show", sha+":pkg/parser/schemas/main_workflow_schema.json")
	if err != nil {
		return snapshot, err
	}
	var document struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &document); err != nil {
		return snapshot, fmt.Errorf("read schema at %s: %w", sha, err)
	}
	if len(document.Properties) == 0 {
		return snapshot, fmt.Errorf("empty schema population at %s is unavailable, not zero coverage", sha)
	}
	for field := range document.Properties {
		coverage.Fields = append(coverage.Fields, field)
	}
	slices.Sort(coverage.Fields)
	lines, err := schemaCoverageLines(git, sha)
	if err != nil {
		return snapshot, err
	}
	have := make(map[string]struct{})
	for line := range strings.SplitSeq(lines, "\n") {
		have[strings.TrimSuffix(line, ":")] = struct{}{}
	}
	for _, field := range coverage.Fields {
		if _, covered := have[field]; covered {
			coverage.Covered++
		} else {
			coverage.Missing = append(coverage.Missing, field)
		}
	}
	snapshot.Value = new(float64(coverage.Covered))
	return snapshot, nil
}

func schemaCoverageLines(git valueGit, sha string) (string, error) {
	inventory, err := git.run("ls-tree", "-r", "--name-only", "-z", sha, "--", logsWorkflowsPathSegment, "schema-demos")
	if err != nil {
		return "", err
	}
	found := false
	for file := range strings.SplitSeq(inventory, "\x00") {
		if strings.HasSuffix(file, ".md") {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("missing Markdown inventory at %s", sha)
	}
	lines, err := git.run("grep", "-h", "-o", "-E", "^[^:[:space:]]+:", sha, "--",
		":(glob).github/workflows/**/*.md", ":(glob)schema-demos/**/*.md")
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return "", fmt.Errorf("read Markdown inventory at %s: %w", sha, err)
	}
	return lines, nil
}

func (schemaCoverageMeasurement) change(before, after valueSnapshot) valueChange {
	if before.Coverage == nil || after.Coverage == nil {
		return valueChange{Reason: "schema_coverage_evidence_missing"}
	}
	coverage := &valueCoverageChange{
		Closed:  []string{},
		Removed: valueDifference(before.Coverage.Fields, after.Coverage.Fields),
		Added:   valueDifference(after.Coverage.Fields, before.Coverage.Fields),
	}
	change := valueChange{Coverage: coverage}
	if len(coverage.Removed) != 0 || len(coverage.Added) != 0 {
		change.Reason = "schema_population_changed"
		return change
	}
	coverage.Closed = valueDifference(before.Coverage.Missing, after.Coverage.Missing)
	value := float64(len(coverage.Closed))
	change.Value = &value
	return change
}

func (schemaCoverageMeasurement) cohort(weekly []valueSnapshot) *valueCohort {
	if len(weekly) == 0 {
		return nil
	}
	first, last := weekly[0], weekly[len(weekly)-1] //nolint:uncheckedsliceindex // Weekly is nonempty above.
	cohort := &valueCohort{Fields: []string{}}
	for _, field := range first.Coverage.Fields {
		if slices.ContainsFunc(weekly, func(snapshot valueSnapshot) bool {
			return !slices.Contains(snapshot.Coverage.Fields, field)
		}) {
			continue
		}
		cohort.Fields = append(cohort.Fields, field)
		if !slices.Contains(first.Coverage.Missing, field) {
			cohort.Before++
		}
		if !slices.Contains(last.Coverage.Missing, field) {
			cohort.After++
		}
	}
	return cohort
}

func (schemaCoverageMeasurement) format(snapshot valueSnapshot) string {
	return fmt.Sprintf("%d/%d fields", snapshot.Coverage.Covered, len(snapshot.Coverage.Fields))
}

func valueDifference(a, b []string) []string {
	result := []string{}
	for _, item := range a {
		if !slices.Contains(b, item) {
			result = append(result, item)
		}
	}
	return result
}
