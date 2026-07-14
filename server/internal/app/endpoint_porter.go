package app

import (
	"context"

	"beecon/internal/delivery"
	"beecon/internal/organizations"
)

// endpointPorterAdapter satisfies organizations.EndpointPorter over
// *delivery.Facade (Slice 9, PD46): organizations never imports delivery
// (BOUNDARIES) — config export/import reaches an org's webhook endpoints
// only through this composition-root adapter, mirroring
// triggersEventSink/connectionsEventSink's own consumer-defined-port shape
// (app/recorders.go), just for the opposite module pairing.
type endpointPorterAdapter struct {
	delivery *delivery.Facade
}

var _ organizations.EndpointPorter = endpointPorterAdapter{}

// ListEndpoints satisfies organizations.EndpointPorter: every one of org's
// endpoints, URL and event-type filter only — never a secret (Slice 9,
// PD46's export never carries one).
func (a endpointPorterAdapter) ListEndpoints(ctx context.Context, org organizations.OrgID) ([]organizations.PortedEndpoint, error) {
	items, err := a.delivery.ListEndpoints(ctx, org)
	if err != nil {
		return nil, err
	}
	ported := make([]organizations.PortedEndpoint, 0, len(items))
	for _, item := range items {
		ported = append(ported, organizations.PortedEndpoint{ID: string(item.ID), URL: item.URL, EventTypes: item.EventTypes})
	}
	return ported, nil
}

// CreateEndpoint satisfies organizations.EndpointPorter: mints a fresh
// endpoint with a freshly generated signing secret, returned exactly once
// (Slice 9, PD46 — an import document never carries a secret to reuse).
func (a endpointPorterAdapter) CreateEndpoint(ctx context.Context, org organizations.OrgID, url string, eventTypes []string) (organizations.PortedEndpointSecret, error) {
	result, err := a.delivery.CreateEndpoint(ctx, org, url, eventTypes)
	if err != nil {
		return organizations.PortedEndpointSecret{}, err
	}
	return organizations.PortedEndpointSecret{ID: string(result.ID), Secret: result.Secret}, nil
}

// UpdateEndpoint satisfies organizations.EndpointPorter: replaces one
// existing endpoint's URL and event-type filter — never its secret (Slice
// 9).
func (a endpointPorterAdapter) UpdateEndpoint(ctx context.Context, org organizations.OrgID, endpointID, url string, eventTypes []string) error {
	_, err := a.delivery.UpdateEndpoint(ctx, org, delivery.EndpointID(endpointID), url, eventTypes)
	return err
}

// DeleteEndpoint satisfies organizations.EndpointPorter: permanently
// removes one endpoint (Slice 9's mode=replace: "removing what the document
// omits").
func (a endpointPorterAdapter) DeleteEndpoint(ctx context.Context, org organizations.OrgID, endpointID string) error {
	return a.delivery.DeleteEndpoint(ctx, org, delivery.EndpointID(endpointID))
}
