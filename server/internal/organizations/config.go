// This file holds Slice 9's (PD46) config export/import domain model and
// diff logic: the versioned ConfigDocument wire shape, the merge/replace
// reconciliation rules, and the pure functions that compute what an import
// would change. It is deliberately free of I/O (no context.Context, no
// port calls) — Facade.ExportConfig/ImportConfig (facade.go) are the thin
// orchestration layer that reads current state through GetGovernance/
// GetRetention/EndpointPorter and hands it to these pure functions, so the
// diff/plan logic itself is trivially unit-testable without a fake port.
package organizations

import "fmt"

// CurrentConfigSchemaVersion is the only schemaVersion GET
// .../config/export ever writes and POST .../config/import ever accepts
// (Slice 9, PD46). There is exactly one version so far; a future
// incompatible change to ConfigDocument's shape bumps this constant and
// ValidateConfigSchemaVersion is the seam that extends into a real
// version-migration table — not built ahead of any real second version
// (YAGNI).
const CurrentConfigSchemaVersion = 1

// ConfigDocument is Slice 9's versioned export/import document (PD46): an
// organization's governance, webhook endpoints (URL + event-type filter
// only — never a secret), and retention config, and nothing else. It is
// structurally incapable of carrying a secret, credential, connection, user
// token, or provider definition — there is no field for any of them.
type ConfigDocument struct {
	SchemaVersion int
	Governance    ConfigGovernance
	Endpoints     []ConfigEndpoint
	Retention     ConfigRetention
}

// ConfigGovernance is ConfigDocument's governance section: the same
// settable fields as Governance itself (AllowList/Hidden/Featured/
// FeaturedCap), carried as its own type so the export/import wire format
// doesn't also expose Governance's OrgID or its retention fields (those live
// in ConfigDocument's own Retention section instead).
type ConfigGovernance struct {
	AllowList   *[]string
	Hidden      []string
	Featured    []string
	FeaturedCap int
}

// ConfigEndpoint is one webhook endpoint in ConfigDocument's endpoints
// section (PD46): URL and its event-type filter only — never a secret,
// since GET .../config/export never has one to export, and POST
// .../config/import always mints a fresh one for any endpoint it creates.
type ConfigEndpoint struct {
	URL        string
	EventTypes []string
}

// ConfigRetention is ConfigDocument's retention section — the same
// tri-state pointers Governance itself carries (nil = inherit the
// installation default; 0 = unlimited/disabled).
type ConfigRetention struct {
	LogRetentionDays   *int
	EventRetentionDays *int
}

// ValidateConfigSchemaVersion rejects any schemaVersion other than
// CurrentConfigSchemaVersion (Slice 9's AC: "importing a document of an
// unknown or incompatible schema version is rejected with a validation
// error and writes nothing").
func ValidateConfigSchemaVersion(version int) error {
	if version != CurrentConfigSchemaVersion {
		return ErrUnsupportedSchemaVersion(version)
	}
	return nil
}

// ImportMode selects how ImportConfig reconciles the document against an
// org's existing governance/endpoints/retention (Slice 9, PD46).
type ImportMode string

const (
	// ImportModeMerge upserts the document's settings, leaving anything the
	// document does not mention untouched — the default (PD46).
	ImportModeMerge ImportMode = "merge"
	// ImportModeReplace makes governance/endpoints/retention match the
	// document exactly, removing whatever the document omits.
	ImportModeReplace ImportMode = "replace"
)

// normalizeImportMode defaults an absent mode to ImportModeMerge (PD46's
// "mode=merge (default)") and rejects anything other than the two known
// modes.
func normalizeImportMode(mode ImportMode) (ImportMode, error) {
	switch mode {
	case "":
		return ImportModeMerge, nil
	case ImportModeMerge, ImportModeReplace:
		return mode, nil
	default:
		return "", ErrValidation("mode", fmt.Sprintf("must be %q or %q", ImportModeMerge, ImportModeReplace))
	}
}

// ImportOptions is ImportConfig's caller-facing shape: DryRun defaults to
// true at the driving httpapi layer (PD46 — a missing/unspecified dryRun
// query param IS a dry-run), so this type carries no further defaulting of
// its own.
type ImportOptions struct {
	DryRun bool
	Mode   ImportMode
}

// ConfigChange is one line of ImportConfig's dry-run plan or apply result
// (Slice 9): Area names which part of the document it describes
// ("governance", "retention", or "endpoint"), Field names the specific
// setting (or, for an endpoint, its URL), Action is what would
// happen/happened ("set", "create", "update", or "delete"), and Detail is a
// short human-readable summary the Admin UI's diff preview renders
// verbatim.
type ConfigChange struct {
	Area   string
	Field  string
	Action string
	Detail string
}

// ImportedEndpointSecret is one freshly minted webhook signing secret an
// apply created (Slice 9, PD46: "endpoints created by an import get freshly
// generated secrets, shown once, since secrets are never exported").
type ImportedEndpointSecret struct {
	EndpointID string
	Secret     string
}

