//go:build !integration

package campaign

import "testing"

func TestEnsureSingleSelectOptionBefore(t *testing.T) {
	tests := []struct {
		name    string
		options []singleSelectOption
		want    []singleSelectOption
		changed bool
	}{
		{
			name: "inserts before Done when missing",
			options: []singleSelectOption{
				{Name: "Todo", Color: "GRAY", Description: ""},
				{Name: "In Progress", Color: "BLUE", Description: ""},
				{Name: "Done", Color: "GREEN", Description: ""},
			},
			want: []singleSelectOption{
				{Name: "Todo", Color: "GRAY", Description: ""},
				{Name: "In Progress", Color: "BLUE", Description: ""},
				{Name: "Review Required", Color: "BLUE", Description: "Needs review before moving to Done"},
				{Name: "Done", Color: "GREEN", Description: ""},
			},
			changed: true,
		},
		{
			name: "moves existing option before Done",
			options: []singleSelectOption{
				{Name: "Todo", Color: "GRAY", Description: ""},
				{Name: "Done", Color: "GREEN", Description: ""},
				{Name: "Review Required", Color: "PINK", Description: "keep"},
			},
			want: []singleSelectOption{
				{Name: "Todo", Color: "GRAY", Description: ""},
				{Name: "Review Required", Color: "BLUE", Description: "Needs review before moving to Done"},
				{Name: "Done", Color: "GREEN", Description: ""},
			},
			changed: true,
		},
		{
			name: "no change when already before Done",
			options: []singleSelectOption{
				{Name: "Todo", Color: "GRAY", Description: ""},
				{Name: "Review Required", Color: "BLUE", Description: "Needs review before moving to Done"},
				{Name: "Done", Color: "GREEN", Description: ""},
			},
			want: []singleSelectOption{
				{Name: "Todo", Color: "GRAY", Description: ""},
				{Name: "Review Required", Color: "BLUE", Description: "Needs review before moving to Done"},
				{Name: "Done", Color: "GREEN", Description: ""},
			},
			changed: false,
		},
		{
			name: "appends when Done missing",
			options: []singleSelectOption{
				{Name: "Todo", Color: "GRAY", Description: ""},
			},
			want: []singleSelectOption{
				{Name: "Todo", Color: "GRAY", Description: ""},
				{Name: "Review Required", Color: "BLUE", Description: "Needs review before moving to Done"},
			},
			changed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := ensureSingleSelectOptionBefore(
				tt.options,
				singleSelectOption{Name: "Review Required", Color: "BLUE", Description: "Needs review before moving to Done"},
				"Done",
			)

			if changed != tt.changed {
				t.Fatalf("changed=%v, want %v", changed, tt.changed)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got[%d]=%+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
