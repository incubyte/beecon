package httpapi

import (
	"time"

	"beecon/internal/access"
)

const rfc3339Millis = "2006-01-02T15:04:05.000Z07:00"

// issueKeyRequestDTO is Issue's optional request body (PD41, Slice 4): an
// empty/absent scope defaults to "read-write" (access.ParseScope), keeping
// every pre-existing caller's full-access behavior unchanged.
type issueKeyRequestDTO struct {
	Scope string `json:"scope"`
}

// issuedKeyDTO is the response to Issue: the only time the full secret ever
// appears in an API response.
type issuedKeyDTO struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Prefix    string `json:"prefix"`
	Scope     string `json:"scope"`
	CreatedAt string `json:"createdAt"`
}

func toIssuedKeyDTO(issued access.IssuedKey) issuedKeyDTO {
	return issuedKeyDTO{
		ID:        string(issued.ID),
		Key:       issued.Secret,
		Prefix:    issued.Prefix,
		Scope:     string(issued.Scope),
		CreatedAt: issued.CreatedAt.Format(rfc3339Millis),
	}
}

// keyDTO is the response to List: id, prefix, scope (PD41), created date,
// and rotation/revocation state (Slice 4/Slice 8 AC5) — never the secret or
// its hash. RevokedAt (Slice 4, AC3: the console must show revocation
// state) is the same optional-timestamp shape RotatedAt/OverlapExpiresAt
// already use.
type keyDTO struct {
	ID               string  `json:"id"`
	Prefix           string  `json:"prefix"`
	Scope            string  `json:"scope"`
	CreatedAt        string  `json:"createdAt"`
	RevokedAt        *string `json:"revokedAt,omitempty"`
	RotatedAt        *string `json:"rotatedAt,omitempty"`
	OverlapExpiresAt *string `json:"overlapExpiresAt,omitempty"`
}

func toKeyDTO(listing access.KeyListing) keyDTO {
	return keyDTO{
		ID:               string(listing.ID),
		Prefix:           listing.Prefix,
		Scope:            string(listing.Scope),
		CreatedAt:        listing.CreatedAt.Format(rfc3339Millis),
		RevokedAt:        formatOptionalTime(listing.RevokedAt),
		RotatedAt:        formatOptionalTime(listing.RotatedAt),
		OverlapExpiresAt: formatOptionalTime(listing.OverlapExpiresAt),
	}
}

func toKeyDTOs(listings []access.KeyListing) []keyDTO {
	dtos := make([]keyDTO, 0, len(listings))
	for _, listing := range listings {
		dtos = append(dtos, toKeyDTO(listing))
	}
	return dtos
}

// rotateRequestDTO is Rotate's optional request body: overlapHours lets an
// admin choose a different overlap window than PD23's 24h default.
type rotateRequestDTO struct {
	OverlapHours *int `json:"overlapHours"`
}

// rotatedKeyDTO is the response to Rotate: the new secret, returned exactly
// once (PD23), plus when the outgoing secret's overlap window ends.
type rotatedKeyDTO struct {
	ID               string `json:"id"`
	Key              string `json:"key"`
	Prefix           string `json:"prefix"`
	OverlapExpiresAt string `json:"overlapExpiresAt"`
}

func toRotatedKeyDTO(rotated access.RotateResult) rotatedKeyDTO {
	return rotatedKeyDTO{
		ID:               string(rotated.ID),
		Key:              rotated.Secret,
		Prefix:           rotated.Prefix,
		OverlapExpiresAt: rotated.OverlapExpiresAt.Format(rfc3339Millis),
	}
}

func formatOptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := t.Format(rfc3339Millis)
	return &formatted
}

// issuedSigningSecretDTO is the response to IssueSigningSecret (PD20): the
// only time the raw signing secret ever appears in an API response.
type issuedSigningSecretDTO struct {
	ID        string `json:"id"`
	Secret    string `json:"secret"`
	Prefix    string `json:"prefix"`
	CreatedAt string `json:"createdAt"`
}

func toIssuedSigningSecretDTO(issued access.IssuedSigningSecret) issuedSigningSecretDTO {
	return issuedSigningSecretDTO{
		ID:        string(issued.ID),
		Secret:    issued.Secret,
		Prefix:    issued.Prefix,
		CreatedAt: issued.CreatedAt.Format(rfc3339Millis),
	}
}

// signingSecretDTO is the response to ListSigningSecrets: id, display
// prefix, and created date only — never the secret or its ciphertext.
type signingSecretDTO struct {
	ID        string `json:"id"`
	Prefix    string `json:"prefix"`
	CreatedAt string `json:"createdAt"`
}

func toSigningSecretDTO(secret access.SigningSecret) signingSecretDTO {
	return signingSecretDTO{
		ID:        string(secret.ID),
		Prefix:    secret.DisplayPrefix,
		CreatedAt: secret.CreatedAt.Format(rfc3339Millis),
	}
}

func toSigningSecretDTOs(secrets []access.SigningSecret) []signingSecretDTO {
	dtos := make([]signingSecretDTO, 0, len(secrets))
	for _, secret := range secrets {
		dtos = append(dtos, toSigningSecretDTO(secret))
	}
	return dtos
}
