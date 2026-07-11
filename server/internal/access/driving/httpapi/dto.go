package httpapi

import "beecon/internal/access"

const rfc3339Millis = "2006-01-02T15:04:05.000Z07:00"

// issuedKeyDTO is the response to Issue: the only time the full secret ever
// appears in an API response.
type issuedKeyDTO struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Prefix    string `json:"prefix"`
	CreatedAt string `json:"createdAt"`
}

func toIssuedKeyDTO(issued access.IssuedKey) issuedKeyDTO {
	return issuedKeyDTO{
		ID:        string(issued.ID),
		Key:       issued.Secret,
		Prefix:    issued.Prefix,
		CreatedAt: issued.CreatedAt.Format(rfc3339Millis),
	}
}

// keyDTO is the response to List: id, prefix, and created date only — never
// the secret or its hash.
type keyDTO struct {
	ID        string `json:"id"`
	Prefix    string `json:"prefix"`
	CreatedAt string `json:"createdAt"`
}

func toKeyDTO(key access.ServerApiKey) keyDTO {
	return keyDTO{
		ID:        string(key.ID),
		Prefix:    key.LookupPrefix,
		CreatedAt: key.CreatedAt.Format(rfc3339Millis),
	}
}

func toKeyDTOs(keys []access.ServerApiKey) []keyDTO {
	dtos := make([]keyDTO, 0, len(keys))
	for _, key := range keys {
		dtos = append(dtos, toKeyDTO(key))
	}
	return dtos
}