// ImportResult is ImportConfig's response: Plan/Warnings are populated for
// a dry-run (nothing was written); Applied/Secrets are populated for an
// apply. Warnings are computed the same way in both cases, but PD46's AC
// names them as specifically "reported in the dry-run" — the httpapi layer
// renders only Plan+Warnings for a dry-run and only Applied+Secrets for an
// apply.
type ImportResult struct {
	Plan     []ConfigChange
	Warnings []string
	Applied  []ConfigChange
	Secrets  []ImportedEndpointSecret
}

// resolveGovernanceUpdate computes the GovernanceUpdate ImportConfig would
// pass to Facade.SetGovernance (Slice 9): mode=replace takes the document's
// governance section exactly as given (mirroring SetGovernance's own
// whole-replace convention for its half of org_governance); mode=merge
// starts from existing and overlays only the fields the document actually
// sets — a nil AllowList, an empty Hidden/Featured, or a zero FeaturedCap
// all mean "not mentioned" and leave existing's own value alone.
func resolveGovernanceUpdate(existing Governance, doc ConfigGovernance, mode ImportMode) GovernanceUpdate {
	if mode == ImportModeReplace {
		return GovernanceUpdate{AllowList: doc.AllowList, Hidden: doc.Hidden, Featured: doc.Featured, FeaturedCap: doc.FeaturedCap}
	}
	update := GovernanceUpdate{AllowList: existing.AllowList, Hidden: existing.Hidden, Featured: existing.Featured, FeaturedCap: existing.FeaturedCap}
	if doc.AllowList != nil {
		update.AllowList = doc.AllowList
	}
	if len(doc.Hidden) > 0 {
		update.Hidden = doc.Hidden
	}
	if len(doc.Featured) > 0 {
		update.Featured = doc.Featured
	}
	if doc.FeaturedCap > 0 {
		update.FeaturedCap = doc.FeaturedCap
	}
	return update
}

// resolveRetentionUpdate is resolveGovernanceUpdate's mirror for the
// retention section: mode=replace takes the document's two pointers exactly
// (nil is itself a meaningful "inherit the installation default" value
// under replace, matching SetRetention's own existing whole-replace
// convention); mode=merge only overlays a pointer the document actually
// sets (non-nil), leaving existing's own value alone otherwise.
func resolveRetentionUpdate(existing RetentionView, doc ConfigRetention, mode ImportMode) RetentionUpdate {
	if mode == ImportModeReplace {
		return RetentionUpdate{LogRetentionDays: doc.LogRetentionDays, EventRetentionDays: doc.EventRetentionDays}
	}
	update := RetentionUpdate{LogRetentionDays: existing.LogRetentionDays, EventRetentionDays: existing.EventRetentionDays}
	if doc.LogRetentionDays != nil {
		update.LogRetentionDays = doc.LogRetentionDays
	}
	if doc.EventRetentionDays != nil {
		update.EventRetentionDays = doc.EventRetentionDays
	}
	return update
}

// describeGovernanceChanges reports, as ConfigChange lines, which resolved
// governance fields actually differ from existing — the shared diff step
// both the dry-run plan and the apply result render (no duplication between
// "what would change" and "what changed").
func describeGovernanceChanges(existing Governance, update GovernanceUpdate) []ConfigChange {
	var changes []ConfigChange
	if !allowListEqual(existing.AllowList, update.AllowList) {
		changes = append(changes, ConfigChange{Area: "governance", Field: "allowList", Action: "set", Detail: describeAllowList(update.AllowList)})
	}
	if !stringsEqual(existing.Hidden, update.Hidden) {
		changes = append(changes, ConfigChange{Area: "governance", Field: "hidden", Action: "set", Detail: fmt.Sprintf("%d integration(s) hidden", len(update.Hidden))})
	}
	effectiveCap := effectiveFeaturedCap(update.FeaturedCap)
	if !stringsEqual(existing.Featured, update.Featured) || existing.FeaturedCap != effectiveCap {
		changes = append(changes, ConfigChange{Area: "governance", Field: "onboarding", Action: "set", Detail: fmt.Sprintf("%d featured integration(s), cap %d", len(update.Featured), effectiveCap)})
	}
	return changes
}

func effectiveFeaturedCap(cap int) int {
	if cap <= 0 {
		return DefaultFeaturedCap
	}
	return cap
}

func describeAllowList(allowList *[]string) string {
	if allowList == nil {
		return "inherit the full installation catalog"
	}
	return fmt.Sprintf("%d integration(s) allow-listed", len(*allowList))
}

