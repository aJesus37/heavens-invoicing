// Package whatsapp manages the WhatsApp session used to deliver invoices
// and reminders, backed by the whatsmeow library.
package whatsapp

import (
	"fmt"
	"strings"
)

// NormalizePhone strips spaces, dashes and parentheses from an E.164 phone
// number and validates its shape: it must start with a +, contain only
// digits afterwards, and have between 10 and 15 digits (country code
// included). It returns the number without the leading +, which is the
// user part of a WhatsApp JID.
func NormalizePhone(phone string) (string, error) {
	trimmed := strings.TrimSpace(phone)
	if trimmed == "" {
		return "", fmt.Errorf("invalid phone number %q: empty", phone)
	}
	if !strings.HasPrefix(trimmed, "+") {
		return "", fmt.Errorf("invalid phone number %q: must start with + followed by the country code", phone)
	}

	var b strings.Builder
	for _, r := range trimmed[1:] {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
			// separators are ignored
		default:
			return "", fmt.Errorf("invalid phone number %q: contains invalid characters", phone)
		}
	}

	digits := b.String()
	if len(digits) < 10 {
		return "", fmt.Errorf("invalid phone number %q: too short", phone)
	}
	if len(digits) > 15 {
		return "", fmt.Errorf("invalid phone number %q: too long", phone)
	}
	return digits, nil
}
