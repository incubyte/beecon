package registrybundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ContentHash computes bundle's integrity hash (PD62/PD67): a sha256 over
// its canonical JSON encoding, with ContentHash itself cleared first so the
// hash commits to the bundle's actual content, not to its own previous
// value. Shared by the registry service (which computes it once at publish,
// registryservice/facade.go) and the installation's catalog module (which
// recomputes it on every activation to verify the registry-reported hash
// matches, PD67, catalog/registry_sync.go) — both sides must use the exact
// same algorithm over the exact same wire type, or a byte-identical bundle
// could produce two different "correct" hashes and every activation would
// spuriously fail as tampered.
func ContentHash(bundle Bundle) (string, error) {
	bundle.ContentHash = ""
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
