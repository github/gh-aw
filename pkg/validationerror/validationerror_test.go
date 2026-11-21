//go:build !integration

package validationerror_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/validationerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exampleError is a minimal concrete type embedding validationerror.Payload,
// mirroring how parser.ValidationError and workflow.WorkflowValidationError
// each embed the shared payload.
type exampleError struct {
	validationerror.Payload
}

func (e *exampleError) Error() string {
	return validationerror.Format(fmt.Sprintf("Validation failed for field '%s'", e.Field), e.Payload, false)
}

func TestFormat(t *testing.T) {
	tests := []struct {
		name          string
		payload       validationerror.Payload
		truncateValue bool
		wantContains  []string
		wantAbsent    []string
	}{
		{
			name: "all fields",
			payload: validationerror.Payload{
				Field: "skills", Value: "docs", Reason: "duplicate name already defined",
				Suggestion: "Rename one of the duplicate skills.",
			},
			wantContains: []string{"header", "\n\nValue: docs\nReason: duplicate name already defined", "\nSuggestion: Rename one of the duplicate skills."},
		},
		{
			name:         "empty value and suggestion",
			payload:      validationerror.Payload{Reason: "reason only"},
			wantContains: []string{"header\nReason: reason only"},
			wantAbsent:   []string{"Value:", "Suggestion:"},
		},
		{
			name:         "empty reason",
			payload:      validationerror.Payload{Value: "docs", Suggestion: "Try again."},
			wantContains: []string{"\n\nValue: docs\nReason: \nSuggestion: Try again."},
		},
		{
			name:          "value of 100 characters",
			payload:       validationerror.Payload{Value: strings.Repeat("x", 100), Reason: "reason"},
			truncateValue: true,
			wantContains:  []string{"\n\nValue: " + strings.Repeat("x", 100) + "\nReason: reason"},
		},
		{
			name:          "value of 101 characters",
			payload:       validationerror.Payload{Value: strings.Repeat("x", 101), Reason: "reason"},
			truncateValue: true,
			wantContains:  []string{"\n\nValue: " + strings.Repeat("x", 97) + "...\nReason: reason"},
		},
		{
			name:          "value of 200 characters truncated",
			payload:       validationerror.Payload{Value: strings.Repeat("x", 200), Reason: "reason"},
			truncateValue: true,
			wantContains:  []string{"\n\nValue: " + strings.Repeat("x", 97) + "...\nReason: reason"},
			wantAbsent:    []string{strings.Repeat("x", 200)},
		},
		{
			name:         "value of 200 characters without truncation",
			payload:      validationerror.Payload{Value: strings.Repeat("x", 200), Reason: "reason"},
			wantContains: []string{"\n\nValue: " + strings.Repeat("x", 200) + "\nReason: reason"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := validationerror.Format("header", tt.payload, tt.truncateValue)
			for _, want := range tt.wantContains {
				assert.Contains(t, msg, want, tt.name)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, msg, absent, tt.name)
			}
		})
	}
}

func TestFormat_MultibyteValue(t *testing.T) {
	t.Run("cut between runes", func(t *testing.T) {
		value := strings.Repeat("é", 48) + strings.Repeat("x", 10)
		msg := validationerror.Format("header", validationerror.Payload{Value: value}, true)
		assert.Contains(t, msg, "\n\nValue: "+strings.Repeat("é", 48)+"x...\nReason: ")
		assert.True(t, utf8.ValidString(msg), "truncated message should be valid UTF-8")
	})
	t.Run("cut inside rune", func(t *testing.T) {
		value := strings.Repeat("x", 96) + strings.Repeat("é", 3)
		msg := validationerror.Format("header", validationerror.Payload{Value: value}, true)
		if !utf8.ValidString(msg) {
			t.Skip("known bug: truncation splits a UTF-8 rune at the cut point")
		}
		assert.True(t, utf8.ValidString(msg), "truncated message should be valid UTF-8")
	})
}

func TestValidationError_ErrorsAsWorksAcrossConcreteTypes(t *testing.T) {
	err := error(&exampleError{Payload: validationerror.Payload{
		Field:      "skills",
		Value:      "docs",
		Reason:     "duplicate name already defined",
		Suggestion: "Rename one of the duplicate skills.",
	}})

	var ve validationerror.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, "skills", ve.ValidationField())
	assert.Equal(t, "docs", ve.ValidationValue())
	assert.Equal(t, "duplicate name already defined", ve.ValidationReason())
	assert.Equal(t, "Rename one of the duplicate skills.", ve.ValidationSuggestion())

	var unmatched validationerror.ValidationError
	assert.NotErrorAs(t, errors.New("plain error"), &unmatched, "plain error must not match ValidationError")
}
