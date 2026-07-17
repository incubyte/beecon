package membraneimport

import "strings"

// deriveSlug derives a lower-kebab provider slug from the integration
// record's key, falling back to its name when the key yields nothing
// usable (Slice 1, AC: "derived from the integration key/name, lower-kebab,
// non-empty").
func deriveSlug(key, name string) string {
	if slug := kebab(key); slug != "" {
		return slug
	}
	return kebab(name)
}

// kebab lowercases s and joins its alphanumeric runs with single hyphens,
// dropping anything else (spaces, underscores, punctuation) and trimming
// leading/trailing hyphens.
func kebab(s string) string {
	var b strings.Builder
	lastWasHyphen := true // suppresses a leading hyphen
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasHyphen = false
		case !lastWasHyphen:
			b.WriteByte('-')
			lastWasHyphen = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
