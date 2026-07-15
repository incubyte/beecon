package access

import (
	"context"
	"time"
)

// OperatorFacade is the access module's public surface for operator
// accounts and sessions (PD49/PD58, FD-H) — a dedicated facade, not an
// extension of Facade (the org-key/user-token/webhook-secret surface):
// operator identity and session lifecycle is a distinct responsibility with
// its own single reason to change, so it gets its own facade rather than
// growing Facade's already-large surface (SRP).
type OperatorFacade struct {
	operators        Operators
	sessions         OperatorSessions
	newOperatorID    func() string
	newSessionID     func() string
	now              func() time.Time
	sessionTTL       time.Duration
	loginMaxAttempts int
	loginLockout     time.Duration
}

// defaultLoginMaxAttempts and defaultLoginLockout are WithLoginThrottle's own
// fallback (Slice 5, FD-G) for a facade built without it — every existing
// test constructor from Slices 1-4 falls into this branch, mirroring
// organizations/driving/httpapi.Handler's own
// installationDefaultRetentionDaysOrFallback belt-and-braces pattern. These
// deliberately duplicate config.DefaultLoginMaxAttempts/config's
// BEECON_LOGIN_LOCKOUT default rather than importing config: this package
// only ever needs the fallback numbers themselves, and production wiring
// always calls WithLoginThrottle with the actually-configured values anyway.
const (
	defaultLoginMaxAttempts    = 5
	defaultLoginLockoutMinutes = 15
)

// NewOperatorFacade wires the facade with the operator and session driven
// ports, injected id minters and a clock (so tests can supply deterministic
// ids and a fixed time, matching every other facade in this codebase), and
// the configured absolute session lifetime (BEECON_SESSION_TTL).
func NewOperatorFacade(
	operators Operators,
	sessions OperatorSessions,
	newOperatorID func() string,
	newSessionID func() string,
	now func() time.Time,
	sessionTTL time.Duration,
) *OperatorFacade {
	return &OperatorFacade{
		operators:     operators,
		sessions:      sessions,
		newOperatorID: newOperatorID,
		newSessionID:  newSessionID,
		now:           now,
		sessionTTL:    sessionTTL,
	}
}

// WithLoginThrottle sets the brute-force lockout's threshold and cooldown
// (Slice 5, FD-G: BEECON_LOGIN_MAX_ATTEMPTS/BEECON_LOGIN_LOCKOUT). Production
// wiring always calls this with the configured values; test constructors
// that never call it fall back to defaultLoginMaxAttempts/
// defaultLoginLockoutMinutes, mirroring
// organizations/driving/httpapi.Handler.WithInstallationDefaultRetentionDays'
// own fluent-setter convention.
func (f *OperatorFacade) WithLoginThrottle(maxAttempts int, lockout time.Duration) *OperatorFacade {
	f.loginMaxAttempts = maxAttempts
	f.loginLockout = lockout
	return f
}

func (f *OperatorFacade) loginMaxAttemptsOrFallback() int {
	if f.loginMaxAttempts <= 0 {
		return defaultLoginMaxAttempts
	}
	return f.loginMaxAttempts
}

func (f *OperatorFacade) loginLockoutOrFallback() time.Duration {
	if f.loginLockout <= 0 {
		return defaultLoginLockoutMinutes * time.Minute
	}
	return f.loginLockout
}

// BootstrappedOperator is Bootstrap's result: the newly created operator's
// id and email — never the password or its hash.
type BootstrappedOperator struct {
	ID    OperatorID
	Email string
}

