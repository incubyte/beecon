// Package catalog_test (continues definition_test.go's package and helpers:
// validDefinitionYAML, mapFSWithFile, yamlWithoutLineContaining). This file
// covers the finalized definition format's v1-specific shape (PD13, slice 1):
// the full mapping block (baseUrl, path/method/query/header/body,
// pagination, deprecated), formatVersion dispatch failures naming the found
// and supported versions, per-tool input/output schema requirements, strict
// (KnownFields) YAML rejecting typos, and the reserved triggers key being
// accepted and ignored.
package catalog_test

import (
	"strings"
	"testing"

	"beecon/internal/catalog"
)

const validV1MappingYAML = `
formatVersion: 1
slug: outlook
name: Outlook
logo: https://static.beecon.dev/providers/outlook.png
authScheme: oauth2
oauth:
  authorizeUrl: https://example.com/authorize
  tokenUrl: https://example.com/token
  scopes:
    - offline_access
mapping:
  baseUrl: https://graph.microsoft.com/v1.0
tools:
  - slug: outlook-get-message
    name: Get email message
    description: Retrieves a message by its id.
    deprecated: true
    inputSchema:
      type: object
      properties:
        messageId:
          type: string
    outputSchema:
      type: object
      properties:
        id:
          type: string
    mapping:
      method: GET
      path: /me/messages/{input.messageId}
      query:
        $select: "{input.select}"
      header:
        Prefer: "{input.preference}"
      body:
        note: "{input.note}"
      pagination:
        pageSizeParam: top
        cursorParam: skip
        nextCursorPath: "@odata.nextLink"
`

func TestLoadProviderDefinitions_ParsesTheFullMappingBlockPathMethodQueryHeaderBodyPaginationAndDeprecated(t *testing.T) {
	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", validV1MappingYAML))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := defs[0]
	if d.BaseURL != "https://graph.microsoft.com/v1.0" {
		t.Errorf("BaseURL = %q, want %q", d.BaseURL, "https://graph.microsoft.com/v1.0")
	}
	if len(d.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(d.Tools))
	}
	tool := d.Tools[0]
	if tool.Method != "GET" {
		t.Errorf("Method = %q, want %q", tool.Method, "GET")
	}
	if tool.Path != "/me/messages/{input.messageId}" {
		t.Errorf("Path = %q, want %q", tool.Path, "/me/messages/{input.messageId}")
	}
	if !tool.Deprecated {
		t.Error("Deprecated = false, want true")
	}
	if got := tool.Mapping.Query["$select"]; got != "{input.select}" {
		t.Errorf("Mapping.Query[$select] = %q, want %q", got, "{input.select}")
	}
	if got := tool.Mapping.Header["Prefer"]; got != "{input.preference}" {
		t.Errorf("Mapping.Header[Prefer] = %q, want %q", got, "{input.preference}")
	}
	if got := tool.Mapping.Body["note"]; got != "{input.note}" {
		t.Errorf("Mapping.Body[note] = %q, want %q", got, "{input.note}")
	}
	if tool.Mapping.Pagination == nil {
		t.Fatal("Mapping.Pagination = nil, want the declared pagination block")
	}
	if tool.Mapping.Pagination.PageSizeParam != "top" {
		t.Errorf("Pagination.PageSizeParam = %q, want %q", tool.Mapping.Pagination.PageSizeParam, "top")
	}
	if tool.Mapping.Pagination.CursorParam != "skip" {
		t.Errorf("Pagination.CursorParam = %q, want %q", tool.Mapping.Pagination.CursorParam, "skip")
	}
	if tool.Mapping.Pagination.NextCursorPath != "@odata.nextLink" {
		t.Errorf("Pagination.NextCursorPath = %q, want %q", tool.Mapping.Pagination.NextCursorPath, "@odata.nextLink")
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileFoundVersionAndSupportedVersionWhenFormatVersionIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "formatVersion:")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: formatVersion 0 is not supported (supported: 1)`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileFoundVersionAndSupportedVersionWhenFormatVersionIsUnsupported(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "formatVersion: 1", "formatVersion: 2", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: formatVersion 2 is not supported (supported: 1)`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAToolIsMissingItsInputSchema(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "    inputSchema:\n      type: object\n", "", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "tools[0].inputSchema" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAToolIsMissingItsOutputSchema(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "    outputSchema:\n      type: object\n", "", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "tools[0].outputSchema" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenMappingBaseURLIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "baseUrl:")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "mapping.baseUrl" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAToolsMappingMethodIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "method: GET")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "tools[0].mapping.method" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAToolsMappingPathIsMissing(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML, "path: /v1.0/me/messages")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "tools[0].mapping.path" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// TestLoadProviderDefinitions_RejectsAnUnknownTopLevelKey proves strict
// (KnownFields) YAML decoding: a typo'd top-level field must fail boot
// instead of silently being ignored.
func TestLoadProviderDefinitions_RejectsAnUnknownTopLevelKey(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "slug: outlook\n", "slug: outlook\nslugg: outlook\n", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	if err == nil {
		t.Fatal("expected an error for the unknown top-level field \"slugg\", got nil")
	}
	if !strings.Contains(err.Error(), "slugg") {
		t.Errorf("error = %q, want it to name the unknown field %q", err.Error(), "slugg")
	}
}

