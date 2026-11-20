package lint

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/checkoutpolicy"
)

func UnpinnedImagesRule() Rule {
	return Rule{
		ID: "unpinned-images", Description: "Require job and service images to use immutable digests",
		Severity: Warning, Targets: ActionsAST | AWAST,
		Check: func(ctx context.Context, input Input) ([]Diagnostic, error) {
			return checkImages(ctx, input, false, unpinnedImage)
		},
	}
}

func unpinnedImage(raw any, path string, strategy map[string]any) ([]Diagnostic, error) {
	if raw == nil {
		return nil, nil
	}
	if fields, ok := raw.(map[string]any); ok {
		raw = fields["image"]
		path += ".image"
	}
	image, ok := raw.(string)
	if !ok {
		return []Diagnostic{{Path: path, Message: "Incomplete image pin analysis: image must be a string; use a literal image pinned to an immutable digest"}}, nil
	}
	if strings.Contains(image, "${{") {
		if alternatives, known := matrixImageAlternatives(image, strategy); known {
			for _, alternative := range alternatives {
				if _, ok := alternative.(string); !ok {
					return []Diagnostic{{Path: path, Message: "Incomplete image pin analysis: matrix image alternatives must be literal image strings pinned to immutable digests"}}, nil
				}
				if findings, err := unpinnedImage(alternative, path, nil); err != nil || len(findings) > 0 {
					return findings, err
				}
			}
			return nil, nil
		}
		return []Diagnostic{{Path: path, Message: "Incomplete image pin analysis: computed image cannot be verified; use a literal image pinned to an immutable digest"}}, nil
	}

	message, _ := integrityReference("docker://"+image, false)
	if message == "" {
		return nil, nil
	}
	return []Diagnostic{{Path: path, Message: "Job or service image is mutable or malformed; pin it to a valid image with an immutable sha256, sha384, or sha512 digest"}}, nil
}

func matrixImageAlternatives(image string, strategy map[string]any) ([]any, bool) {
	matrix, ok := strategy["matrix"].(map[string]any)
	if !ok {
		return nil, false
	}
	axes := maps.Clone(matrix)
	delete(axes, "include")
	delete(axes, "exclude")
	var includes []any
	if raw, exists := matrix["include"]; exists {
		var ok bool
		includes, ok = raw.([]any)
		if !ok {
			return nil, false
		}
	}
	for _, raw := range includes {
		if row, ok := raw.(map[string]any); ok {
			for key := range row {
				if _, exists := axes[key]; !exists {
					axes[key] = nil
				}
			}
		} else {
			return nil, false
		}
	}
	for _, key := range slices.Sorted(maps.Keys(axes)) {
		if !checkoutpolicy.MatchesExpression(image, "matrix."+key) {
			continue
		}
		var alternatives []any
		switch values := axes[key].(type) {
		case []any:
			alternatives = slices.Clone(values)
		case []string:
			for _, value := range values {
				alternatives = append(alternatives, value)
			}
		case nil:
		default:
			return nil, false
		}
		for _, raw := range includes {
			if row, ok := raw.(map[string]any); ok {
				if value, exists := row[key]; exists {
					alternatives = append(alternatives, value)
				}
			}
		}
		return alternatives, len(alternatives) > 0
	}
	return nil, false
}
