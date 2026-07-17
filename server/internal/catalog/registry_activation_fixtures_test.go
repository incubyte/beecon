// registry_activation_fixtures_test.go (package catalog_test) holds the
// bundle/tool/trigger builders shared by the atomicity, dependent-safety
// (deprecation, trigger-pause, connections-untouched), and in-flight-
// execution test files below — extracted once three or more of those files
// needed the same "build a minimal, content-hash-valid bundle with an
// explicit tool/trigger set" shape (DRY, third-occurrence rule). Every
// builder computes its own real registrybundle.ContentHash rather than a
// hand-typed placeholder, exactly like outlookBundleWithListMessagesTool and
// registryReviewBundle already do elsewhere in this package, because
// Activate verifies it on every call (Slice 4, PD67).
package catalog_test

import "beecon/internal/registrybundle"

// activationBundle builds a minimal, structurally valid "outlook" bundle at
// version, carrying exactly the tools and triggers supplied.
func activationBundle(version string, tools []registrybundle.Tool, triggers []registrybundle.Trigger) registrybundle.Bundle {
	return activationBundleForProvider("outlook", version, tools, triggers)
}

// activationBundleForProvider is activationBundle's general form, for tests
// that need a provider slug outside the embedded seed entirely (e.g. proving
// a genuinely first-ever activation never calls the trigger-instance
// pauser).
func activationBundleForProvider(providerSlug, version string, tools []registrybundle.Tool, triggers []registrybundle.Trigger) registrybundle.Bundle {
	bundle := registrybundle.Bundle{
		FormatVersion: 1,
		ProviderSlug:  providerSlug,
		Version:       version,
		Name:          providerSlug,
		AuthScheme:    "oauth2",
		OAuth: registrybundle.OAuthConfig{
			AuthorizeURL: "https://example.com/authorize",
			TokenURL:     "https://example.com/token",
		},
		Tools:    tools,
		Triggers: triggers,
	}
	bundle.ContentHash, _ = registrybundle.ContentHash(bundle)
	return bundle
}

// activationTool builds a minimal, schema-valid tool at the conventional
// "/v1.0/<slug>" path, with no immutable tool_ id set.
func activationTool(slug string) registrybundle.Tool {
	return versionedTool(slug, "/v1.0/"+slug)
}

// activationToolWithID is activationTool plus an explicit tool_ id, for tests
// that resolve a carried-over deprecated tool by id as well as by slug.
func activationToolWithID(slug, id string) registrybundle.Tool {
	tool := activationTool(slug)
	tool.ID = id
	return tool
}

// versionedTool builds a minimal, schema-valid tool at an explicit path — the
// in-flight-execution test uses this to give the same tool slug a
// version-distinguishing path across two activated bundles, so a request
// built against one version can never be mistaken for the other's.
func versionedTool(slug, path string) registrybundle.Tool {
	return registrybundle.Tool{
		Slug: slug, Name: slug,
		InputSchema:  minimalSchema(),
		OutputSchema: minimalSchema(),
		Mapping:      registrybundle.ToolMapping{Method: "GET", Path: path},
	}
}

// activationTrigger builds a minimal, schema-valid trigger definition.
func activationTrigger(slug string) registrybundle.Trigger {
	return registrybundle.Trigger{
		Slug: slug, Name: slug,
		ConfigSchema:  minimalSchema(),
		PayloadSchema: minimalSchema(),
		Ingestion:     "poll",
		Poll: registrybundle.TriggerPoll{
			Method: "GET", Path: "/v1.0/" + slug + "/poll",
			RecordsPath: "items", RecordIDPath: "id", RecordTimestampPath: "ts",
		},
	}
}
