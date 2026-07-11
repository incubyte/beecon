package httpapi

import "beecon/internal/connections"

const rfc3339Millis = "2006-01-02T15:04:05.000Z07:00"

// initiateConnectionRequest is the POST /api/v1/connections/initiate body.
type initiateConnectionRequest struct {
	UserID        string `json:"userId"`
	IntegrationID string `json:"integrationId"`
	RedirectURI   string `json:"redirectUri"`
}

// initiatedConnectionDTO is Initiate's response: a conn_-prefixed id, the
// INITIATED status, and the redirectUrl pointing at Beecon's own connect
// page, bound to exactly this connection attempt (AC7, AC8).
type initiatedConnectionDTO struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	RedirectURL string `json:"redirectUrl"`
}

func toInitiatedConnectionDTO(initiated connections.InitiatedConnection) initiatedConnectionDTO {
	return initiatedConnectionDTO{
		ID:          string(initiated.Connection.ID),
		Status:      string(initiated.Connection.Status),
		RedirectURL: initiated.RedirectURL,
	}
}

// connectionDTO is Get's response: status, provider, and user — never the
// connect token or (from Slice 4) tokens.
type connectionDTO struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ProviderSlug string `json:"providerSlug"`
	UserID       string `json:"userId"`
	CreatedAt    string `json:"createdAt"`
}

func toConnectionDTO(connection connections.Connection) connectionDTO {
	return connectionDTO{
		ID:           string(connection.ID),
		Status:       string(connection.Status),
		ProviderSlug: connection.ProviderSlug,
		UserID:       string(connection.UserID),
		CreatedAt:    connection.CreatedAt.Format(rfc3339Millis),
	}
}
