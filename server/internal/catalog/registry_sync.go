// Phase 5 registry sub-phase, Slice 1 (walking skeleton): the
// installation-side half of pull -> activate -> serve. Publishing and
// minting tool_ ids happens in the separate registry service
// (internal/registryservice); this file only pulls a bundle the registry
// already published, persists it as this installation's activated
// definition (PD65), and swaps it into what Facade serves.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"beecon/internal/registrybundle"
)

// supportedBundleFormatVersion is the only bundle formatVersion this
// installation build accepts (Slice 4, PD66/AC2): ADR-0012 keeps the
// registry's own definition format at formatVersion: 1 — the same value
// registryservice's own publish-time gate enforces. An installation that
// pulls a bundle at any other formatVersion refuses to activate it rather
// than serve tools/triggers it cannot correctly interpret.
const supportedBundleFormatVersion = 1

// Activate pulls providerSlug's bundle at version from the registry
// (PD64), verifies it before touching anything this installation currently
// serves (Slice 4, PD66/PD67 — formatVersion support, content-hash
// integrity, and basic shape), persists it as this installation's activated
// definition (PD65, DB-backed, survives restart with no redeploy), and
// swaps it into the definitions this facade serves — the installation's
// catalog serves the newly activated version's tools and triggers without a
// redeploy (Slice 1's AC). If anything after the swap fails (Slice 4:
// pausing dependent trigger-instances for a removed trigger), the swap and
// its persisted row are rolled back to exactly what they were before this
// call — a failed activation never leaves a half-applied state (PD66). A
// tool the new version no longer declares is carried forward as deprecated
// rather than dropped (PD66), and existing connections (which reference a
// provider by its stable slug/IntegrationID, never by definition version)
// are never touched by any of this. Returns ErrRegistryNotConfigured when
// this facade was never given a RegistryClient (WithRegistrySync, PD59: a
// pinned installation with no BEECON_REGISTRY_URL runs fully offline).
// Persisting the activated definition is best-effort skipped (not failed)
// when this facade has no ActivatedDefinitions store wired — the in-memory
// swap still happens, which is enough for a facade a test wires with only a
// RegistryClient.
func (f *Facade) Activate(ctx context.Context, providerSlug, version string) (ActivatedVersion, error) {
	if f.registryClient == nil {
		return ActivatedVersion{}, ErrRegistryNotConfigured()
	}

	pulled, err := f.registryClient.FetchBundle(ctx, providerSlug, version)
	if err != nil {
		return ActivatedVersion{}, err
	}
	if err := validateBundleForActivation(pulled); err != nil {
		return ActivatedVersion{}, err
	}

	previousDefinition, hadPreviousDefinition := f.definitionByProviderSlug(providerSlug)
	previousActivated, err := f.currentActivatedRow(ctx, providerSlug)
	if err != nil {
		return ActivatedVersion{}, err
	}

	bundle := withCarriedOverDeprecatedTools(pulled, previousDefinition, hadPreviousDefinition)

	if f.activatedDefinitions != nil {
		if err := f.persistActivatedDefinition(ctx, providerSlug, bundle); err != nil {
			return ActivatedVersion{}, err
		}
	}
	f.setDefinition(definitionFromBundle(bundle))

	if err := f.pauseInstancesForRemovedTriggers(ctx, previousDefinition, hadPreviousDefinition, bundle); err != nil {
		f.rollbackActivation(ctx, providerSlug, previousDefinition, hadPreviousDefinition, previousActivated)
		return ActivatedVersion{}, err
	}

	return ActivatedVersion{ProviderSlug: providerSlug, ActiveVersion: bundle.Version}, nil
}

// validateBundleForActivation is Slice 4's pre-flight gate (PD66/PD67): a
// bundle that fails any of these checks is refused before Activate touches
// anything this installation currently serves or persists, so the
// previously active version stays fully in force with no partial swap.
func validateBundleForActivation(bundle registrybundle.Bundle) error {
	if bundle.FormatVersion != supportedBundleFormatVersion {
		return ErrUnsupportedFormatVersion(bundle.FormatVersion)
	}
	recomputed, err := registrybundle.ContentHash(bundle)
	if err != nil {
		return err
	}
	if bundle.ContentHash == "" || recomputed != bundle.ContentHash {
		return ErrContentHashMismatch()
	}
	return validateBundleShape(bundle)
}

