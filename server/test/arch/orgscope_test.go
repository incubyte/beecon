// Package arch: enforces the security-critical invariant that every
// org-scoped persistence-port operation requires an organization id — a
// query without org scope cannot be expressed. Reflects over the driven
// Repository interfaces and fails a method whose identifying parameter is
// neither organizations.OrgID nor a struct that itself carries an OrgID
// field (the "Save(ctx, wholeEntity)" shape, where the entity's own OrgID
// defines the scope of the write).
package arch

import (
	"context"
	"reflect"
	"testing"

	"beecon/internal/access"
	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/delivery"
	"beecon/internal/execution"
	"beecon/internal/logging"
	"beecon/internal/organizations"
	"beecon/internal/triggers"
)

var orgIDType = reflect.TypeOf(organizations.OrgID(""))
var contextType = reflect.TypeOf((*context.Context)(nil)).Elem()

// isOrgScopedParam reports whether t is an acceptable identifying parameter
// for an org-scoped port method.
func isOrgScopedParam(t reflect.Type) bool {
	if t == orgIDType {
		return true
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	field, ok := t.FieldByName("OrgID")
	return ok && field.Type == orgIDType
}

// orgScopeViolations returns one message per method on ifaceType whose
// second parameter (the first parameter after ctx) is not org-scoped per
// isOrgScopedParam. Every port method in this codebase takes ctx first.
func orgScopeViolations(ifaceType reflect.Type) []string {
	var problems []string
	for i := 0; i < ifaceType.NumMethod(); i++ {
		method := ifaceType.Method(i)
		fn := method.Type
		if fn.NumIn() < 2 || fn.In(0) != contextType {
			problems = append(problems, method.Name+": expected context.Context as its first parameter")
			continue
		}
		if !isOrgScopedParam(fn.In(1)) {
			problems = append(problems, method.Name+": second parameter ("+fn.In(1).String()+") is neither organizations.OrgID nor a struct carrying an OrgID field")
		}
	}
	return problems
}

// --- Self-check fixtures: prove orgScopeViolations actually detects the bug
// it exists to catch, independent of the production interfaces below. ---

// badInterface fixture: a query method taking a raw string id instead of
// organizations.OrgID — exactly the "forgot the WHERE clause" class of bug
// this architecture test exists to catch.
type badInterface interface {
	FindByID(ctx context.Context, id string) (int, error)
}

type goodEntity struct {
	OrgID organizations.OrgID
	Name  string
}

// goodInterface fixture: one method scoped directly by OrgID, one method
// that saves a whole entity carrying its own OrgID field. Both are the
// accepted shapes.
type goodInterface interface {
	Save(ctx context.Context, e goodEntity) error
	FindByID(ctx context.Context, org organizations.OrgID, id string) (*goodEntity, error)
}

func TestOrgScopeChecker_FlagsAQueryMethodTakingARawStringInsteadOfOrgID(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*badInterface)(nil)).Elem())

	if len(got) == 0 {
		t.Fatal("expected orgScopeViolations to flag badInterface.FindByID(ctx, id string) — the checker would not catch a real org-scope regression")
	}
}

func TestOrgScopeChecker_PassesAnInterfaceScopedEitherDirectlyOrThroughAnEntitysOwnOrgIDField(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*goodInterface)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("expected no violations for a correctly org-scoped interface, got %v", got)
	}
}

// --- The actual architecture tests, applied to Slice 2's production ports.
// ---

func TestAccessRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*access.Repository)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("access.Repository has org-scope violations: %v", got)
	}
}

// TestAccessSigningSecretsRepository_EveryMethodIsOrgScoped: Slice 5 (PD20) —
// a SigningSecret belongs to exactly one organization, so every
// persistence-port operation on it must be scoped by that organization's id.
// Save takes the whole SigningSecret (which itself carries OrgID);
// ListByOrg takes organizations.OrgID directly.
func TestAccessSigningSecretsRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*access.SigningSecrets)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("access.SigningSecrets has org-scope violations: %v", got)
	}
}

// TestAccessApiKeySecretsRepository_EveryMethodIsOrgScoped: Slice 8 (PD23) —
// an ApiKeySecret belongs to exactly one ServerApiKey, which itself belongs
// to exactly one organization, so every persistence-port operation on it
// must still be scoped by that organization's id (the same rule
// access.Repository itself is held to). Every ApiKeySecrets method takes
// organizations.OrgID directly as its second parameter — Save's own entity
// argument (ApiKeySecret) carries no OrgID field of its own (a secret is
// identified by the key it belongs to, not an organization directly), which
// is exactly why the port shape takes OrgID as an explicit parameter instead
// of relying on isOrgScopedParam's "struct carrying its own OrgID field"
// path.
func TestAccessApiKeySecretsRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*access.ApiKeySecrets)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("access.ApiKeySecrets has org-scope violations: %v", got)
	}
}

func TestOrganizationsUserRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*organizations.UserRepository)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("organizations.UserRepository has org-scope violations: %v", got)
	}
}

