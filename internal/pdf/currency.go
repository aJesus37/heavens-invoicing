package pdf

import (
	"strconv"
	"strings"
)

// FormatBRL renders an amount in cents as a pt-BR currency string,
// e.g. 123456 -> "R$ 1.234,56". Negative amounts get the minus sign
// before the R$ prefix.
func FormatBRL(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	reais := cents / 100
	frac := cents % 100
	digits := strconv.FormatInt(reais, 10)
	var b strings.Builder
	b.WriteString(sign)
	b.WriteString("R$ ")
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(d)
	}
	b.WriteByte(',')
	if frac < 10 {
		b.WriteByte('0')
	}
	b.WriteString(strconv.FormatInt(frac, 10))
	return b.String()
}