// validateBundleShape is a defense-in-depth structural check (Slice 4's
// "fails format/schema validation" half of AC1): the registry's own
// publish-time gate (registryservice, PD63) already enforces this, but a
// pulled bundle is untrusted input on this side of the wire too — every
// tool must carry a slug and both schemas, every trigger a slug and both
// schemas, or activation is refused rather than serving a tool/trigger this
// installation could never execute or validate correctly.
func validateBundleShape(bundle registrybundle.Bundle) error {
	for i, tool := range bundle.Tools {
		prefix := fmt.Sprintf("tools[%d]", i)
		if tool.Slug == "" {
			return ErrValidation(prefix+".slug", "must not be empty")
		}
		if len(tool.InputSchema) == 0 {
			return ErrValidation(prefix+".inputSchema", "must not be empty")
		}
		if len(tool.OutputSchema) == 0 {
			return ErrValidation(prefix+".outputSchema", "must not be empty")
		}
	}
	for i, trigger := range bundle.Triggers {
		prefix := fmt.Sprintf("triggers[%d]", i)
		if trigger.Slug == "" {
			return ErrValidation(prefix+".slug", "must not be empty")
		}
		if len(trigger.ConfigSchema) == 0 {
			return ErrValidation(prefix+".configSchema", "must not be empty")
		}
		if len(trigger.PayloadSchema) == 0 {
			return ErrValidation(prefix+".payloadSchema", "must not be empty")
		}
	}
	return nil
}

// currentActivatedRow returns providerSlug's persisted ActivatedDefinition
// row exactly as it stood before this Activate call touches anything, or
// nil when this provider has never been activated through the registry
// before (or this facade has no ActivatedDefinitions store wired) —
// rollbackActivation's own restore-or-delete decision.
func (f *Facade) currentActivatedRow(ctx context.Context, providerSlug string) (*ActivatedDefinition, error) {
	if f.activatedDefinitions == nil {
		return nil, nil
	}
	return f.activatedDefinitions.FindByProviderSlug(ctx, providerSlug)
}

// rollbackActivation restores providerSlug's served definition and
// persisted activated-definition row to exactly what they were before this
// Activate call started (Slice 4, PD66): called only after the definition
// has already been persisted and swapped, when a later step (pausing
// dependent trigger-instances) then fails — so the whole activation must
// not be allowed to stick. previousActivated is the row read before this
// call touched anything; nil means this provider had never been activated
// through the registry before, so rolling back means removing the row this
// call just wrote, not restoring an earlier one. Best-effort: a failure to
// roll back is logged rather than compounding the error already being
// returned to the caller.
func (f *Facade) rollbackActivation(ctx context.Context, providerSlug string, previousDefinition ProviderDefinition, hadPreviousDefinition bool, previousActivated *ActivatedDefinition) {
	if hadPreviousDefinition {
		f.setDefinition(previousDefinition)
	} else {
		f.deleteDefinition(providerSlug)
	}
	if f.activatedDefinitions == nil {
		return
	}
	var err error
	if previousActivated != nil {
		err = f.activatedDefinitions.Save(ctx, *previousActivated)
	} else {
		err = f.activatedDefinitions.Delete(ctx, providerSlug)
	}
	if err != nil {
		slog.Default().Error("activation rollback failed to restore the previously activated definition row",
			"provider", providerSlug, "error", err)
	}
}

// pauseInstancesForRemovedTriggers calls the TriggerInstancePauser port once
// per trigger slug bundle no longer declares that previousDefinition did
// (Slice 4, PD66) — never for a provider's very first activation
// (hadPreviousDefinition false), since there is nothing yet to diff
// against, and never at all when no pauser is wired (a facade under test
// that doesn't exercise removed triggers, or an installation not yet wired
// with one).
func (f *Facade) pauseInstancesForRemovedTriggers(ctx context.Context, previousDefinition ProviderDefinition, hadPreviousDefinition bool, bundle registrybundle.Bundle) error {
	if f.triggerInstancePauser == nil || !hadPreviousDefinition {
		return nil
	}
	for _, slug := range removedTriggerSlugs(previousDefinition, bundle) {
		if err := f.triggerInstancePauser.PauseInstancesForRemovedTrigger(ctx, slug); err != nil {
			return err
		}
	}
	return nil
}

// removedTriggerSlugs returns every trigger slug previousDefinition declared
// that bundle no longer does.
func removedTriggerSlugs(previousDefinition ProviderDefinition, bundle registrybundle.Bundle) []string {
	stillDeclared := make(map[string]bool, len(bundle.Triggers))
	for _, t := range bundle.Triggers {
		stillDeclared[t.Slug] = true
	}
	var removed []string
	for _, t := range previousDefinition.Triggers {
		if !stillDeclared[t.Slug] {
			removed = append(removed, t.Slug)
		}
	}
	return removed
}