// TestOrganizationsGovernanceRepository_EveryMethodIsOrgScoped: Slice 5
// (PD42/PD43) — a Governance settings record belongs to exactly one
// organization (its primary key IS the organization_id, migration 0018), and
// Slice 5's own AC requires that changing one org's governance never affects
// another's, so every persistence-port operation on it must take that
// organization's id. FindByOrg takes organizations.OrgID directly;
// SaveGovernance takes the whole Governance (which itself carries an OrgID
// field) — the same "Save(ctx, wholeEntity)" shape goodInterface's own
// fixture above documents as acceptable.
func TestOrganizationsGovernanceRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*organizations.GovernanceRepository)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("organizations.GovernanceRepository has org-scope violations: %v", got)
	}
}

// TestConnectionsRepository_EveryMethodIsOrgScoped: Slice 3 (BOUNDARIES:
// connections depends on organizations) — a Connection belongs to exactly one
// organization, so every persistence-port method must take that organization
// id.
func TestConnectionsRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*connections.Repository)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("connections.Repository has org-scope violations: %v", got)
	}
}

// TestLoggingRepository_EveryMethodIsOrgScoped: Slice 5 (BOUNDARIES: logging
// depends on organizations) — an EventLog belongs to exactly one
// organization, and AC10 requires that a caller can never see another
// organization's log entries. Save takes the whole EventLog (which itself
// carries OrgID); Query takes organizations.OrgID directly — a query without
// org scope cannot be expressed.
func TestLoggingRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*logging.Repository)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("logging.Repository has org-scope violations: %v", got)
	}
}

// TestExecutionFilesRepository_EveryMethodIsOrgScoped: Slice 7 (PD22,
// ADR-0011) — an uploaded file belongs to exactly one organization, and
// AC2/AC5 require that a file_ id can never be resolved (for download or as
// a file-typed tool argument) across organizations. Save takes the whole
// FileMetadata (which itself carries OrgID); FindByID takes
// organizations.OrgID directly.
func TestExecutionFilesRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*execution.Files)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("execution.Files has org-scope violations: %v", got)
	}
}

// TestTriggersRepository_EveryMethodIsOrgScoped: Slice 2 (PD33; BOUNDARIES:
// triggers depends on connections and catalog) — a TriggerInstance belongs
// to exactly one organization, and cross-org access must be indistinguishable
// from not-found (PD33's own AC), so every persistence-port method must take
// that organization's id. Save takes the whole TriggerInstance (which itself
// carries OrgID); FindByID/ListPage/Delete/DeleteByConnection all take
// organizations.OrgID directly. The installation-level ClaimDuePolls claim
// query is deliberately a separate port (triggers.PollQueue, Slice 4) so it
// can be whitelisted honestly instead of polluting this org-scoped check —
// see TestInstallationLevelPortsAreExplicitlyWhitelisted below.
func TestTriggersRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*triggers.Repository)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("triggers.Repository has org-scope violations: %v", got)
	}
}

// TestDeliveryRepository_EveryMethodIsOrgScoped: Slice 3 (PD27/PD31;
// BOUNDARIES: delivery depends on access and organizations) — a
// WebhookEndpoint and an outbox Event each belong to exactly one
// organization (PD31's own AC5: fetching another organization's endpoint or
// event must be indistinguishable from not-found), so every persistence-port
// method on delivery.Repository must take that organization's id.
// SaveEndpoint/SaveEvent take the whole entity (which itself carries OrgID);
// FindEndpoint/FindEvent/ListEventsPage take organizations.OrgID directly.
// The installation-level ClaimDue claim query is deliberately a separate
// port (delivery.WorkQueue) so it can be whitelisted honestly instead of
// polluting this org-scoped check — see
// TestInstallationLevelPortsAreExplicitlyWhitelisted below.
func TestDeliveryRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*delivery.Repository)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("delivery.Repository has org-scope violations: %v", got)
	}
}

// TestAccessWebhookSecretsRepository_EveryMethodIsOrgScoped: Slice 3
// (PD27/PD31) — a WebhookSigningSecret belongs directly to exactly one
// organization (no intermediate "key" entity, unlike ApiKeySecret), so every
// persistence-port operation on it must still be scoped by that
// organization's id, mirroring access.ApiKeySecrets' own org-scoping rule
// (rotation state is the only reason this isn't a plain Save-carries-OrgID
// shape — Save's own WebhookSigningSecret argument does carry OrgID, but
// ListByOrg/MarkExpiring both need it as an explicit parameter too).
func TestAccessWebhookSecretsRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*access.WebhookSecrets)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("access.WebhookSecrets has org-scope violations: %v", got)
	}
}

