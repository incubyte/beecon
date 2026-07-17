package registryservice

import (
	"bytes"
	"encoding/json"

	"beecon/internal/registrybundle"
)

// ParseBundleStrict decodes raw as a registrybundle.Bundle under the same
// strictness catalog's embedded-YAML loader applies via yaml.v3's
// KnownFields(true) (definition_v1.go): an unknown or misspelled field fails
// the parse instead of being silently dropped. The registry binary depends
// on no domain module (BOUNDARIES.md), so this deliberately does not import
// catalog; it reapplies the equivalent strictness directly against the
// shared registrybundle.Bundle wire shape using encoding/json's own
// DisallowUnknownFields, which recurses into every nested struct
// (OAuthConfig, ExpectedParam, Tool, ToolMapping, Pagination, Trigger,
// TriggerPoll) the same way KnownFields does — registrybundle.Bundle is
// deliberately the JSON mirror of catalog's formatVersion: 1 shape, field
// for field, so this is not a duplicated validator, just the same
// strictness applied to the JSON wire form instead of the YAML file form.
func ParseBundleStrict(raw []byte) (registrybundle.Bundle, error) {
	var bundle registrybundle.Bundle
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return registrybundle.Bundle{}, ErrStrictParseFailed(err.Error())
	}
	return bundle, nil
}