// Bootstrap creates the installation's first operator account (PD54): it
// succeeds only while no operator account exists yet — ErrOperatorExists
// (409) once one does, so this is a first-account-only path, and (from
// Slice 4 on) the break-glass reset path the admin key retains. email is
// lowercased and shape-validated; password must meet minPasswordLength
// (ErrPasswordTooShort, 422, naming the requirement) or the account is
// never created.
func (f *OperatorFacade) Bootstrap(ctx context.Context, email, password string) (BootstrappedOperator, error) {
	exists, err := f.operators.Exists(ctx)
	if err != nil {
		return BootstrappedOperator{}, err
	}
	if exists {
		return BootstrappedOperator{}, ErrOperatorExists()
	}
	operator, err := f.newValidatedOperator(email, password)
	if err != nil {
		return BootstrappedOperator{}, err
	}
	if err := f.operators.Save(ctx, operator); err != nil {
		return BootstrappedOperator{}, err
	}
	return BootstrappedOperator{ID: operator.ID, Email: operator.Email}, nil
}

// newValidatedOperator builds a fresh ACTIVE Operator (a new id, the
// lowercased+shape-validated email, and an Argon2id hash of password) but
// does not persist it — shared by Bootstrap and CreateOperator (Slice 4),
// which differ only in which existence check gates the save (Bootstrap:
// no operator account at all; CreateOperator: no other account with this
// email).
func (f *OperatorFacade) newValidatedOperator(email, password string) (Operator, error) {
	normalizedEmail, err := normalizeAndValidateEmail(email)
	if err != nil {
		return Operator{}, err
	}
	if len(password) < minPasswordLength {
		return Operator{}, ErrPasswordTooShort(minPasswordLength)
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return Operator{}, err
	}
	return Operator{
		ID:           OperatorID(f.newOperatorID()),
		Email:        normalizedEmail,
		PasswordHash: passwordHash,
		Status:       OperatorStatusActive,
		CreatedAt:    f.now(),
	}, nil
}

// OperatorSummary is ListOperators' per-row result: email, status, and
// created date — never a password hash (Slice 4, spec Slice 4 AC3).
type OperatorSummary struct {
	ID        OperatorID
	Email     string
	Status    OperatorStatus
	CreatedAt time.Time
}

// ListOperators returns every operator account in the installation (Slice
// 4, spec Slice 4 AC3): flat, installation-wide, like the admin key it
// replaces — there is no organization to scope this listing by.
func (f *OperatorFacade) ListOperators(ctx context.Context) ([]OperatorSummary, error) {
	operators, err := f.operators.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]OperatorSummary, len(operators))
	for i, operator := range operators {
		summaries[i] = OperatorSummary{
			ID:        operator.ID,
			Email:     operator.Email,
			Status:    operator.Status,
			CreatedAt: operator.CreatedAt,
		}
	}
	return summaries, nil
}

// CreatedOperator is CreateOperator's result: the newly created operator's
// id and email — never the password or its hash.
type CreatedOperator struct {
	ID    OperatorID
	Email string
}

// CreateOperator creates an additional ACTIVE operator account from within
// an authenticated console session (Slice 4, spec Slice 4 AC1/AC2) —
// distinct from Bootstrap, which is the admin-key/first-account-only path:
// this one runs any time, gated only by "no other operator already holds
// this email" (ErrEmailAlreadyExists, 409) rather than "no operator account
// exists at all". email is lowercased and shape-validated; password must
// meet minPasswordLength, exactly like Bootstrap.
func (f *OperatorFacade) CreateOperator(ctx context.Context, email, password string) (CreatedOperator, error) {
	operator, err := f.newValidatedOperator(email, password)
	if err != nil {
		return CreatedOperator{}, err
	}
	existing, err := f.operators.FindByEmail(ctx, operator.Email)
	if err != nil {
		return CreatedOperator{}, err
	}
	if existing != nil {
		return CreatedOperator{}, ErrEmailAlreadyExists()
	}
	if err := f.operators.Save(ctx, operator); err != nil {
		return CreatedOperator{}, err
	}
	return CreatedOperator{ID: operator.ID, Email: operator.Email}, nil
}