// TestInstallationLevelPortsAreExplicitlyWhitelisted documents (and pins,
// via NumMethod, so a rename/removal is noticed) the ports deliberately
// exempted from org-scoping: access.PrefixLookup authenticates a secret
// before any organization is known — the lookup prefix is how a caller's
// organization is discovered in the first place; access.SigningSecretLookup
// is the same shape for user tokens (Slice 5, PD20) — VerifyUserToken
// discovers a JWT's signing secret (and so its organization) by the token's
// "kid" header before any organization is known, pre-auth exactly like
// PrefixLookup; organizations.Repository operates on Organization itself,
// which IS the isolation unit with no wider scope to filter by;
// organizations.Repository's own ListAll (Slice 1, PD40) is the same
// installation-level shape once more: an operator-only view over every
// organization in the installation, guarded by AdminAuth rather than any
// org-scoped key — see
// TestOrganizationsRepository_ListAllWouldFailOrgScopingIfNotWhitelisted
// (organizations_repository_installation_level_test.go) for the pinning
// test that proves this whitelist entry is deliberate; catalog.Repository is
// installation-level by design (PD7: an Integration is visible to every
// organization in the installation) — there is no organization id to filter
// by; connections.OAuthRepository is deliberately
// pre-auth (Slice 4): the connect page and OAuth callback authenticate a
// connection attempt through its single-use connect token or CSRF state,
// arriving in the end user's browser before any organization API key is
// ever presented; and execution.FileStore (Slice 7, PD22) is deliberately
// key-addressed byte storage, not org-scoped queryable state — it only ever
// takes an opaque storageKey minted internally as a FileID, and every caller
// reaches it strictly after execution.Files' own org-scoped FindByID has
// already confirmed the file belongs to the caller's organization (AC2,
// AC5), the same "authorize before the pre-auth lookup" spirit as
// PrefixLookup/SigningSecretLookup above; and delivery.WorkQueue (Slice 3,
// PD29) is deliberately installation-level by design, not an oversight: the
// outbox dispatcher is one shared background loop scanning for due work
// across every organization at once (section 3 of the architecture doc),
// not a per-org loop — but ClaimDue's own doc comment (port.go) is explicit
// that every claimed Event still carries its own OrgID, so no row's
// organization is ever ambiguous once claimed; this is the same
// cross-org-by-design rationale as PrefixLookup's pre-auth lookup, just for
// a different reason (a shared worker loop rather than "organization not yet
// known"); and triggers.PollQueue (Phase 3 Slice 4, PD29/PD34) is the same
// rationale again, one worker loop later: the poller claims due
// TriggerInstances across every organization at once, but every claimed
// instance still carries its own OrgID (ClaimDuePolls' own doc comment,
// port.go); and connections.RefreshQueue (Phase 3 Slice 5, PD29/PD36/PD37) is
// the same rationale a third time: RefreshDueOnce and ReconcileOnce each
// claim due ACTIVE Connections across every organization in one shared
// worker loop, but every claimed Connection still carries its own OrgID
// (RefreshQueue's own doc comment, connections/port.go) — exactly the
// delivery.WorkQueue/triggers.PollQueue split, one background job later; and
// connections.StatusCounter/delivery.OutboxStats (Phase 3 Slice 7, PD38d) are
// the same cross-org-by-design rationale once more, for a metrics scrape
// rather than a worker claim: a Prometheus gauge has no per-org dimension
// anywhere in this codebase, so the connections-by-status and outbox
// depth/oldest-pending-age gauges are installation-wide, scrape-time queries
// by design — there is no organization to filter by, not an oversight
// (StatusCounter's and OutboxStats' own doc comments, connections/port.go and
// delivery/port.go); and access.Operators/access.OperatorSessions (Phase 5
// Slice 1, PD49/PD58) are the same "no organization to scope by" rationale
// PrefixLookup/SigningSecretLookup already established, one credential class
// later: an Operator administers the whole installation (not one
// organization), exactly like the admin key it replaces, and an
// OperatorSession belongs to one operator, never to an organization — see
// TestAccessOperatorsRepository_WouldFailOrgScopingIfNotWhitelisted and
// TestAccessOperatorSessionsRepository_WouldFailOrgScopingIfNotWhitelisted
// (operators_repository_installation_level_test.go) for the pinning tests
// that prove these two whitelist entries are deliberate.
func TestInstallationLevelPortsAreExplicitlyWhitelisted(t *testing.T) {
	whitelisted := []reflect.Type{
		reflect.TypeOf((*access.PrefixLookup)(nil)).Elem(),
		reflect.TypeOf((*access.SigningSecretLookup)(nil)).Elem(),
		reflect.TypeOf((*access.Operators)(nil)).Elem(),
		reflect.TypeOf((*access.OperatorSessions)(nil)).Elem(),
		reflect.TypeOf((*organizations.Repository)(nil)).Elem(),
		reflect.TypeOf((*catalog.Repository)(nil)).Elem(),
		reflect.TypeOf((*connections.OAuthRepository)(nil)).Elem(),
		reflect.TypeOf((*execution.FileStore)(nil)).Elem(),
		reflect.TypeOf((*delivery.WorkQueue)(nil)).Elem(),
		reflect.TypeOf((*triggers.PollQueue)(nil)).Elem(),
		reflect.TypeOf((*connections.RefreshQueue)(nil)).Elem(),
		reflect.TypeOf((*connections.StatusCounter)(nil)).Elem(),
		reflect.TypeOf((*delivery.OutboxStats)(nil)).Elem(),
	}
	for _, ifaceType := range whitelisted {
		if ifaceType.NumMethod() == 0 {
			t.Fatalf("%s has no methods — is this port still in use? remove the whitelist entry if not", ifaceType)
		}
	}
}
