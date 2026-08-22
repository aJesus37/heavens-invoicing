package i18n

import (
	"slices"
	"testing"
)

func keysOf(c catalog) []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// TestCatalogParity fails listing every key that drifted between the two
// catalogs; en and pt-BR must cover identical key sets.
func TestCatalogParity(t *testing.T) {
	en := catalogs[En]
	pt := catalogs[PtBR]

	var missingInEn, missingInPt []string
	for _, k := range keysOf(pt) {
		if _, ok := en[k]; !ok {
			missingInEn = append(missingInEn, k)
		}
	}
	for _, k := range keysOf(en) {
		if _, ok := pt[k]; !ok {
			missingInPt = append(missingInPt, k)
		}
	}
	if len(missingInEn)+len(missingInPt) > 0 {
		t.Fatalf("locale catalogs drifted:\n  missing in en.json: %v\n  missing in pt-BR.json: %v",
			missingInEn, missingInPt)
	}
}

func TestTFallbackChain(t *testing.T) {
	tests := []struct {
		lang Lang
		want string
	}{
		{PtBR, "Clientes"},
		{En, "Clients"},
		// Unsupported and empty langs fall through to the English catalog.
		{Lang("es"), "Clients"},
		{Lang(""), "Clients"},
	}
	for _, tc := range tests {
		if got := T(tc.lang, "nav.clients"); got != tc.want {
			t.Errorf("T(%q, nav.clients) = %q, want %q", tc.lang, got, tc.want)
		}
	}
}

func TestTMissingKeyIsLoud(t *testing.T) {
	for _, lang := range []Lang{PtBR, En, Lang("es"), Lang("")} {
		if got := T(lang, "no.such.key"); got != "!no.such.key" {
			t.Errorf("T(%q, missing) = %q, want loud marker", lang, got)
		}
	}
}

func TestTFormatting(t *testing.T) {
	if got, want := T(En, "detail.title", int64(42)), "Invoice #000042"; got != want {
		t.Errorf("en detail.title = %q, want %q", got, want)
	}
	if got, want := T(PtBR, "detail.title", int64(42)), "Fatura #000042"; got != want {
		t.Errorf("pt-BR detail.title = %q, want %q", got, want)
	}
	if got, want := T(PtBR, "error.money_invalid", "abc"), `valor inválido "abc"`; got != want {
		t.Errorf("pt-BR money_invalid = %q, want %q", got, want)
	}
}

func TestParse(t *testing.T) {
	for s, want := range map[string]Lang{
		"en":       En,
		"pt-BR":    PtBR,
		"  en  ":   En,
		"\tpt-BR": PtBR,
	} {
		got, ok := Parse(s)
		if !ok || got != want {
			t.Errorf("Parse(%q) = (%q, %v), want (%q, true)", s, got, ok, want)
		}
	}
	for _, s := range []string{"", " ", "pt_br", "en-US", "Português"} {
		if got, ok := Parse(s); ok {
			t.Errorf("Parse(%q) = (%q, true), want rejected", s, got)
		}
	}
}
