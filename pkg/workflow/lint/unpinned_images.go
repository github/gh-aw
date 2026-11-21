package lint

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
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

func unpinnedImage(raw any, path string, strategy map[string]any, service bool) ([]Diagnostic, error) {
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
	if service && image == "" {
		return nil, nil
	}
	if strings.Contains(image, "${{") {
		if alternatives, known := matrixImageAlternatives(image, strategy); known {
			for _, alternative := range alternatives {
				if _, ok := alternative.(string); !ok {
					return []Diagnostic{{Path: path, Message: "Incomplete image pin analysis: matrix image alternatives must be literal image strings pinned to immutable digests"}}, nil
				}
				if findings, err := unpinnedImage(alternative, path, nil, service); err != nil || len(findings) > 0 {
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
	spans, err := actionslanguageservice.ExtractExpressions(image)
	if err != nil || len(spans) == 0 {
		return nil, false
	}
	matrix, ok := strategy["matrix"].(map[string]any)
	if !ok {
		return nil, false
	}
	axes := maps.Clone(matrix)
	delete(axes, "include")
	delete(axes, "exclude")
	includes, includesKnown := matrixImageRows(matrix["include"])
	excludes, excludesKnown := matrixImageRows(matrix["exclude"])
	if !includesKnown || !excludesKnown {
		return nil, false
	}
	for _, row := range includes {
		for key := range row {
			if _, exists := axes[key]; !exists {
				axes[key] = nil
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(axes)) {
		if slices.ContainsFunc(spans, func(span actionslanguageservice.Interpolation) bool {
			return !checkoutpolicy.MatchesExpression(image[span.Start:span.End], "matrix."+key)
		}) {
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
		alternatives = slices.DeleteFunc(alternatives, func(value any) bool {
			return slices.ContainsFunc(excludes, func(row map[string]any) bool {
				excluded, exists := row[key]
				return len(row) == 1 && exists && matrixImageScalarEqual(excluded, value)
			})
		})
		for _, row := range includes {
			if value, exists := row[key]; exists {
				alternatives = append(alternatives, value)
			}
		}
		return matrixImageSubstitute(image, spans, alternatives)
	}
	return nil, false
}

func matrixImageScalarEqual(left, right any) bool {
	switch left.(type) {
	case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return left == right
	default:
		return false
	}
}

func matrixImageSubstitute(image string, spans []actionslanguageservice.Interpolation, alternatives []any) ([]any, bool) {
	first, found := sliceAt(spans, 0)
	if found && len(spans) == 1 && strings.TrimSpace(image) == image[first.Start:first.End] {
		return alternatives, len(alternatives) > 0
	}
	for index, alternative := range alternatives {
		var replacement string
		switch value := alternative.(type) {
		case string:
			if strings.Contains(value, "${{") {
				return nil, false
			}
			replacement = value
		case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			replacement = fmt.Sprint(value)
		default:
			return nil, false
		}
		var resolved strings.Builder
		offset := 0
		for _, span := range spans {
			resolved.WriteString(image[offset:span.Start])
			resolved.WriteString(replacement)
			offset = span.End
		}
		resolved.WriteString(image[offset:])
		alternatives[index] = resolved.String()
	}
	return alternatives, len(alternatives) > 0
}

func sliceAt[T any](values []T, index int) (T, bool) {
	if index >= 0 && index < len(values) {
		return values[index], true
	}
	var zero T
	return zero, false
}

func matrixImageRows(raw any) ([]map[string]any, bool) {
	if raw == nil {
		return nil, true
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	var rows []map[string]any
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		rows = append(rows, row)
	}
	return rows, true
}