// TestLoadProviderDefinitions_RejectsAnUnknownKeyNestedInsideATool proves
// strict decoding applies recursively, not just at the top level.
func TestLoadProviderDefinitions_RejectsAnUnknownKeyNestedInsideATool(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML,
		"    mapping:\n      method: GET\n",
		"    notAField: typo\n    mapping:\n      method: GET\n", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	if err == nil {
		t.Fatal("expected an error for the unknown tool-level field \"notAField\", got nil")
	}
	if !strings.Contains(err.Error(), "notAField") {
		t.Errorf("error = %q, want it to name the unknown field %q", err.Error(), "notAField")
	}
}

// validTriggersYAML is the reserved triggers key made real (Phase 3, Slice
// 1, PD28): one poll-ingestion trigger with both required schemas and a
// complete poll mapping.
const validTriggersYAML = `
triggers:
  - slug: outlook-message-received
    name: New message received
    description: Triggered when a new message arrives in the configured folder.
    ingestion: poll
    pollIntervalSeconds: 60
    configSchema:
      type: object
      properties:
        folderId:
          type: string
          default: Inbox
    payloadSchema:
      type: object
      properties:
        id:
          type: string
    poll:
      method: GET
      path: /me/mailFolders/{config.folderId}/messages
      query:
        $filter: "receivedDateTime gt {watermark}"
      recordsPath: value
      recordIdPath: id
      recordTimestampPath: receivedDateTime
      payload:
        id: id
`

// TestLoadProviderDefinitions_ParsesTriggersSlugNameSchemasIngestionAndPollMapping
// is Slice 1, AC1/AC2: the reserved triggers key (PD13) becomes a real,
// parsed trigger definition carrying config/payload schemas, ingestion mode,
// and the poll mapping (parsed and validated here; first executed in Slice
// 4).
func TestLoadProviderDefinitions_ParsesTriggersSlugNameSchemasIngestionAndPollMapping(t *testing.T) {
	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", validDefinitionYAML+validTriggersYAML))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs[0].Triggers) != 1 {
		t.Fatalf("len(Triggers) = %d, want 1", len(defs[0].Triggers))
	}
	trigger := defs[0].Triggers[0]
	if trigger.Slug != "outlook-message-received" {
		t.Errorf("Slug = %q, want %q", trigger.Slug, "outlook-message-received")
	}
	if trigger.Name != "New message received" {
		t.Errorf("Name = %q, want %q", trigger.Name, "New message received")
	}
	if trigger.Ingestion != "poll" {
		t.Errorf("Ingestion = %q, want %q", trigger.Ingestion, "poll")
	}
	if trigger.PollIntervalSeconds != 60 {
		t.Errorf("PollIntervalSeconds = %d, want 60", trigger.PollIntervalSeconds)
	}
	if len(trigger.ConfigSchema) == 0 {
		t.Error("ConfigSchema is empty, want the parsed folderId schema")
	}
	if len(trigger.PayloadSchema) == 0 {
		t.Error("PayloadSchema is empty, want the parsed id schema")
	}
	if trigger.Poll.Method != "GET" || trigger.Poll.Path != "/me/mailFolders/{config.folderId}/messages" {
		t.Errorf("Poll method/path = %q %q, want GET /me/mailFolders/{config.folderId}/messages", trigger.Poll.Method, trigger.Poll.Path)
	}
	if trigger.Poll.RecordsPath != "value" || trigger.Poll.RecordIDPath != "id" || trigger.Poll.RecordTimestampPath != "receivedDateTime" {
		t.Errorf("Poll records/id/timestamp paths = %+v, want value/id/receivedDateTime", trigger.Poll)
	}
	if trigger.Poll.Payload["id"] != "id" {
		t.Errorf(`Poll.Payload["id"] = %q, want "id"`, trigger.Poll.Payload["id"])
	}
}