// ChangeMyPassword lets a logged-in operator change their own password
// (Slice 4, spec Slice 4 AC4; closes Slice 2's carried-forward AC4): it
// re-verifies currentPassword against the stored hash — a wrong current
// password is ErrInvalidCredentials (401), the same generic verdict Login
// itself uses, since the caller is already identified by operatorID and
// there is no email-enumeration risk left to defend against here. newPassword
// must meet minPasswordLength. On success, UpdatePasswordHash persists the
// new Argon2id hash, then RevokeAllForOperatorExcept ends every OTHER
// session belonging to operatorID — currentSessionID (the session making
// this very request, threaded in by the caller from
// access.OperatorSessionFromContext) is the one exception, so the operator
// making the change is never logged out by their own action.
func (f *OperatorFacade) ChangeMyPassword(ctx context.Context, operatorID OperatorID, currentSessionID OperatorSessionID, currentPassword, newPassword string) error {
	operator, err := f.operators.FindByID(ctx, operatorID)
	if err != nil {
		return err
	}
	if operator == nil || !verifyPassword(currentPassword, operator.PasswordHash) {
		return ErrInvalidCredentials()
	}
	if len(newPassword) < minPasswordLength {
		return ErrPasswordTooShort(minPasswordLength)
	}
	newHash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := f.operators.UpdatePasswordHash(ctx, operatorID, newHash); err != nil {
		return err
	}
	return f.sessions.RevokeAllForOperatorExcept(ctx, operatorID, currentSessionID, f.now())
}

// Deactivate disables another operator account (Slice 4, spec Slice 4 AC5):
// once DISABLED, VerifySession's own ACTIVE re-check (Slice 2) already
// rejects that operator's sessions immediately, so RevokeAllForOperator here
// is belt-and-braces — it marks every one of the target's session rows
// revoked too, rather than leaving them to expire naturally or rely solely
// on the ACTIVE check running on every future request. Refuses
// (ErrLastActiveOperator, 409) when the target is currently the
// installation's only ACTIVE operator (spec Slice 4 AC6) — deactivating it
// would leave nobody able to log in at all.
func (f *OperatorFacade) Deactivate(ctx context.Context, targetOperatorID OperatorID) error {
	target, err := f.operators.FindByID(ctx, targetOperatorID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrOperatorNotFound()
	}
	if target.IsActive() {
		activeCount, err := f.operators.CountActive(ctx)
		if err != nil {
			return err
		}
		if activeCount <= 1 {
			return ErrLastActiveOperator()
		}
	}
	if err := f.operators.SetStatus(ctx, targetOperatorID, OperatorStatusDisabled); err != nil {
		return err
	}
	return f.sessions.RevokeAllForOperator(ctx, targetOperatorID, f.now())
}

// ResetPassword is the break-glass recovery path (FD-B, admin-key
// authenticated, works even after operators exist — the only console-adjacent
// write the admin key still performs post-bootstrap): it sets a fresh
// password on targetOperatorID, reactivates the account if it had been
// DISABLED (the very scenario this path exists to recover from — every
// operator locked out or disabled), and revokes every one of its existing
// sessions (there is no "acting session" to preserve here, unlike
// ChangeMyPassword — the admin key holder is not that operator), so a
// possibly-compromised session can never survive the reset.
func (f *OperatorFacade) ResetPassword(ctx context.Context, targetOperatorID OperatorID, newPassword string) error {
	if len(newPassword) < minPasswordLength {
		return ErrPasswordTooShort(minPasswordLength)
	}
	target, err := f.operators.FindByID(ctx, targetOperatorID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrOperatorNotFound()
	}
	newHash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := f.operators.UpdatePasswordHash(ctx, targetOperatorID, newHash); err != nil {
		return err
	}
	if err := f.operators.SetStatus(ctx, targetOperatorID, OperatorStatusActive); err != nil {
		return err
	}
	return f.sessions.RevokeAllForOperator(ctx, targetOperatorID, f.now())
}

// OperatorsExist reports whether any operator account exists yet — the
// predicate authmw.ConsoleAuth uses to decide whether the break-glass admin
// key still authenticates the general console surface (PD54; Slice 4, AC8).
func (f *OperatorFacade) OperatorsExist(ctx context.Context) (bool, error) {
	return f.operators.Exists(ctx)
}

