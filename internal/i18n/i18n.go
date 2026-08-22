// Package i18n holds the embedded translation catalogs and the lookup
// used across the web UI. Catalogs are flat JSON maps with dotted,
// namespace-style keys ("nav.clients"). Lookup order is requested
// locale → English → a loud "!key" marker, so a missing translation fails
// visibly during development instead of silently rendering blanks.
package i18n

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Lang identifies a supported UI locale.
type Lang string

// Supported locales. English doubles as the fallback catalog.
const (
	En   Lang = "en"
	PtBR Lang = "pt-BR"
)

//go:embed locales/en.json
var enJSON []byte

//go:embed locales/pt-BR.json
var ptBRJSON []byte

type catalog map[string]string

var catalogs = map[Lang]catalog{
	En:   mustLoad(enJSON),
	PtBR: mustLoad(ptBRJSON),
}

func mustLoad(raw []byte) catalog {
	c := catalog{}
	if err := json.Unmarshal(raw, &c); err != nil {
		panic(fmt.Sprintf("i18n: bad locale catalog: %v", err))
	}
	return c
}

// Parse validates a stored locale string, trimming surrounding
// whitespace. Anything outside the supported set fails.
func Parse(s string) (Lang, bool) {
	switch Lang(strings.TrimSpace(s)) {
	case En:
		return En, true
	case PtBR:
		return PtBR, true
	default:
		return "", false
	}
}

// Normalize validates a client-supplied language preference for storage.
// A blank value defaults to pt-BR so legacy callers keep the historical
// behavior; any other unsupported value fails.
func Normalize(s string) (Lang, bool) {
	if strings.TrimSpace(s) == "" {
		return PtBR, true
	}
	return Parse(s)
}

// Resolve maps a stored language value onto a supported Lang, falling
// back to pt-BR when it is empty or unknown. Read paths (PDF rendering,
// delivery messages) use it so legacy rows keep rendering in Portuguese;
// write paths validate with Parse/Normalize instead of coercing.
func Resolve(s string) Lang {
	if l, ok := Parse(s); ok {
		return l
	}
	return PtBR
}

// ResolveSettings is the single settings-backed locale resolver: it parses a
// stored localePreference and defaults to pt-BR when unset or unknown. The
// scheduler, delivery router, admin bot and web all funnel through it so the
// Parse+fallback logic lives in exactly one place.
func ResolveSettings(s string) Lang {
	return Resolve(s)
}

// T translates key for lang, applying fmt formatting when args are given.
// Resolution order: lang → En → "!"+key so missing entries stay loud.
func T(lang Lang, key string, args ...any) string {
	if v, ok := lookup(lang, key); ok {
		return format(v, args)
	}
	if v, ok := lookup(En, key); ok {
		return format(v, args)
	}
	return "!" + key
}

func lookup(lang Lang, key string) (string, bool) {
	c, ok := catalogs[lang]
	if !ok {
		return "", false
	}
	v, ok := c[key]
	return v, ok
}

func format(v string, args []any) string {
	if len(args) == 0 {
		return v
	}
	return fmt.Sprintf(v, args...)
}
