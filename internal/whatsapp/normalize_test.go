package whatsapp_test

import (
	"testing"

	"github.com/jesus/invoice-app/internal/whatsapp"
)

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"plain E.164", "+5511999999999", "5511999999999", false},
		{"Brazilian with spaces and dash", "+55 11 99999-9999", "5511999999999", false},
		{"US with parens", "+1 (415) 555-2671", "14155552671", false},
		{"tabs around", "\t+5511999999999\n", "5511999999999", false},

		{"no leading +", "5511999999999", "", true},
		{"letters", "+55 11 abcde-aaaa", "", true},
		{"too short", "+12345", "", true},
		{"too long", "+551199999999912345", "", true},
		{"empty", "", "", true},
		{"only spaces", "   ", "", true},
		{"only plus", "+", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := whatsapp.NormalizePhone(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizePhone(%q): want error, got %q", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePhone(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizePhone(%q): want %q, got %q", tc.input, tc.want, got)
			}
		})
	}
}
