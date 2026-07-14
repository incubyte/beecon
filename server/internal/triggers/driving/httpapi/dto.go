package httpapi

import "beecon/internal/triggers"

const rfc3339Millis = "2006-01-02T15:04:05.000Z07:00"

// createTriggerInstanceRequest is the POST /api/v1/trigger-instances body
// (API Shape): the userId recorded on the instance is always the owning
// connection's own user — there is no independent userId field.
type createTriggerInstanceRequest struct {
	ConnectionID string         `json:"connectionId"`
	TriggerSlug  string         `json:"triggerSlug"`
	Config       map[string]any `json:"config"`
}

// createdTriggerInstanceDTO is Create's 201 response (API Shape): just the
// new id and its born-ACTIVE status.
type createdTriggerInstanceDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func toCreatedTriggerInstanceDTO(instance triggers.TriggerInstance) createdTriggerInstanceDTO {
	return createdTriggerInstanceDTO{ID: string(instance.ID), Status: string(instance.Status)}
}

// triggerInstanceDTO is Get's and List's per-item response: status, trigger
// slug, connection, config, and owning user (PD33's AC: "showing status,
// trigger slug, connection, and config").
type triggerInstanceDTO struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	ConnectionID string         `json:"connectionId"`
	TriggerSlug  string         `json:"triggerSlug"`
	Config       map[string]any `json:"config"`
	UserID       string         `json:"userId"`
	CreatedAt    string         `json:"createdAt"`
}

func toTriggerInstanceDTO(instance triggers.TriggerInstance) triggerInstanceDTO {
	return triggerInstanceDTO{
		ID:           string(instance.ID),
		Status:       string(instance.Status),
		ConnectionID: string(instance.ConnectionID),
		TriggerSlug:  instance.TriggerSlug,
		Config:       instance.Config,
		UserID:       string(instance.UserID),
		CreatedAt:    instance.CreatedAt.Format(rfc3339Millis),
	}
}

// triggerInstancesPageDTO is List's response: one cursor-paginated page of
// trigger instances, newest first; nextCursor is absent when this was the
// last page.
type triggerInstancesPageDTO struct {
	Items      []triggerInstanceDTO `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
}

func toTriggerInstancesPageDTO(result triggers.ListResult) triggerInstancesPageDTO {
	items := make([]triggerInstanceDTO, 0, len(result.Items))
	for _, instance := range result.Items {
		items = append(items, toTriggerInstanceDTO(instance))
	}
	return triggerInstancesPageDTO{Items: items, NextCursor: result.NextCursor}
}

// triggerInstanceStatusDTO is Disable's and Enable's response: the
// instance's id and its new status.
type triggerInstanceStatusDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func toTriggerInstanceStatusDTO(instance triggers.TriggerInstance) triggerInstanceStatusDTO {
	return triggerInstanceStatusDTO{ID: string(instance.ID), Status: string(instance.Status)}
}
