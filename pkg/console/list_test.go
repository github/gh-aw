//go:build !integration

package console

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewListItem(t *testing.T) {
	item := NewListItem("Title", "Description", "value")

	assert.Equal(t, "Title", item.Title())
	assert.Equal(t, "Description", item.Description())
	assert.Equal(t, "Title", item.FilterValue()) // FilterValue returns title for searching
}

func TestListItem_Title(t *testing.T) {
	item := ListItem{title: "My Title", description: "desc", value: "val"}
	assert.Equal(t, "My Title", item.Title())
}

func TestListItem_Description(t *testing.T) {
	item := ListItem{title: "title", description: "My Description", value: "val"}
	assert.Equal(t, "My Description", item.Description())
}

func TestListItem_FilterValue(t *testing.T) {
	item := ListItem{title: "Searchable Title", description: "desc", value: "val"}
	// FilterValue should return title for searching
	assert.Equal(t, "Searchable Title", item.FilterValue())
}

func TestItemDelegate_Height(t *testing.T) {
	delegate := itemDelegate{}
	assert.Equal(t, 1, delegate.Height())
}

func TestItemDelegate_Spacing(t *testing.T) {
	delegate := itemDelegate{}
	assert.Equal(t, 0, delegate.Spacing())
}

func TestShowInteractiveList_EmptyItems(t *testing.T) {
	items := []ListItem{}
	_, err := ShowInteractiveList("Test", items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no items to display")
}

// Note: Full interactive list testing requires TTY and cannot be automated
// Manual testing should be performed to verify:
// - Arrow key navigation works
// - Search/filter functionality
// - Selection with Enter key
// - Quit with Esc/Ctrl+C
// - Non-TTY fallback to text list