func allowListEqual(a, b *[]string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return stringsEqual(*a, *b)
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// describeRetentionChanges is describeGovernanceChanges' mirror for the
// retention section.
func describeRetentionChanges(existing RetentionView, update RetentionUpdate) []ConfigChange {
	var changes []ConfigChange
	if !intPtrEqual(existing.LogRetentionDays, update.LogRetentionDays) {
		changes = append(changes, ConfigChange{Area: "retention", Field: "logRetentionDays", Action: "set", Detail: describeRetentionDays(update.LogRetentionDays)})
	}
	if !intPtrEqual(existing.EventRetentionDays, update.EventRetentionDays) {
		changes = append(changes, ConfigChange{Area: "retention", Field: "eventRetentionDays", Action: "set", Detail: describeRetentionDays(update.EventRetentionDays)})
	}
	return changes
}

func describeRetentionDays(days *int) string {
	if days == nil {
		return "inherit the installation default"
	}
	if *days == 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d day(s)", *days)
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// endpointAction is one endpoint-level step ImportConfig's diff computes
// (Slice 9): "create"/"update" carry the document's own URL/EventTypes;
// "delete" (mode=replace only) carries the existing endpoint's id.
type endpointAction struct {
	Action     string
	URL        string
	EventTypes []string
	EndpointID string
}

// planEndpoints matches doc against existing by URL (an import document
// never carries an endpoint id — ids don't round-trip across installations,
// PD46) and reports the create/update/delete steps mode requires: a URL
// present only in doc is a create; a URL in both with a different
// EventTypes filter is an update; under mode=replace, an existing URL absent
// from doc is a delete. mode=merge never deletes — an org endpoint the
// document doesn't mention is left exactly as it is ("leaves unmentioned
// settings untouched", PD46).
func planEndpoints(existing []PortedEndpoint, doc []ConfigEndpoint, mode ImportMode) []endpointAction {
	existingByURL := make(map[string]PortedEndpoint, len(existing))
	for _, endpoint := range existing {
		existingByURL[endpoint.URL] = endpoint
	}
	mentioned := make(map[string]bool, len(doc))
	actions := make([]endpointAction, 0, len(doc))

	for _, endpoint := range doc {
		mentioned[endpoint.URL] = true
		current, ok := existingByURL[endpoint.URL]
		if !ok {
			actions = append(actions, endpointAction{Action: "create", URL: endpoint.URL, EventTypes: endpoint.EventTypes})
			continue
		}
		if !stringsEqual(current.EventTypes, endpoint.EventTypes) {
			actions = append(actions, endpointAction{Action: "update", URL: endpoint.URL, EventTypes: endpoint.EventTypes, EndpointID: current.ID})
		}
	}
	if mode == ImportModeReplace {
		for _, endpoint := range existing {
			if !mentioned[endpoint.URL] {
				actions = append(actions, endpointAction{Action: "delete", URL: endpoint.URL, EndpointID: endpoint.ID})
			}
		}
	}
	return actions
}

func describeEndpointChanges(actions []endpointAction) []ConfigChange {
	changes := make([]ConfigChange, 0, len(actions))
	for _, action := range actions {
		changes = append(changes, ConfigChange{Area: "endpoint", Field: action.URL, Action: action.Action, Detail: describeEndpointAction(action)})
	}
	return changes
}

func describeEndpointAction(action endpointAction) string {
	switch action.Action {
	case "create":
		return fmt.Sprintf("create endpoint %s (%s)", action.URL, describeEventTypeFilter(action.EventTypes))
	case "update":
		return fmt.Sprintf("update endpoint %s to %s", action.URL, describeEventTypeFilter(action.EventTypes))
	case "delete":
		return fmt.Sprintf("delete endpoint %s", action.URL)
	default:
		return action.URL
	}
}

func describeEventTypeFilter(eventTypes []string) string {
	if len(eventTypes) == 0 {
		return "all event types"
	}
	return fmt.Sprintf("%d event type(s)", len(eventTypes))
}

// referencedIntegrationIDs collects every integration id doc's governance
// section names — allow-listed, hidden, or featured — deduplicated, the set
// ImportConfig's dry-run checks for existence (Slice 9's AC: "integration
// ids that don't exist in this installation are reported ... not silently
// dropped").
func referencedIntegrationIDs(doc ConfigGovernance) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(values []string) {
		for _, id := range values {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if doc.AllowList != nil {
		add(*doc.AllowList)
	}
	add(doc.Hidden)
	add(doc.Featured)
	return ids
}

// combineConfigChanges assembles ImportConfig's single change list — the
// governance, then retention, then endpoint diff lines, in that order — the
// exact list both a dry-run's Plan and an apply's Applied render.
func combineConfigChanges(existingGovernance Governance, governanceUpdate GovernanceUpdate, existingRetention RetentionView, retentionUpdate RetentionUpdate, endpointActions []endpointAction) []ConfigChange {
	changes := describeGovernanceChanges(existingGovernance, governanceUpdate)
	changes = append(changes, describeRetentionChanges(existingRetention, retentionUpdate)...)
	changes = append(changes, describeEndpointChanges(endpointActions)...)
	return changes
}
