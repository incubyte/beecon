// expireConnection is FD1's single funnel: every ACTIVE->EXPIRED transition
// and connection.expired emission goes through here (scheduler, request-path
// refresh, reconciliation), guarded by TransitionStatus's conditional flip so
// the event fires exactly once regardless of which caller detected it.
package connections

import "context"

// EventTypeConnectionExpired is connections' own copy of
// delivery.EventTypeConnectionExpired's literal (BOUNDARIES: connections
// does not import delivery — EventSink is the seam).
const EventTypeConnectionExpired = "connection.expired"

const (
	ExpiredReasonRefreshDenied        = "refresh_denied"
	ExpiredReasonReconciliationFailed = "reconciliation_failed"
)

// ExpiredEventData is PD32's connection.expired data shape.
type ExpiredEventData struct {
	ConnectionID  string `json:"connectionId"`
	UserID        string `json:"userId"`
	IntegrationID string `json:"integrationId"`
	ProviderSlug  string `json:"providerSlug"`
	Reason        string `json:"reason"`
}

func (f *Facade) expireConnection(ctx context.Context, connection Connection, reason string) (Connection, error) {
	expired := connection.MarkExpired()
	flipped, err := f.repo.TransitionStatus(ctx, connection.OrgID, connection.ID, StatusActive, StatusExpired)
	if err != nil {
		return Connection{}, err
	}
	if !flipped {
		return expired, nil
	}
	if err := f.emitExpired(ctx, expired, reason); err != nil {
		return Connection{}, err
	}
	return expired, nil
}

func (f *Facade) emitExpired(ctx context.Context, connection Connection, reason string) error {
	if f.events == nil {
		return nil
	}
	return f.events.ConnectionExpired(ctx, connection.OrgID, ExpiredEventData{
		ConnectionID:  string(connection.ID),
		UserID:        string(connection.UserID),
		IntegrationID: string(connection.IntegrationID),
		ProviderSlug:  connection.ProviderSlug,
		Reason:        reason,
	})
}
