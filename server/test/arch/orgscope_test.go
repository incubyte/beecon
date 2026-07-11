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
	"beecon/internal/organizations"
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

func TestOrganizationsUserRepository_EveryMethodIsOrgScoped(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*organizations.UserRepository)(nil)).Elem())

	if len(got) != 0 {
		t.Fatalf("organizations.UserRepository has org-scope violations: %v", got)
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

// TestInstallationLevelPortsAreExplicitlyWhitelisted documents (and pins,
// via NumMethod, so a rename/removal is noticed) the ports deliberately
// exempted from org-scoping: access.PrefixLookup authenticates a secret
// before any organization is known — the lookup prefix is how a caller's
// organization is discovered in the first place; organizations.Repository
// operates on Organization itself, which IS the isolation unit with no wider
// scope to filter by; and catalog.Repository is installation-level by design
// (PD7: an Integration is visible to every organization in the
// installation) — there is no organization id to filter by.
func TestInstallationLevelPortsAreExplicitlyWhitelisted(t *testing.T) {
	whitelisted := []reflect.Type{
		reflect.TypeOf((*access.PrefixLookup)(nil)).Elem(),
		reflect.TypeOf((*organizations.Repository)(nil)).Elem(),
		reflect.TypeOf((*catalog.Repository)(nil)).Elem(),
	}
	for _, ifaceType := range whitelisted {
		if ifaceType.NumMethod() == 0 {
			t.Fatalf("%s has no methods — is this port still in use? remove the whitelist entry if not", ifaceType)
		}
	}
}
