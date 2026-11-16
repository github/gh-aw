//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdoptionConformance(t *testing.T) {
	var cases []struct {
		Name      string                     `json:"name"`
		Item      CreatedItemReport          `json:"item"`
		Responses map[string]json.RawMessage `json:"responses"`
		Errors    map[string]int             `json:"errors"`
		Expected  AdoptionEvaluation         `json:"expected"`
		ZeroTouch *bool                      `json:"zero_touch"`
	}

	content, err := os.ReadFile("testdata/adoption_conformance.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, &cases))
	oldGet, oldArray := adoptionEvidenceGHAPIGet, adoptionEvidenceGHAPIGetArray
	oldPR, oldPRArray := adoptionEvalPRGHAPIGet, adoptionEvalPRGHAPIGetArray
	oldClose, oldCloseArray := closeStickyGHAPIGet, closeStickyGHAPIGetArray
	oldUpdate, oldReview, oldReviewArray := adoptionUpdateGHAPIGet, adoptionReviewGHAPIGet, adoptionReviewGHAPIGetArray
	oldGeneric, oldWorkflow := genericAdoptionGHAPIGet, workflowAdoptionGHAPIGet
	t.Cleanup(func() {
		adoptionEvidenceGHAPIGet, adoptionEvidenceGHAPIGetArray = oldGet, oldArray
		adoptionEvalPRGHAPIGet, adoptionEvalPRGHAPIGetArray = oldPR, oldPRArray
		closeStickyGHAPIGet, closeStickyGHAPIGetArray = oldClose, oldCloseArray
		adoptionUpdateGHAPIGet, adoptionReviewGHAPIGet, adoptionReviewGHAPIGetArray = oldUpdate, oldReview, oldReviewArray
		genericAdoptionGHAPIGet, workflowAdoptionGHAPIGet = oldGeneric, oldWorkflow
	})
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			read := func(endpoint string, target any) error {
				if status := tc.Errors[endpoint]; status != 0 {
					return fmt.Errorf("HTTP %d Not Found", status)
				}
				raw, ok := tc.Responses[endpoint]
				if !ok {
					return fmt.Errorf("unavailable fixture endpoint %s", endpoint)
				}
				return json.Unmarshal(raw, target)
			}
			get := func(_ context.Context, endpoint, repo string) (map[string]any, error) {
				if repo != "acme/repo" {
					endpoint = "repos/" + repo + "/" + endpoint
				}
				var value map[string]any
				err := read(endpoint, &value)
				return value, err
			}
			array := func(_ context.Context, endpoint, _ string) ([]map[string]any, error) {
				var value []map[string]any
				err := read(endpoint, &value)
				return value, err
			}
			adoptionEvidenceGHAPIGet, adoptionEvidenceGHAPIGetArray = get, array
			adoptionEvalPRGHAPIGet, adoptionEvalPRGHAPIGetArray = get, array
			closeStickyGHAPIGet, closeStickyGHAPIGetArray = get, array
			adoptionUpdateGHAPIGet, adoptionReviewGHAPIGet, adoptionReviewGHAPIGetArray = get, get, array
			genericAdoptionGHAPIGet, workflowAdoptionGHAPIGet = get, get
			evaluator := adoptionEvaluators[tc.Item.Type]
			if evaluator == nil {
				evaluator = evalGenericSticky
			}
			report := evaluator(context.Background(), tc.Item, "acme/repo")
			require.Equal(t, tc.Expected, normalizeAdoptionEvaluation(report))
			if tc.ZeroTouch != nil {
				require.Equal(t, *tc.ZeroTouch, report.ZeroTouch)
			}
		})
	}
}

func TestAdoptionAPIArrayPagination(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in\n*--paginate*--slurp*) printf '%s' '[[{\"id\":1}],[{\"id\":2}]]' ;;\n*) exit 1 ;;\nesac\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	items, err := ghAPIGetArray(context.Background(), "issues/1/comments", "acme/repo")
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.InDelta(t, 1, items[0]["id"], 0)
	require.InDelta(t, 2, items[1]["id"], 0)
}
