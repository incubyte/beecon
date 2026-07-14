package httpapi

import "beecon/internal/delivery"

const rfc3339Millis = "2006-01-02T15:04:05.000Z07:00"

// setWebhookEndpointRequest is the PUT /api/v1/webhook-endpoint body (API
// Shape).
type setWebhookEndpointRequest struct {
	URL string `json:"url"`
}

// webhookEndpointCreatedDTO is SetEndpoint's response (API Shape): secret
// is present only on first creation — a later URL-only update leaves it
// empty (omitted).
type webhookEndpointCreatedDTO struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Secret    string `json:"secret,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func toWebhookEndpointCreatedDTO(result delivery.SetEndpointResult) webhookEndpointCreatedDTO {
	return webhookEndpointCreatedDTO{
		ID:        string(result.ID),
		URL:       result.URL,
		Secret:    result.Secret,
		CreatedAt: result.CreatedAt.Format(rfc3339Millis),
	}
}

// webhookEndpointDTO is GetEndpoint's response (API Shape): URL, secret
// prefix, and creation date — never the full secret.
type webhookEndpointDTO struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	SecretPrefix string `json:"secretPrefix"`
	CreatedAt    string `json:"createdAt"`
}

func toWebhookEndpointDTO(view delivery.EndpointView) webhookEndpointDTO {
	return webhookEndpointDTO{
		ID:           string(view.ID),
		URL:          view.URL,
		SecretPrefix: view.SecretPrefix,
		CreatedAt:    view.CreatedAt.Format(rfc3339Millis),
	}
}

// rotateSecretRequest is the POST /api/v1/webhook-endpoint/rotate-secret
// body (API Shape): overlapHours is optional, falling back to
// access.DefaultOverlapHours.
type rotateSecretRequest struct {
	OverlapHours *int `json:"overlapHours,omitempty"`
}

// rotatedSecretDTO is RotateSecret's response (API Shape): the new secret,
// returned exactly once.
type rotatedSecretDTO struct {
	Secret string `json:"secret"`
}

func toRotatedSecretDTO(result delivery.RotateSecretResult) rotatedSecretDTO {
	return rotatedSecretDTO{Secret: result.Secret}
}

// eventDTO is one item in GET /api/v1/events' response (API Shape).
type eventDTO struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	CreatedAt      string `json:"createdAt"`
	DeliveryStatus string `json:"deliveryStatus"`
	Attempts       int    `json:"attempts"`
	LastAttemptAt  string `json:"lastAttemptAt,omitempty"`
}

func toEventDTO(event delivery.Event) eventDTO {
	dto := eventDTO{
		ID:             string(event.ID),
		Type:           event.Type,
		CreatedAt:      event.CreatedAt.Format(rfc3339Millis),
		DeliveryStatus: string(event.Status),
		Attempts:       event.Attempts,
	}
	if event.LastAttemptAt != nil {
		dto.LastAttemptAt = event.LastAttemptAt.Format(rfc3339Millis)
	}
	return dto
}

// eventsPageDTO is one cursor-paginated page of outbox events, newest
// first; nextCursor is absent when this was the last page.
type eventsPageDTO struct {
	Items      []eventDTO `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

func toEventsPageDTO(result delivery.ListEventsResult) eventsPageDTO {
	items := make([]eventDTO, 0, len(result.Items))
	for _, event := range result.Items {
		items = append(items, toEventDTO(event))
	}
	return eventsPageDTO{Items: items, NextCursor: result.NextCursor}
}