// TestLoadProviderDefinitions_DefaultsPollIntervalSecondsTo60WhenOmitted is
// PD28: the Membrane sample's own default cadence.
func TestLoadProviderDefinitions_DefaultsPollIntervalSecondsTo60WhenOmitted(t *testing.T) {
	withoutInterval := yamlWithoutLineContaining(validDefinitionYAML+validTriggersYAML, "pollIntervalSeconds:")

	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", withoutInterval))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs[0].Triggers[0].PollIntervalSeconds != 60 {
		t.Errorf("PollIntervalSeconds = %d, want default 60", defs[0].Triggers[0].PollIntervalSeconds)
	}
}

// TestLoadProviderDefinitions_ClampsPollIntervalSecondsBelowThePlatformMinimum
// is PD28's floor: a declared interval under the platform minimum is raised
// to it rather than failing boot.
func TestLoadProviderDefinitions_ClampsPollIntervalSecondsBelowThePlatformMinimum(t *testing.T) {
	tooLow := strings.Replace(validDefinitionYAML+validTriggersYAML, "pollIntervalSeconds: 60", "pollIntervalSeconds: 5", 1)

	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", tooLow))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs[0].Triggers[0].PollIntervalSeconds != 30 {
		t.Errorf("PollIntervalSeconds = %d, want clamped to the platform minimum 30", defs[0].Triggers[0].PollIntervalSeconds)
	}
}

// TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggerIsMissingItsConfigSchema
// is AC4.
func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggerIsMissingItsConfigSchema(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML+validTriggersYAML,
		"    configSchema:\n      type: object\n      properties:\n        folderId:\n          type: string\n          default: Inbox\n",
		"", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].configSchema" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggerIsMissingItsPayloadSchema
// is AC4.
func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggerIsMissingItsPayloadSchema(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML+validTriggersYAML,
		"    payloadSchema:\n      type: object\n      properties:\n        id:\n          type: string\n",
		"", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].payloadSchema" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// TestLoadProviderDefinitions_FailsWithANotSupportedYetMessageWhenATriggerDeclaresPushIngestion
// is AC5.
func TestLoadProviderDefinitions_FailsWithANotSupportedYetMessageWhenATriggerDeclaresPushIngestion(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML+validTriggersYAML, "ingestion: poll", "ingestion: push", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].ingestion" "push" ingestion is not supported yet`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// TestLoadProviderDefinitions_FailsNamingTheFieldWhenATriggerDeclaresAnUnknownIngestionValue
// is AC4: any ingestion value other than "poll"/"push" fails boot naming the
// field.
func TestLoadProviderDefinitions_FailsNamingTheFieldWhenATriggerDeclaresAnUnknownIngestionValue(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML+validTriggersYAML, "ingestion: poll", "ingestion: sometimes", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].ingestion" must be "poll"`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// --- poll mapping (Slice 1, AC4): execution/poll.go (Slice 4) needs every
// one of these fields to actually poll and interpret a provider's response,
// so each is required field-precisely rather than left to fail at runtime. ---

// TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggersPollMappingIsMissingItsMethod
// removes only the trigger's poll.method line (not the tool's own
// mapping.method, which shares the literal text "method: GET") by matching
// the two-line poll.method/poll.path sequence together.
func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggersPollMappingIsMissingItsMethod(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML+validTriggersYAML,
		"      method: GET\n      path: /me/mailFolders/{config.folderId}/messages\n",
		"      path: /me/mailFolders/{config.folderId}/messages\n", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].poll.method" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggersPollMappingIsMissingItsPath(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML+validTriggersYAML,
		"      path: /me/mailFolders/{config.folderId}/messages\n      query:\n",
		"      query:\n", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].poll.path" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggersPollMappingIsMissingItsRecordsPath(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML+validTriggersYAML, "recordsPath:")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].poll.recordsPath" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggersPollMappingIsMissingItsRecordIDPath(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML+validTriggersYAML, "recordIdPath:")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].poll.recordIdPath" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggersPollMappingIsMissingItsRecordTimestampPath(t *testing.T) {
	invalid := yamlWithoutLineContaining(validDefinitionYAML+validTriggersYAML, "recordTimestampPath:")

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].poll.recordTimestampPath" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggersPollMappingIsMissingItsPayload(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML+validTriggersYAML,
		"      payload:\n        id: id\n", "", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].poll.payload" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggerIsMissingItsSlug
// and ...ItsName round out AC4's "invalid triggers section" matrix: these are
// checked before configSchema/payloadSchema/ingestion/poll, so they must fail
// independently rather than only ever being masked by a later check.
func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggerIsMissingItsSlug(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML+validTriggersYAML, "slug: outlook-message-received", "slug: ''", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].slug" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenATriggerIsMissingItsName(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML+validTriggersYAML, "name: New message received", "name: ''", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "triggers[0].name" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// --- credentialStyle / userInfo (PD13/PD16, Slice 2) ---

// TestLoadProviderDefinitions_DefaultsCredentialStyleToFormBodyWhenOmitted
// pins PD13's stated default: a definition that omits oauth.credentialStyle
// entirely (Outlook's shape, unchanged) must behave exactly as formBody.
func TestLoadProviderDefinitions_DefaultsCredentialStyleToFormBodyWhenOmitted(t *testing.T) {
	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", validDefinitionYAML))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs[0].CredentialStyle != catalog.CredentialStyleFormBody {
		t.Errorf("CredentialStyle = %q, want the default %q", defs[0].CredentialStyle, catalog.CredentialStyleFormBody)
	}
}

func TestLoadProviderDefinitions_ParsesAnExplicitlyDeclaredCredentialStyle(t *testing.T) {
	withStyle := `
formatVersion: 1
slug: hubspot
name: Hubspot
logo: https://static.beecon.dev/providers/hubspot.png
authScheme: oauth2
oauth:
  authorizeUrl: https://app.hubspot.com/oauth/authorize
  tokenUrl: https://api.hubapi.com/oauth/v1/token
  credentialStyle: basicAuth
  scopes:
    - crm.objects.contacts.read
mapping:
  baseUrl: https://api.hubapi.com
tools:
  - slug: hubspot-list-contacts
    name: List contacts
    description: List CRM contacts.
    inputSchema:
      type: object
    outputSchema:
      type: object
    mapping:
      method: GET
      path: /crm/v3/objects/contacts
`

	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("hubspot.yaml", withStyle))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs[0].CredentialStyle != catalog.CredentialStyleBasicAuth {
		t.Errorf("CredentialStyle = %q, want %q", defs[0].CredentialStyle, catalog.CredentialStyleBasicAuth)
	}
}

// TestLoadProviderDefinitions_RejectsAnInvalidCredentialStyle proves the enum
// is validated field-precisely rather than silently accepted or defaulted.
func TestLoadProviderDefinitions_RejectsAnInvalidCredentialStyle(t *testing.T) {
	invalid := strings.Replace(validDefinitionYAML, "oauth:\n", "oauth:\n  credentialStyle: not-a-real-style\n", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", invalid))

	wantMessage := `invalid provider definition outlook.yaml: field "oauth.credentialStyle" must be "formBody" or "basicAuth"`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// TestLoadProviderDefinitions_ParsesTheUserInfoEmailAndDisplayNameFieldMapping
// is PD13/PD16's userInfo mapping: which field of a provider's user-info/
// token-metadata response names the account's email and display name
// (Hubspot's differently-shaped "user"/"hub_domain" fields, proving this is
// generic rather than hardcoded to Outlook's "mail"/"displayName").
func TestLoadProviderDefinitions_ParsesTheUserInfoEmailAndDisplayNameFieldMapping(t *testing.T) {
	withUserInfo := strings.Replace(validDefinitionYAML,
		"oauth:\n  authorizeUrl:", "oauth:\n  userInfo:\n    email: user\n    displayName: hub_domain\n  authorizeUrl:", 1)

	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", withUserInfo))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs[0].UserInfo.EmailField != "user" {
		t.Errorf("UserInfo.EmailField = %q, want %q", defs[0].UserInfo.EmailField, "user")
	}
	if defs[0].UserInfo.DisplayNameField != "hub_domain" {
		t.Errorf("UserInfo.DisplayNameField = %q, want %q", defs[0].UserInfo.DisplayNameField, "hub_domain")
	}
}

// TestLoadProviderDefinitions_DefaultsUserInfoFieldsToEmptyWhenOmitted proves
// a definition with no userInfo block at all (not every provider needs one)
// leaves both fields empty rather than failing boot.
func TestLoadProviderDefinitions_DefaultsUserInfoFieldsToEmptyWhenOmitted(t *testing.T) {
	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", validDefinitionYAML))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs[0].UserInfo.EmailField != "" || defs[0].UserInfo.DisplayNameField != "" {
		t.Errorf("UserInfo = %+v, want both fields empty when the definition declares no userInfo block", defs[0].UserInfo)
	}
}

