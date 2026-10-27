package actionslanguageservice

import (
	"regexp"
	"testing"
)

func TestActionsIdentifier(t *testing.T) {
	tests := []struct {
		value       string
		plain       bool
		allowHyphen bool
	}{
		{"", false, false},
		{"_", true, true},
		{"aZ_09", true, true},
		{"9name", false, false},
		{"-name", false, false},
		{"a-b", false, true},
		{"a-", false, true},
		{"_--0", false, true},
		{"a.b", false, false},
		{"a b", false, false},
		{" a", false, false},
		{"a\n", false, false},
		{"é", false, false},
		{"aé", false, false},
		{"a９", false, false},
		{"a\xff", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			for _, allowHyphen := range []bool{false, true} {
				want := tt.plain
				if allowHyphen {
					want = tt.allowHyphen
				}
				if got := ActionsIdentifier(tt.value, allowHyphen); got != want {
					t.Errorf("ActionsIdentifier(%q, %t) = %t; want %t", tt.value, allowHyphen, got, want)
				}
			}
		})
	}
}

func TestActionsIdentifierByteAcceptance(t *testing.T) {
	plain := regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*$`)
	hyphen := regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9-]*$`)
	for ch := range 256 {
		for _, value := range []string{string([]byte{byte(ch)}), string([]byte{byte(ch), 'a'}), "a" + string([]byte{byte(ch)})} {
			for _, allowHyphen := range []bool{false, true} {
				pattern := plain
				if allowHyphen {
					pattern = hyphen
				}
				if got, want := ActionsIdentifier(value, allowHyphen), pattern.MatchString(value); got != want {
					t.Errorf("ActionsIdentifier(%q, %t) = %t; want %t", value, allowHyphen, got, want)
				}
			}
		}
	}
}
