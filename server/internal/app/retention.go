package app

import (
	"context"

	"beecon/internal/organizations"
)

// retentionOrgPageSize bounds how many organizations retentionReader.
// ListOrgIDs fetches per page while it walks organizationsFacade.ListAll —
// the max page size Beecon's cursor pagination already allows everywhere
// else (httpx.DecodeCursor/EncodeCursor's own max), not a new concept.
const retentionOrgPageSize = 200

// retentionReader is the single composition-root adapter satisfying both
// logging.RetentionReader and delivery.RetentionReader (Slice 7, PD44) —
// structurally: both narrow ports declare the same ListOrgIDs shape plus
// their own Effective*RetentionDays method, so this one concrete type
// wired twice (loggingFacade.WithRetention / deliveryFacade.WithRetention)
// satisfies each without either module importing the other's port.
// installationDefaultDays is the resolved BEECON_RETENTION_DAYS value
// (config parsing + the same OrDefault fallback every other worker-tunable
// setting in this file gets) — logging/delivery never see it directly;
// only this adapter combines it with an org's own governance override.
type retentionReader struct {
	organizations           *organizations.Facade
	installationDefaultDays int
}

// ListOrgIDs enumerates every organization in the installation (Slice 7):
// installation-level, like organizations.Repository.ListAll itself — the
// purge worker iterates every org one at a time, mirroring
// delivery.WorkQueue/triggers.PollQueue's own "one shared background loop,
// not a per-org one" shape.
func (r retentionReader) ListOrgIDs(ctx context.Context) ([]organizations.OrgID, error) {
	var ids []organizations.OrgID
	cursor := ""
	for {
		page, err := r.organizations.ListAll(ctx, organizations.ListAllParams{Cursor: cursor, Limit: retentionOrgPageSize})
		if err != nil {
			return nil, err
		}
		for _, org := range page.Organizations {
			ids = append(ids, org.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return ids, nil
}

// EffectiveLogRetentionDays satisfies logging.RetentionReader: org's own
// governance override (Governance.LogRetentionDays), resolved against the
// installation default.
func (r retentionReader) EffectiveLogRetentionDays(ctx context.Context, org organizations.OrgID) (int, error) {
	governance, err := r.organizations.GetGovernance(ctx, org)
	if err != nil {
		return 0, err
	}
	return governance.EffectiveLogRetentionDays(r.installationDefaultDays), nil
}

// EffectiveEventRetentionDays satisfies delivery.RetentionReader: org's own
// governance override (Governance.EventRetentionDays), resolved against the
// installation default.
func (r retentionReader) EffectiveEventRetentionDays(ctx context.Context, org organizations.OrgID) (int, error) {
	governance, err := r.organizations.GetGovernance(ctx, org)
	if err != nil {
		return 0, err
	}
	return governance.EffectiveEventRetentionDays(r.installationDefaultDays), nil
}
