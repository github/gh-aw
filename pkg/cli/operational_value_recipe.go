package cli

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

type valueRecipe struct {
	Version             int    `json:"version"`
	ID                  string `json:"id"`
	Repository          string `json:"repository"`
	Workflow            string `json:"workflow"`
	Start               string `json:"start"`
	End                 string `json:"end"`
	Author              string `json:"author,omitempty"`
	AuthorLogin         string `json:"author_login,omitempty"`
	EarlyBatchesThrough string `json:"early_batches_through,omitempty"`
	Witnesses           []int  `json:"witnesses"`
	Digest              string `json:"-"`
}

type valueRecipeEvidence struct {
	Definition valueRecipe `json:"definition"`
	Digest     string      `json:"digest"`
}

type valueInspectedWindow struct {
	Label      string           `json:"label"`
	Kind       string           `json:"kind"`
	PRs        []int            `json:"prs"`
	First      time.Time        `json:"first"`
	Last       time.Time        `json:"last"`
	Before     valueSnapshot    `json:"before"`
	After      valueSnapshot    `json:"after"`
	End        *valueSnapshot   `json:"end"`
	Samples    []*valueSnapshot `json:"samples"`
	Closed     []string         `json:"closed"`
	Removed    []string         `json:"removed"`
	Attributed *float64         `json:"attributed_value"`
	Status     string           `json:"status"`
	Reason     string           `json:"reason"`
}

var valueRecipeAuthor = regexp.MustCompile(`^[A-Za-z0-9_/-]+$`)

func loadValueRecipe(filename string, definition valueMeasurementDefinition) (*valueRecipe, error) {
	if filename == "" {
		return nil, nil
	}
	var recipe valueRecipe
	digest, err := readValueDeclaration(filename, &recipe)
	if err != nil {
		return nil, err
	}
	if recipe.Version != 1 || strings.TrimSpace(recipe.ID) == "" ||
		recipe.Repository != path.Join(definition.Host, definition.Repository) || recipe.Workflow != definition.Workflow {
		return nil, errors.New("value recipe requires version 1, an id, and repository/workflow matching the selected [HOST/]OWNER/REPO and workflow")
	}
	if _, err := time.Parse(time.DateOnly, recipe.Start); err != nil {
		return nil, fmt.Errorf("value recipe start: %w", err)
	}
	if _, err := time.Parse(time.DateOnly, recipe.End); err != nil {
		return nil, fmt.Errorf("value recipe end: %w", err)
	}
	if recipe.EarlyBatchesThrough != "" {
		if _, err := time.Parse(time.DateOnly, recipe.EarlyBatchesThrough); err != nil {
			return nil, fmt.Errorf("value recipe early_batches_through: %w", err)
		}
		if recipe.EarlyBatchesThrough < recipe.Start || recipe.EarlyBatchesThrough > recipe.End {
			return nil, errors.New("value recipe early_batches_through must be within start and end")
		}
	}
	if (recipe.Author == "") != (recipe.AuthorLogin == "") ||
		recipe.Author != "" && !valueRecipeAuthor.MatchString(recipe.Author) {
		return nil, errors.New("value recipe author and author_login must both be provided; author must be a GitHub search author identifier")
	}
	seen := make(map[int]struct{})
	for _, pr := range recipe.Witnesses {
		if _, duplicate := seen[pr]; duplicate || pr <= 0 {
			return nil, errors.New("value recipe witnesses must be distinct positive pull-request numbers")
		}
		seen[pr] = struct{}{}
	}
	recipe.Digest = digest
	return &recipe, nil
}

func inspectValueRecipe(report *operationalValueReport, recipe *valueRecipe) error {
	if recipe == nil {
		return nil
	}
	report.Recipe = &valueRecipeEvidence{Definition: *recipe, Digest: recipe.Digest}
	report.InspectedWindows = []valueInspectedWindow{}
	windows := slices.Clone(report.Windows)
	slices.SortFunc(windows, func(a, b valueWindow) int {
		if order := a.MergedAt.Compare(b.MergedAt); order != 0 {
			return order
		}
		return a.PR - b.PR
	})
	batches := make(map[string][]valueWindow)
	found := make(map[int]struct{})
	for _, window := range windows {
		if len(window.Samples) != 14 {
			return fmt.Errorf("PR %d has incomplete daily sample slots; expected 14", window.PR)
		}
		day := window.MergedAt.UTC().Format(time.DateOnly)
		if recipe.EarlyBatchesThrough != "" && day <= recipe.EarlyBatchesThrough {
			batches[day] = append(batches[day], window)
		}
		if slices.Contains(recipe.Witnesses, window.PR) {
			if recipe.EarlyBatchesThrough != "" && day <= recipe.EarlyBatchesThrough {
				return fmt.Errorf("witness PR %d also belongs to an early batch", window.PR)
			}
			if window.Change.Value == nil {
				return fmt.Errorf("witness PR %d has unavailable measurement: %s", window.PR, window.Change.Reason)
			}
			found[window.PR] = struct{}{}
			report.InspectedWindows = append(report.InspectedWindows, inspectValueMembers([]valueWindow{window}, "single"))
		}
	}
	for _, pr := range recipe.Witnesses {
		if _, ok := found[pr]; !ok {
			return fmt.Errorf("witness PR %d is not a merged output in the selected cohort", pr)
		}
	}
	for _, members := range batches {
		report.InspectedWindows = append(report.InspectedWindows, inspectValueMembers(members, "batch"))
	}
	slices.SortFunc(report.InspectedWindows, func(a, b valueInspectedWindow) int { return a.First.Compare(b.First) })
	return nil
}

func inspectValueMembers(members []valueWindow, kind string) valueInspectedWindow {
	first, last := members[0], members[len(members)-1] //nolint:uncheckedsliceindex // Callers provide nonempty batches or a single witness.
	result := valueInspectedWindow{
		Label: first.MergedAt.UTC().Format("Jan 2"), Kind: kind, PRs: []int{},
		First: first.MergedAt, Last: last.MergedAt, Before: first.Before, After: last.After,
		End: last.End, Samples: append(slices.Clone(first.Samples[:7]), last.Samples[7:]...),
		Closed: []string{}, Removed: []string{}, Status: "descriptive", Reason: "batch_not_attributed",
	}
	for _, member := range members {
		result.PRs = append(result.PRs, member.PR)
		if coverage := member.Change.Coverage; coverage != nil {
			result.Closed = append(result.Closed, coverage.Closed...)
			result.Removed = append(result.Removed, coverage.Removed...)
		}
	}
	slices.Sort(result.Closed)
	slices.Sort(result.Removed)
	result.Closed, result.Removed = slices.Compact(result.Closed), slices.Compact(result.Removed)
	if kind == "single" {
		result.Attributed, result.Status, result.Reason = first.Attributed, first.Status, first.Reason
	}
	return result
}
