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

// countFormatVerbs counts the fmt formatting verbs in s, ignoring the
// literal "%%". A drift between catalogs (e.g. an added %s / dropped %d)
// would otherwise render "%!s(MISSING)" at runtime, so parity must cover
// verb counts, not just key/value presence.
func countFormatVerbs(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+1 < len(s) && s[i+1] == '%' {
				i++ // literal percent, not a verb
				continue
			}
			n++
		}
	}
	return n
}

// TestCatalogParity fails listing every key that drifted between the two
// catalogs; en and pt-BR must cover identical key sets and, for every key,
// the same count of fmt formatting verbs.
func TestCatalogParity(t *testing.T) {
	en := catalogs[En]
	pt := catalogs[PtBR]

	var missingInEn, missingInPt, verbDrift []string
	for _, k := range keysOf(pt) {
		ev, ok := en[k]
		if !ok {
			missingInEn = append(missingInEn, k)
			continue
		}
		if countFormatVerbs(ev) != countFormatVerbs(pt[k]) {
			verbDrift = append(verbDrift, k)
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
	if len(verbDrift) > 0 {
		t.Fatalf("locale catalogs drifted in fmt verb counts (key -> en vs pt-BR verbs):\n  %v", verbDrift)
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
		"en":      En,
		"pt-BR":   PtBR,
		"  en  ":  En,
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
