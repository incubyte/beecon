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

// eventTypesPointer renders a nil EventTypes filter as JSON null (match
// every event type, PD45) rather than an empty array, matching the API
// Shape's `eventTypes|null`; a non-nil (possibly multi-element) filter
// renders as its own JSON array.
func eventTypesPointer(types []string) *[]string {
	if types == nil {
		return nil
	}
	return &types
}

// endpointListItemDTO is one item in GET /api/v1/webhook-endpoints'
// response (Slice 8, API Shape): never a secret, only its display prefix.
type endpointListItemDTO struct {
	ID                  string    `json:"id"`
	URL                 string    `json:"url"`
	EventTypes          *[]string `json:"eventTypes"`
	Status              string    `json:"status"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
	SecretPrefix        string    `json:"secretPrefix"`
	CreatedAt           string    `json:"createdAt"`
}

func toEndpointListItemDTO(item delivery.EndpointListItem) endpointListItemDTO {
	return endpointListItemDTO{
		ID:                  string(item.ID),
		URL:                 item.URL,
		EventTypes:          eventTypesPointer(item.EventTypes),
		Status:              string(item.Status),
		ConsecutiveFailures: item.ConsecutiveFailures,
		SecretPrefix:        item.SecretPrefix,
		CreatedAt:           item.CreatedAt.Format(rfc3339Millis),
	}
}

// endpointListDTO is GET /api/v1/webhook-endpoints' response envelope
// (Slice 8, API Shape) — a flat items array, no cursor pagination (the cap
// keeps an org's own endpoint count small).
type endpointListDTO struct {
	Items []endpointListItemDTO `json:"items"`
}

func toEndpointListDTO(items []delivery.EndpointListItem) endpointListDTO {
	dtos := make([]endpointListItemDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toEndpointListItemDTO(item))
	}
	return endpointListDTO{Items: dtos}
}

// createEndpointRequest is POST /api/v1/webhook-endpoints' request body
// (Slice 8, API Shape): EventTypes absent or JSON null means "match every
// event type" (PD45); present (even []) restricts the endpoint to exactly
// those types.
type createEndpointRequest struct {
	URL        string    `json:"url"`
	EventTypes *[]string `json:"eventTypes"`
}

func (req createEndpointRequest) eventTypes() []string {
	if req.EventTypes == nil {
		return nil
	}
	return *req.EventTypes
}

// createEndpointDTO is CreateEndpoint's response (Slice 8, API Shape): the
// secret shown exactly once, at creation.
type createEndpointDTO struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	EventTypes *[]string `json:"eventTypes"`
	Secret     string    `json:"secret"`
}

func toCreateEndpointDTO(result delivery.CreateEndpointResult) createEndpointDTO {
	return createEndpointDTO{
		ID:         string(result.ID),
		URL:        result.URL,
		EventTypes: eventTypesPointer(result.EventTypes),
		Secret:     result.Secret,
	}
}

// updateEndpointRequest is PUT /api/v1/webhook-endpoints/{wepId}'s request
// body (Slice 8, API Shape): a whole-object update — url is always
// required (mirroring setWebhookEndpointRequest's own rule), eventTypes
// nil clears the filter back to "match every type".
type updateEndpointRequest struct {
	URL        string    `json:"url"`
	EventTypes *[]string `json:"eventTypes"`
}

func (req updateEndpointRequest) eventTypes() []string {
	if req.EventTypes == nil {
		return nil
	}
	return *req.EventTypes
}

// updateEndpointDTO is UpdateEndpoint's/EnableEndpoint's/DisableEndpoint's
// shared response shape (Slice 8): never a secret.
type updateEndpointDTO struct {
	ID                  string    `json:"id"`
	URL                 string    `json:"url"`
	EventTypes          *[]string `json:"eventTypes"`
	Status              string    `json:"status"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
	CreatedAt           string    `json:"createdAt"`
}

func toUpdateEndpointDTO(result delivery.UpdateEndpointResult) updateEndpointDTO {
	return updateEndpointDTO{
		ID:                  string(result.ID),
		URL:                 result.URL,
		EventTypes:          eventTypesPointer(result.EventTypes),
		Status:              string(result.Status),
		ConsecutiveFailures: result.ConsecutiveFailures,
		CreatedAt:           result.CreatedAt.Format(rfc3339Millis),
	}
}
