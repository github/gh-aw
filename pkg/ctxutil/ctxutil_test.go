package ctxutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrBackground(t *testing.T) {
	t.Parallel()
	type key string
	const testKey key = "test-key"

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "nil context returns background",
			ctx:  nil,
		},
		{
			name: "non-nil context returned unchanged",
			ctx:  context.WithValue(context.Background(), testKey, "value"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OrBackground(tt.ctx)
			require.NotNil(t, got, "OrBackground should never return a nil context")
			if tt.ctx == nil {
				assert.Equal(t, context.Background(), got, "OrBackground should fall back to context.Background() for a nil input")
			} else {
				assert.Same(t, tt.ctx, got, "OrBackground should return the original non-nil context unchanged")
				assert.Equal(t, "value", got.Value(testKey), "OrBackground should preserve values carried by the input context")
			}
		})
	}
}
