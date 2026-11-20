//go:build !integration

package validationerror_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/validationerror"
)

// TestSpec_Types_PayloadAndValidationError validates the documented Payload
// fields, accessors, and ValidationError interface.
func TestSpec_Types_PayloadAndValidationError(t *testing.T) {
	payload := validationerror.Payload{
		Field:      "engine",
		Value:      "unknown",
		Reason:     "unsupported engine",
		Suggestion: "Use a supported engine.",
	}

	if got := payload.ValidationField(); got != payload.Field {
		t.Errorf("ValidationField() = %q, want %q", got, payload.Field)
	}
	if got := payload.ValidationValue(); got != payload.Value {
		t.Errorf("ValidationValue() = %q, want %q", got, payload.Value)
	}
	if got := payload.ValidationReason(); got != payload.Reason {
		t.Errorf("ValidationReason() = %q, want %q", got, payload.Reason)
	}
	if got := payload.ValidationSuggestion(); got != payload.Suggestion {
		t.Errorf("ValidationSuggestion() = %q, want %q", got, payload.Suggestion)
	}

	var target validationerror.ValidationError
	if !errors.As(error(&exampleError{Payload: payload}), &target) {
		t.Fatal("Payload embedded in an error should implement ValidationError")
	}
}

// TestSpec_Constants_MaxValueLength validates the documented truncation limit.
func TestSpec_Constants_MaxValueLength(t *testing.T) {
	if validationerror.MaxValueLength != 100 {
		t.Errorf("MaxValueLength = %d, want 100", validationerror.MaxValueLength)
	}
}

// TestSpec_PublicAPI_Format validates the documented message shape, optional
// sections, and value truncation.
func TestSpec_PublicAPI_Format(t *testing.T) {
	t.Run("formats all payload fields", func(t *testing.T) {
		got := validationerror.Format("validation failed", validationerror.Payload{
			Value:      "unknown",
			Reason:     "unsupported engine",
			Suggestion: "Use a supported engine.",
		}, false)
		want := "validation failed\n\nValue: unknown\nReason: unsupported engine\nSuggestion: Use a supported engine."
		if got != want {
			t.Errorf("Format() = %q, want %q", got, want)
		}
	})

	t.Run("omits empty optional fields", func(t *testing.T) {
		got := validationerror.Format("validation failed", validationerror.Payload{Reason: "missing value"}, false)
		want := "validation failed\nReason: missing value"
		if got != want {
			t.Errorf("Format() = %q, want %q", got, want)
		}
	})

	t.Run("truncates values to MaxValueLength", func(t *testing.T) {
		got := validationerror.Format("validation failed", validationerror.Payload{
			Value:  strings.Repeat("x", validationerror.MaxValueLength+1),
			Reason: "too long",
		}, true)
		wantValue := strings.Repeat("x", validationerror.MaxValueLength-3) + "..."
		want := fmt.Sprintf("validation failed\n\nValue: %s\nReason: too long", wantValue)
		if got != want {
			t.Errorf("Format() = %q, want %q", got, want)
		}
	})
}
