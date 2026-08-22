package web

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jesus/invoice-app/internal/i18n"
)

// formatReais renders cents as a plain decimal string suitable for
// <input type="text"> values: 123456 -> "1234,56" (no thousands separator).
func formatReais(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d,%02d", sign, cents/100, cents%100)
}

// parseReais accepts pt-BR ("1.234,56") and US-style ("1,234.56") decimal
// input, an optional R$ prefix, and returns whole cents. Error text is
// localized for form banners.
func parseReais(lang i18n.Lang, s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.ToUpper(s), "R$")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("%s", i18n.T(lang, "error.money_empty"))
	}

	hasComma := strings.Contains(s, ",")
	hasDot := strings.Contains(s, ".")
	switch {
	case hasComma && hasDot:
		if strings.LastIndex(s, ",") > strings.LastIndex(s, ".") {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.ReplaceAll(s, ",", ".")
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
	case hasComma:
		s = strings.ReplaceAll(s, ",", ".")
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s", i18n.T(lang, "error.money_invalid", s))
	}
	if math.Abs(v) >= 1e10 {
		return 0, fmt.Errorf("%s", i18n.T(lang, "error.money_range"))
	}
	cents := int64(math.Round(v * 100))
	if cents == 0 && v != 0 {
		return 0, fmt.Errorf("%s", i18n.T(lang, "error.money_too_small"))
	}
	return cents, nil
}
