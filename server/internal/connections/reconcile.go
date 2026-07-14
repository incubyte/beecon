package connections

import (
	"context"
	"errors"
	"time"
)

const reconcileClaimBatchLimit = 50

const reconcileLeaseTTL = 2 * time.Minute

const reconcileSpacingMax = 500 * time.Millisecond

type sleepFunc func(ctx context.Context, d time.Duration) error

func realSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ReconcileOnce claims ACTIVE connections not probed within
// BEECON_RECONCILE_INTERVAL and verifies each against its provider (PD37),
// spacing successive probes apart with a jittered pause.
func (f *Facade) ReconcileOnce(ctx context.Context) error {
	now := f.now()
	due, err := f.refreshQueue.ClaimDueReconcile(ctx, now, f.reconcileInterval, reconcileLeaseTTL, reconcileClaimBatchLimit)
	if err != nil {
		return err
	}
	for i, connection := range due {
		if i > 0 {
			if err := f.sleep(ctx, f.reconcileSpacing()); err != nil {
				return err
			}
		}
		f.reconcileOne(ctx, connection)
	}
	return nil
}

func (f *Facade) reconcileOne(ctx context.Context, connection Connection) {
	definition, err := f.providers.GetProviderDefinition(ctx, connection.ProviderSlug)
	if err != nil || definition.UserInfoURL == "" {
		return
	}
	accessToken, err := f.vault.Decrypt(connection.EncryptedAccessToken)
	if err != nil {
		return
	}

	probeErr := f.oauthClient.FetchUserInfo(ctx, definition.UserInfoURL, accessToken)
	if probeErr == nil {
		_ = f.markReconciled(ctx, connection)
		return
	}
	if !errors.Is(probeErr, ErrProbeUnauthorized) {
		// Network/5xx is never evidence (FD9) — skip, retry next run.
		return
	}
	f.reconcileRevocation(ctx, connection)
}

// reconcileRevocation confirms an unauthorized probe with a forced refresh
// (FD9): RefreshDenied has already expired the connection — recording
// ExpiredReasonReconciliationFailed, distinct from a scheduled/request-path
// refusal, since reconciliation catches a different failure mode (a token
// the provider revoked out-of-band, not yet caught by expiry alone) — and
// emitted its event by the time this returns; any other outcome leaves it
// ACTIVE.
func (f *Facade) reconcileRevocation(ctx context.Context, connection Connection) {
	refreshed, err := f.refreshOnce(ctx, connection.OrgID, connection.ID, refreshParams{force: true, deniedReason: ExpiredReasonReconciliationFailed})
	if err != nil || refreshed.Status == StatusExpired {
		return
	}
	_ = f.markReconciled(ctx, refreshed)
}

func (f *Facade) markReconciled(ctx context.Context, connection Connection) error {
	return f.repo.Update(ctx, connection.MarkReconciled(f.now()))
}

func (f *Facade) reconcileSpacing() time.Duration {
	return time.Duration(f.jitter() * float64(reconcileSpacingMax))
}
