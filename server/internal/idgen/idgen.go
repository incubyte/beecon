// Package idgen mints type-prefixed CUID2 string ids (e.g. "org_<cuid2>").
// Id generation is shared infrastructure; each feature is injected a minter
// func rather than sharing one IDGen interface.
package idgen

import "github.com/akshayvadher/cuid2"

// Prefixed returns a func that mints CUID2 ids with the given prefix.
func Prefixed(prefix string) func() string {
	return func() string {
		return prefix + cuid2.CreateId()
	}
}