// LoggedInSession is Login's result: the opaque session token and CSRF
// token to set as cookies, and when the session expires. The token is
// returned exactly once, here — only its hash is ever persisted.
type LoggedInSession struct {
	OperatorID OperatorID
	Token      string
	CSRFToken  string
	ExpiresAt  time.Time
}

// Login verifies email/password and, on success, mints a new session. A
// wrong password and an unknown email both fail identically
// (ErrInvalidCredentials, generic 401) — for an unknown email, verifyPassword
// still runs (against the fixed decoyPasswordHash) so the response takes the
// same Argon2id-bound time either way, closing the user-enumeration timing
// side-channel; an unknown email never gets a lockout row (that would itself
// be an enumeration oracle, and unbounded row growth from probing random
// addresses) — the brute-force lockout below applies only to an email that
// resolves to a real operator. A disabled operator's correct password is
// rejected the same generic way, after paying the same verification cost as
// an active one, and does not count as a failed attempt (the account is
// already permanently unloggable-into; there is nothing further to throttle).
//
// Brute-force lockout (Slice 5, FD-G, per-account): while operator.IsLockedOut
// is true (checked against the injected clock, never wall-clock), the
// password is never even examined — the request is rejected outright with
// ErrAccountLocked (429), a response exactly as generic as ErrInvalidCredentials'
// 401 (never reveals whether an account exists, only that requests against
// *some* account are currently throttled). Once unlocked, a further wrong
// password increments FailedAttempts and, on reaching loginMaxAttemptsOrFallback,
// sets LockedUntil to now+loginLockoutOrFallback; a successful login resets the
// counter and clears the lock entirely before a session is minted.
func (f *OperatorFacade) Login(ctx context.Context, email, password string) (LoggedInSession, error) {
	operator, err := f.operators.FindByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return LoggedInSession{}, err
	}
	if operator == nil {
		verifyPassword(password, decoyPasswordHash)
		return LoggedInSession{}, ErrInvalidCredentials()
	}
	now := f.now()
	if operator.IsLockedOut(now) {
		return LoggedInSession{}, ErrAccountLocked()
	}
	if !verifyPassword(password, operator.PasswordHash) {
		if err := f.recordFailedAttempt(ctx, *operator, now); err != nil {
			return LoggedInSession{}, err
		}
		return LoggedInSession{}, ErrInvalidCredentials()
	}
	if !operator.IsActive() {
		return LoggedInSession{}, ErrInvalidCredentials()
	}
	if err := f.operators.ResetFailedAttempts(ctx, operator.ID); err != nil {
		return LoggedInSession{}, err
	}
	return f.mintSession(ctx, operator.ID)
}

// recordFailedAttempt persists one more consecutive wrong-password guess
// against operator: FailedAttempts increments by one, and once the new count
// reaches loginMaxAttemptsOrFallback, LockedUntil is set to now plus
// loginLockoutOrFallback — computed here from the count Login already holds,
// not by re-reading the row, so Operators.RecordFailedAttempt never needs to.
func (f *OperatorFacade) recordFailedAttempt(ctx context.Context, operator Operator, now time.Time) error {
	newFailedAttempts := operator.FailedAttempts + 1
	var lockedUntil *time.Time
	if newFailedAttempts >= f.loginMaxAttemptsOrFallback() {
		lockedAt := now.Add(f.loginLockoutOrFallback())
		lockedUntil = &lockedAt
	}
	return f.operators.RecordFailedAttempt(ctx, operator.ID, lockedUntil)
}

func (f *OperatorFacade) mintSession(ctx context.Context, operatorID OperatorID) (LoggedInSession, error) {
	token, err := generateSessionToken()
	if err != nil {
		return LoggedInSession{}, err
	}
	csrfToken, err := generateCSRFToken()
	if err != nil {
		return LoggedInSession{}, err
	}
	now := f.now()
	session := OperatorSession{
		ID:         OperatorSessionID(f.newSessionID()),
		OperatorID: operatorID,
		TokenHash:  hashSessionToken(token),
		CSRFToken:  csrfToken,
		CreatedAt:  now,
		ExpiresAt:  now.Add(f.sessionTTL),
	}
	if err := f.sessions.Save(ctx, session); err != nil {
		return LoggedInSession{}, err
	}
	return LoggedInSession{
		OperatorID: operatorID,
		Token:      token,
		CSRFToken:  csrfToken,
		ExpiresAt:  session.ExpiresAt,
	}, nil
}