// withCarriedOverDeprecatedTools returns a copy of bundle with every tool
// previousDefinition was serving, but bundle no longer declares, appended
// back in as deprecated (Slice 4, PD66: "soft-deprecated, still resolvable"
// rather than hard-deleted) — folded into the very bundle this installation
// persists and serves, so a restart's LoadActivatedDefinitions rebuild
// reproduces the identical accumulated deprecated-tool set with no
// special-casing at boot (the persisted row IS the served definition,
// always, PD65). hadPreviousDefinition false (a provider's very first
// activation) has nothing yet to carry over.
func withCarriedOverDeprecatedTools(bundle registrybundle.Bundle, previousDefinition ProviderDefinition, hadPreviousDefinition bool) registrybundle.Bundle {
	if !hadPreviousDefinition {
		return bundle
	}
	stillDeclared := make(map[string]bool, len(bundle.Tools))
	for _, t := range bundle.Tools {
		stillDeclared[t.Slug] = true
	}
	var carried []registrybundle.Tool
	for _, tool := range previousDefinition.Tools {
		if stillDeclared[tool.Slug] {
			continue
		}
		carried = append(carried, bundleToolFromCarriedOverProviderTool(tool))
	}
	if len(carried) == 0 {
		return bundle
	}
	merged := bundle
	merged.Tools = append(append([]registrybundle.Tool{}, bundle.Tools...), carried...)
	return merged
}

// bundleToolFromCarriedOverProviderTool converts a ProviderTool this
// installation was already serving (Activate's previousDefinition) into the
// registrybundle.Tool wire shape, so it can be folded back into the bundle
// this installation persists and serves going forward — the reverse of
// toolsFromBundle/mappingFromBundle/paginationFromBundle, used only for this
// one purpose. Always marked deprecated, regardless of whatever it carried
// before: being carried over is itself the deprecation event.
func bundleToolFromCarriedOverProviderTool(tool ProviderTool) registrybundle.Tool {
	return registrybundle.Tool{
		ID:           tool.ID,
		Slug:         tool.Slug,
		Name:         tool.Name,
		Description:  tool.Description,
		Deprecated:   true,
		InputSchema:  tool.InputSchema,
		OutputSchema: tool.OutputSchema,
		Mapping: registrybundle.ToolMapping{
			Method:     tool.Method,
			Path:       tool.Path,
			Query:      tool.Mapping.Query,
			Header:     tool.Mapping.Header,
			Body:       tool.Mapping.Body,
			Pagination: bundlePaginationFromCarriedOverMapping(tool.Mapping.Pagination),
			FileInputs: tool.Mapping.FileInputs,
		},
	}
}

func bundlePaginationFromCarriedOverMapping(p *Pagination) *registrybundle.Pagination {
	if p == nil {
		return nil
	}
	return &registrybundle.Pagination{
		PageSizeParam:  p.PageSizeParam,
		CursorParam:    p.CursorParam,
		NextCursorPath: p.NextCursorPath,
	}
}

func (f *Facade) persistActivatedDefinition(ctx context.Context, providerSlug string, bundle registrybundle.Bundle) error {
	encoded, err := encodeBundleJSON(bundle)
	if err != nil {
		return err
	}
	record := ActivatedDefinition{
		ProviderSlug: providerSlug,
		Version:      bundle.Version,
		ContentHash:  bundle.ContentHash,
		BundleJSON:   encoded,
		ActivatedAt:  f.now(),
	}
	return f.activatedDefinitions.Save(ctx, record)
}

// LoadActivatedDefinitions rebuilds this installation's served definitions
// from the DB-backed activated-definition store (PD65): called once at
// boot (app/wiring.go, mirroring EncryptPlaintextClientSecrets' own
// boot-backfill convention), after the embedded-seed definitions passed to
// NewFacade are already loaded. Each activated row overrides its
// provider's embedded seed by slug — the DB store is the source of truth
// once a provider has ever been activated (PD65) — so a previously
// activated provider keeps serving its activated version across a
// restart, with no registry reachability required at all (PD59). A
// facade with no ActivatedDefinitions wired (WithRegistrySync never
// called) treats this as a no-op.
func (f *Facade) LoadActivatedDefinitions(ctx context.Context) error {
	if f.activatedDefinitions == nil {
		return nil
	}
	activated, err := f.activatedDefinitions.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, record := range activated {
		bundle, err := decodeBundleJSON(record.BundleJSON)
		if err != nil {
			return fmt.Errorf("decode activated definition for %s: %w", record.ProviderSlug, err)
		}
		f.setDefinition(definitionFromBundle(bundle))
	}
	return nil
}

