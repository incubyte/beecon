package idgen_test

import (
	"regexp"
	"testing"

	"beecon/internal/idgen"
)

// cuid2Shape matches the default CUID2 output: lowercase alphanumeric,
// starting with a letter (github.com/akshayvadher/cuid2 default length 24).
var cuid2Shape = regexp.MustCompile(`^[a-z][a-z0-9]{23}$`)

func TestPrefixed_AppliesThePrefixToEveryID(t *testing.T) {
	newID := idgen.Prefixed("org_")

	id := newID()

	if len(id) < len("org_") || id[:len("org_")] != "org_" {
		t.Fatalf("id = %q, want it to start with %q", id, "org_")
	}
}

func TestPrefixed_SuffixIsCUID2Shaped(t *testing.T) {
	newID := idgen.Prefixed("user_")

	id := newID()
	suffix := id[len("user_"):]

	if !cuid2Shape.MatchString(suffix) {
		t.Errorf("id suffix = %q, want it to match CUID2 shape %s", suffix, cuid2Shape.String())
	}
}

func TestPrefixed_GeneratesUniqueIDsAcrossCalls(t *testing.T) {
	newID := idgen.Prefixed("conn_")

	const n = 1000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := newID()
		if seen[id] {
			t.Fatalf("duplicate id generated: %q", id)
		}
		seen[id] = true
	}
}

func TestPrefixed_DifferentPrefixesProduceDifferentIDs(t *testing.T) {
	orgID := idgen.Prefixed("org_")()
	userID := idgen.Prefixed("user_")()

	if orgID == userID {
		t.Fatalf("expected different prefixes to produce different ids, both were %q", orgID)
	}
	if orgID[:4] != "org_" {
		t.Errorf("orgID = %q, want prefix %q", orgID, "org_")
	}
	if userID[:5] != "user_" {
		t.Errorf("userID = %q, want prefix %q", userID, "user_")
	}
}