// AuthenticatedOperator is VerifySession's result: the operator a valid
// session belongs to, the session's own id, and its CSRF token —
// authmw.ConsoleAuth reads CSRFToken to satisfy Slice 3's double-submit
// check, and (Slice 4) injects SessionID into context via
// access.WithOperatorSession so ChangeMyPassword can identify which of the
// operator's own sessions to keep alive.
type AuthenticatedOperator struct {
	OperatorID OperatorID
	SessionID  OperatorSessionID
	CSRFToken  string
}

// VerifySession authenticates a presented opaque session token: hash it,
// look it up (FindByTokenHash), confirm it is not revoked (Slice 2) and not
// past its absolute expiry, and confirm its operator is still ACTIVE. Any
// failure is the same generic ErrSessionUnauthorized — the caller never
// learns which check failed.
func (f *OperatorFacade) VerifySession(ctx context.Context, token string) (AuthenticatedOperator, error) {
	hash := hashSessionToken(token)
	session, err := f.sessions.FindByTokenHash(ctx, hash)
	if err != nil {
		return AuthenticatedOperator{}, err
	}
	if session == nil || !sessionTokenHashMatches(hash, session.TokenHash) {
		return AuthenticatedOperator{}, ErrSessionUnauthorized()
	}
	if session.IsRevoked() || session.IsExpired(f.now()) {
		return AuthenticatedOperator{}, ErrSessionUnauthorized()
	}
	operator, err := f.operators.FindByID(ctx, session.OperatorID)
	if err != nil {
		return AuthenticatedOperator{}, err
	}
	if operator == nil || !operator.IsActive() {
		return AuthenticatedOperator{}, ErrSessionUnauthorized()
	}
	return AuthenticatedOperator{OperatorID: operator.ID, SessionID: session.ID, CSRFToken: session.CSRFToken}, nil
}

// Logout ends the session identified by token (Slice 2, PD51/PD52): it hashes
// token itself and resolves the session directly, the same way VerifySession
// does, rather than requiring the caller to have already resolved a session
// id — the caller (OperatorHandler.Logout) never needs anything beyond the
// raw cookie value it already has. Idempotent by design (spec Slice 2 AC7): a
// token that matches no session, or one that is already revoked, is not an
// error — there is simply nothing left to revoke, so a repeated logout (or
// one with a stale/absent token) always succeeds rather than surfacing a
// 500. Only an actual RevokedAt write reaches OperatorSessions.Revoke, and
// that call is itself idempotent (the port's own contract) as defense in
// depth against a race between two concurrent logouts for the same session.
func (f *OperatorFacade) Logout(ctx context.Context, token string) error {
	session, err := f.sessions.FindByTokenHash(ctx, hashSessionToken(token))
	if err != nil {
		return err
	}
	if session == nil || session.IsRevoked() {
		return nil
	}
	return f.sessions.Revoke(ctx, session.ID, f.now())
}

// OperatorProfile is Me's result: the authenticated operator's own identity
// — never a password hash.
type OperatorProfile struct {
	ID    OperatorID
	Email string
}

// Me returns the profile of the operator identified by operatorID — the
// SPA's session probe (GET /api/v1/auth/me).
func (f *OperatorFacade) Me(ctx context.Context, operatorID OperatorID) (OperatorProfile, error) {
	operator, err := f.operators.FindByID(ctx, operatorID)
	if err != nil {
		return OperatorProfile{}, err
	}
	if operator == nil {
		return OperatorProfile{}, ErrSessionUnauthorized()
	}
	return OperatorProfile{ID: operator.ID, Email: operator.Email}, nil
}