// definitionFromBundle converts a registry-pulled Bundle into the domain
// ProviderDefinition catalog serves everywhere else — a direct
// field-for-field mapping, since registrybundle.Bundle mirrors
// ProviderDefinition/ProviderTool/TriggerDefinition exactly (by design, so
// this conversion never has to guess).
func definitionFromBundle(bundle registrybundle.Bundle) ProviderDefinition {
	return ProviderDefinition{
		Slug:            bundle.ProviderSlug,
		Name:            bundle.Name,
		Logo:            bundle.Logo,
		AuthScheme:      bundle.AuthScheme,
		BaseURL:         bundle.BaseURL,
		AuthorizeURL:    bundle.OAuth.AuthorizeURL,
		TokenURL:        bundle.OAuth.TokenURL,
		UserInfoURL:     bundle.OAuth.UserInfoURL,
		Scopes:          bundle.OAuth.Scopes,
		CredentialStyle: bundle.OAuth.CredentialStyle,
		UserInfo: UserInfoMapping{
			EmailField:       bundle.OAuth.UserInfoEmailField,
			DisplayNameField: bundle.OAuth.UserInfoDisplayNameField,
		},
		ExpectedParams: expectedParamsFromBundle(bundle.ExpectedParams),
		Tools:          toolsFromBundle(bundle.Tools),
		Triggers:       triggersFromBundle(bundle.Triggers),
	}
}

func expectedParamsFromBundle(params []registrybundle.ExpectedParam) []ExpectedParam {
	converted := make([]ExpectedParam, 0, len(params))
	for _, p := range params {
		converted = append(converted, ExpectedParam{
			Name:        p.Name,
			DisplayName: p.DisplayName,
			Description: p.Description,
			Required:    p.Required,
			Secret:      p.Secret,
		})
	}
	return converted
}

func toolsFromBundle(tools []registrybundle.Tool) []ProviderTool {
	converted := make([]ProviderTool, 0, len(tools))
	for _, t := range tools {
		converted = append(converted, ProviderTool{
			ID:           t.ID,
			Slug:         t.Slug,
			Name:         t.Name,
			Description:  t.Description,
			Method:       t.Mapping.Method,
			Path:         t.Mapping.Path,
			InputSchema:  t.InputSchema,
			OutputSchema: t.OutputSchema,
			Deprecated:   t.Deprecated,
			Mapping:      mappingFromBundle(t.Mapping),
		})
	}
	return converted
}

func mappingFromBundle(m registrybundle.ToolMapping) Mapping {
	return Mapping{
		Query:      m.Query,
		Header:     m.Header,
		Body:       m.Body,
		Pagination: paginationFromBundle(m.Pagination),
		FileInputs: m.FileInputs,
	}
}

func paginationFromBundle(p *registrybundle.Pagination) *Pagination {
	if p == nil {
		return nil
	}
	return &Pagination{
		PageSizeParam:  p.PageSizeParam,
		CursorParam:    p.CursorParam,
		NextCursorPath: p.NextCursorPath,
	}
}

func triggersFromBundle(triggers []registrybundle.Trigger) []TriggerDefinition {
	converted := make([]TriggerDefinition, 0, len(triggers))
	for _, t := range triggers {
		converted = append(converted, TriggerDefinition{
			Slug:                t.Slug,
			Name:                t.Name,
			Description:         t.Description,
			ConfigSchema:        t.ConfigSchema,
			PayloadSchema:       t.PayloadSchema,
			Ingestion:           t.Ingestion,
			PollIntervalSeconds: t.PollIntervalSeconds,
			Poll: TriggerPollMapping{
				Method:              t.Poll.Method,
				Path:                t.Poll.Path,
				Query:               t.Poll.Query,
				Body:                t.Poll.Body,
				RecordsPath:         t.Poll.RecordsPath,
				RecordIDPath:        t.Poll.RecordIDPath,
				RecordTimestampPath: t.Poll.RecordTimestampPath,
				Payload:             t.Poll.Payload,
			},
		})
	}
	return converted
}

func encodeBundleJSON(bundle registrybundle.Bundle) (string, error) {
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeBundleJSON(raw string) (registrybundle.Bundle, error) {
	var bundle registrybundle.Bundle
	if err := json.Unmarshal([]byte(raw), &bundle); err != nil {
		return registrybundle.Bundle{}, err
	}
	return bundle, nil
}
