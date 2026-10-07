//go:build !integration

package console

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewListItem(t *testing.T) {
	t.Parallel()
	item := NewListItem("Title", "Description", "value")

	assert.Equal(t, "Title", item.title)
	assert.Equal(t, "Description", item.description)
}

func TestShowInteractiveList_EmptyItems(t *testing.T) {
	t.Parallel()
	items := []ListItem{}
	_, err := ShowInteractiveList("Test", items)
	require.Error(t, err)
	require.ErrorContains(t, err, "no items to display")
}

func TestShowInteractiveList_NonTTYStdin(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { os.Stdin = oldStdin })
	t.Cleanup(func() { r.Close() })
	t.Cleanup(func() { w.Close() })
	os.Stdin = r
	_, err = w.WriteString("1\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, err = ShowInteractiveList("Choose", []ListItem{NewListItem("One", "", "one")})
	require.Error(t, err)
	require.ErrorContains(t, err, "stdin is not a TTY")
}

// Note: Full interactive list testing requires TTY and cannot be automated.
// Manual testing should be performed to verify:
// - Arrow key navigation works
// - Selection with Enter key
// - Quit with Esc/Ctrl+C
// - Non-TTY fallback to text list
