package pdf

import "testing"

func TestFormatBRL(t *testing.T) {
	tests := []struct {
		name  string
		cents int64
		want  string
	}{
		{"typical", 123456, "R$ 1.234,56"},
		{"five cents", 5, "R$ 0,05"},
		{"zero", 0, "R$ 0,00"},
		{"negative", -150, "-R$ 1,50"},
		{"large", 123456789, "R$ 1.234.567,89"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBRL(tt.cents); got != tt.want {
				t.Errorf("FormatBRL(%d) = %q, want %q", tt.cents, got, tt.want)
			}
		})
	}
}