// --- expectedParams (PD13, Slice 3, AC1) ---

// validExpectedParamsYAML declares two pre-auth params: a required, non-secret
// "region" and a required, secret "apiKey" — the same shape
// fake_param_provider.go's fixture definition uses.
const validExpectedParamsYAML = `
formatVersion: 1
slug: fake-provider
name: Fake Provider
logo: https://static.beecon.dev/providers/fake-provider.png
authScheme: oauth2
oauth:
  authorizeUrl: https://example.com/authorize
  tokenUrl: https://example.com/token
  scopes:
    - read
mapping:
  baseUrl: https://example.com
expectedParams:
  - name: region
    displayName: Region
    description: Your account's region, e.g. eu or us.
    required: true
    secret: false
  - name: apiKey
    displayName: API Key
    description: Your account's API key.
    required: true
    secret: true
tools:
  - slug: fake-provider-tool
    name: Tool
    description: A tool.
    inputSchema:
      type: object
    outputSchema:
      type: object
    mapping:
      method: GET
      path: /tool
`

func TestLoadProviderDefinitions_ParsesExpectedParamsNameDisplayNameDescriptionRequiredAndSecret(t *testing.T) {
	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("fake-provider.yaml", validExpectedParamsYAML))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs[0].ExpectedParams) != 2 {
		t.Fatalf("len(ExpectedParams) = %d, want 2", len(defs[0].ExpectedParams))
	}
	region := defs[0].ExpectedParams[0]
	if region.Name != "region" || region.DisplayName != "Region" || region.Description != "Your account's region, e.g. eu or us." {
		t.Errorf("region param = %+v, want name/displayName/description parsed from the file", region)
	}
	if !region.Required {
		t.Error("region.Required = false, want true")
	}
	if region.Secret {
		t.Error("region.Secret = true, want false")
	}
	apiKey := defs[0].ExpectedParams[1]
	if apiKey.Name != "apiKey" || apiKey.DisplayName != "API Key" {
		t.Errorf("apiKey param = %+v, want name/displayName parsed from the file", apiKey)
	}
	if !apiKey.Required {
		t.Error("apiKey.Required = false, want true")
	}
	if !apiKey.Secret {
		t.Error("apiKey.Secret = false, want true")
	}
}

// TestLoadProviderDefinitions_DefaultsToNoExpectedParamsWhenOmitted proves a
// definition that declares no expectedParams block at all (Outlook, Hubspot —
// AC6) leaves the slice empty rather than failing boot.
func TestLoadProviderDefinitions_DefaultsToNoExpectedParamsWhenOmitted(t *testing.T) {
	defs, err := catalog.LoadProviderDefinitions(mapFSWithFile("outlook.yaml", validDefinitionYAML))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs[0].ExpectedParams) != 0 {
		t.Errorf("ExpectedParams = %+v, want empty when the definition declares no expectedParams block", defs[0].ExpectedParams)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAnExpectedParamIsMissingItsName(t *testing.T) {
	// Blanking the value (rather than dropping the whole line) keeps the
	// expectedParams entry's "- " YAML sequence-item marker intact.
	invalid := strings.Replace(validExpectedParamsYAML, "name: region", "name: ''", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("fake-provider.yaml", invalid))

	wantMessage := `invalid provider definition fake-provider.yaml: field "expectedParams[0].name" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenAnExpectedParamIsMissingItsDisplayName(t *testing.T) {
	invalid := strings.Replace(validExpectedParamsYAML, "displayName: Region", "displayName: ''", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("fake-provider.yaml", invalid))

	wantMessage := `invalid provider definition fake-provider.yaml: field "expectedParams[0].displayName" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}

// TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenTheSecondExpectedParamIsMissingItsName
// proves the index in the field-precise error names the specific entry that
// is invalid, not always the first.
func TestLoadProviderDefinitions_FailsNamingTheFileAndFieldWhenTheSecondExpectedParamIsMissingItsName(t *testing.T) {
	invalid := strings.Replace(validExpectedParamsYAML, "name: apiKey", "name: ''", 1)

	_, err := catalog.LoadProviderDefinitions(mapFSWithFile("fake-provider.yaml", invalid))

	wantMessage := `invalid provider definition fake-provider.yaml: field "expectedParams[1].name" must not be empty`
	if err == nil || err.Error() != wantMessage {
		t.Errorf("error = %v, want %q", err, wantMessage)
	}
}
