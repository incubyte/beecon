package registryservice

import (
	"fmt"
	"strconv"
	"strings"
)

// semverParts is a parsed major.minor.patch version (PD62's bump-direction
// enforcement needs to compare and combine version numbers, not just treat
// them as opaque strings).
type semverParts struct {
	major, minor, patch int
}

// parseSemver parses version as a strict major.minor.patch triple — no
// pre-release/build-metadata suffixes, since that is the only shape this
// registry's own Publish ever assigns or accepts.
func parseSemver(version string) (semverParts, error) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) != 3 {
		return semverParts{}, fmt.Errorf("must be a major.minor.patch version, got %q", version)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semverParts{}, fmt.Errorf("invalid major version number in %q", version)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semverParts{}, fmt.Errorf("invalid minor version number in %q", version)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semverParts{}, fmt.Errorf("invalid patch version number in %q", version)
	}
	return semverParts{major: major, minor: minor, patch: patch}, nil
}

func (v semverParts) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

// compare returns -1, 0, or 1 as v is less than, equal to, or greater than
// other.
func (v semverParts) compare(other semverParts) int {
	if v.major != other.major {
		return signOf(v.major - other.major)
	}
	if v.minor != other.minor {
		return signOf(v.minor - other.minor)
	}
	return signOf(v.patch - other.patch)
}

func signOf(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// autoNextVersion computes the version a publish gets when the caller
// leaves the version field empty: a removal bumps major, an addition (with
// no removal) bumps minor, and a content-only change (neither) bumps patch —
// PD62's bump-direction rule, applied automatically rather than enforced
// against a caller-supplied number.
func autoNextVersion(prior semverParts, diff bundleDiff) semverParts {
	switch {
	case diff.hasRemovals():
		return semverParts{major: prior.major + 1}
	case diff.hasAdditions():
		return semverParts{major: prior.major, minor: prior.minor + 1}
	default:
		return semverParts{major: prior.major, minor: prior.minor, patch: prior.patch + 1}
	}
}
