package model

import (
	"regexp"
	"testing"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewID(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		id := NewID()
		if !uuidV4.MatchString(id) {
			t.Fatalf("invalid UUIDv4 format: %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate ID generated: %q", id)
		}
		seen[id] = true
	}
}
