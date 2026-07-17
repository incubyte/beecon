package membraneimport

import (
	"reflect"
	"strings"
	"testing"
)

// triggerRecordWithActionShapedInnerNode is a minimal, valid Membrane trigger
// flow record (own key, parametersSchema, and a data-record-created-trigger
// node with an outputSchema) whose node graph also carries a second node
// shaped exactly like a Membrane action (`type: api-request-to-external-app`,
// the same type string groupRecords recognizes for standalone action files) —
// the same node type the real Membrane export's own trigger flow carries
// further down its chain. It shares testdata/integration.yaml's
// integrationUuid so it groups into the same provider without needing its
// own integration record.
func triggerRecordWithActionShapedInnerNode() SourceFile {
	return SourceFile{Name: "trigger-with-action-node.yaml", Content: []byte(`
uuid: 9f000000-0000-4000-8000-000000000099
key: test-crm-record-created
name: Record Created
description: Fires when a new record is created in the CRM.
parametersSchema:
  type: object
  properties:
    recordId:
      type: string
nodes:
  record-created:
    type: data-record-created-trigger
    outputSchema:
      type: object
      properties:
        id:
          type: string
  call-external-app:
    type: api-request-to-external-app
    config:
      request:
        method: GET
        path: /records
integrationUuid: grp-test-crm-uuid
`)}
}

// triggerRecordMissingDataRecordCreatedNode is a trigger flow record that has
// its own key and parametersSchema but no data-record-created-trigger node
// anywhere in its node graph — the shape buildTrigger cannot build a
// payloadSchema from at all.
func triggerRecordMissingDataRecordCreatedNode() SourceFile {
	return SourceFile{Name: "trigger-missing-data-record-node.yaml", Content: []byte(`
uuid: 9f000000-0000-4000-8000-000000000098
key: test-crm-record-created
name: Record Created
description: Fires when a new record is created in the CRM.
parametersSchema:
  type: object
  properties:
    recordId:
      type: string
nodes:
  send-to-api:
    type: api-request-to-your-app
    config:
      request:
        uri: /api/flows/webhook
        method: POST
integrationUuid: grp-test-crm-uuid
`)}
}

// TestConvert_ClassifiesANodesGraphAsATriggerNotAnActionEvenWithAnActionShapedInnerNode
// verifies a Membrane record carrying a top-level `nodes:` map is always
// classified and converted as a trigger, never as an action — even when one
// of its inner nodes carries the exact node type
// (`api-request-to-external-app`) that identifies a standalone action file.
// No tool must ever be emitted from a trigger flow file.
func TestConvert_ClassifiesANodesGraphAsATriggerNotAnActionEvenWithAnActionShapedInnerNode(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		triggerRecordWithActionShapedInnerNode(),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	if len(def.Tools) != 0 {
		t.Errorf("Tools = %+v, want none — a nodes: record must never be emitted as a tool", def.Tools)
	}
	if len(def.Triggers) != 1 {
		t.Fatalf("len(Triggers) = %d, want 1", len(def.Triggers))
	}
	if def.Triggers[0].Slug != "test-crm-record-created" {
		t.Errorf("Triggers[0].Slug = %q, want %q", def.Triggers[0].Slug, "test-crm-record-created")
	}
}

// TestConvert_TriggerConfigSchemaPreservesTheParametersSchemaDefault verifies
// the Membrane trigger flow's own parametersSchema becomes the emitted
// trigger's configSchema, and that a property default declared there
// (folderId's default of Inbox) survives the conversion rather than being
// dropped.
func TestConvert_TriggerConfigSchemaPreservesTheParametersSchemaDefault(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "trigger-outlook-message-received.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	if len(def.Triggers) != 1 {
		t.Fatalf("len(Triggers) = %d, want 1", len(def.Triggers))
	}

	folderID := mapAt(def.Triggers[0].ConfigSchema, "properties", "folderId")
	if folderID == nil {
		t.Fatalf("configSchema.properties.folderId missing:\n%+v", def.Triggers[0].ConfigSchema)
	}
	if got := folderID["default"]; got != "Inbox" {
		t.Errorf("configSchema.properties.folderId.default = %v, want %q", got, "Inbox")
	}
}

// TestConvert_TriggerPayloadSchemaCarriesTheDataRecordCreatedTriggerNodeOutputFields
// verifies the emitted trigger's payloadSchema comes from the flow's own
// data-record-created-trigger node outputSchema (the record shape a fired
// trigger event will carry), not some other node's schema.
func TestConvert_TriggerPayloadSchemaCarriesTheDataRecordCreatedTriggerNodeOutputFields(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "trigger-outlook-message-received.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	if len(def.Triggers) != 1 {
		t.Fatalf("len(Triggers) = %d, want 1", len(def.Triggers))
	}

	properties := mapAt(def.Triggers[0].PayloadSchema, "properties")
	for _, field := range []string{"id", "subject", "receivedDateTime"} {
		if _, ok := properties[field]; !ok {
			t.Errorf("payloadSchema.properties missing %q (want the trigger node's own output fields): %+v", field, properties)
		}
	}
}

// TestConvert_TriggerPollFieldsAreExplicitTodoPlaceholdersNeverGuessedValues
// verifies every field of the emitted trigger's poll mapping is one of the
// package's own TODO sentinel constants — never a value guessed from the
// Membrane flow's abstract collectionKey (which has no concrete REST path,
// method, or record/timestamp paths to guess from).
func TestConvert_TriggerPollFieldsAreExplicitTodoPlaceholdersNeverGuessedValues(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "trigger-outlook-message-received.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	if len(def.Triggers) != 1 {
		t.Fatalf("len(Triggers) = %d, want 1", len(def.Triggers))
	}

	poll := def.Triggers[0].Poll
	if poll.Method != todoTriggerMethod {
		t.Errorf("Poll.Method = %q, want the TODO sentinel %q", poll.Method, todoTriggerMethod)
	}
	if poll.Path != todoTriggerPath {
		t.Errorf("Poll.Path = %q, want the TODO sentinel %q", poll.Path, todoTriggerPath)
	}
	if poll.RecordsPath != todoTriggerRecordsPath {
		t.Errorf("Poll.RecordsPath = %q, want the TODO sentinel %q", poll.RecordsPath, todoTriggerRecordsPath)
	}
	if poll.RecordIDPath != todoTriggerRecordIDPath {
		t.Errorf("Poll.RecordIDPath = %q, want the TODO sentinel %q", poll.RecordIDPath, todoTriggerRecordIDPath)
	}
	if poll.RecordTimestampPath != todoTriggerRecordTimestampPath {
		t.Errorf("Poll.RecordTimestampPath = %q, want the TODO sentinel %q", poll.RecordTimestampPath, todoTriggerRecordTimestampPath)
	}
	wantPayload := map[string]string{todoTriggerPayloadKey: todoTriggerPayloadValue}
	if !reflect.DeepEqual(poll.Payload, wantPayload) {
		t.Errorf("Poll.Payload = %+v, want the TODO sentinel %+v", poll.Payload, wantPayload)
	}
}

// TestConvert_ReportsTheTriggerAsPartialNamingAllSixMissingPollFields verifies
// the report lists the trigger under Partial (never Converted), identifies it
// by kind/provider/source, and names every one of the six poll fields a human
// must supply — method, path, recordsPath, recordIdPath, recordTimestampPath,
// and payload — not just a generic "needs review" note.
func TestConvert_ReportsTheTriggerAsPartialNamingAllSixMissingPollFields(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "trigger-outlook-message-received.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	report := string(result.Report)
	partialSection := reportSection(t, report, "Partial")
	for _, want := range []string{
		"trigger",                               // Kind
		"outlook-message-trigger",               // trigger slug (the flow's own key)
		"test-crm",                              // provider slug
		"trigger-outlook-message-received.yaml", // source file
	} {
		if !strings.Contains(partialSection, want) {
			t.Errorf("Partial section missing %q:\n%s", want, partialSection)
		}
	}
	for _, field := range missingTriggerPollFields {
		wanted := "poll." + field
		if !strings.Contains(partialSection, wanted) {
			t.Errorf("Partial section missing the caveat naming %q:\n%s", wanted, partialSection)
		}
	}

	convertedSection := reportSection(t, report, "Converted")
	if strings.Contains(convertedSection, "outlook-message-trigger") {
		t.Errorf("Converted section must never list the needs-human trigger:\n%s", convertedSection)
	}
}

// TestConvert_ARunWithANeedsHumanTriggerNeverFailsAndStillEmitsASiblingAction
// verifies a needs-human trigger never fails the whole run: an action sharing
// the same integrationUuid still converts cleanly and lands in the same
// emitted provider definition, alongside the trigger reported as needs-human.
func TestConvert_ARunWithANeedsHumanTriggerNeverFailsAndStillEmitsASiblingAction(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
		loadTestdataFile(t, "trigger-outlook-message-received.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	if len(def.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1 — the sibling action must still convert", len(def.Tools))
	}
	if def.Tools[0].Slug != "test-crm-get-record" {
		t.Errorf("Tools[0].Slug = %q, want %q", def.Tools[0].Slug, "test-crm-get-record")
	}
	if len(def.Triggers) != 1 {
		t.Fatalf("len(Triggers) = %d, want 1 — the needs-human trigger must still be emitted", len(def.Triggers))
	}

	report := string(result.Report)
	convertedSection := reportSection(t, report, "Converted")
	if !strings.Contains(convertedSection, "test-crm-get-record") {
		t.Errorf("Converted section missing the sibling action's tool slug:\n%s", convertedSection)
	}
	partialSection := reportSection(t, report, "Partial")
	if !strings.Contains(partialSection, "outlook-message-trigger") {
		t.Errorf("Partial section missing the needs-human trigger:\n%s", partialSection)
	}
}

// TestConvert_EmittedTriggerRoundTripsThroughTheRealLoaderWithTheDefaultPollInterval
// pins the round-trip anchor: even with every poll field a TODO placeholder,
// the emitted trigger definition still parses through the real
// catalog.LoadProviderDefinitions, and the loader's own default (60s) applies
// since the importer never emits a pollIntervalSeconds value.
func TestConvert_EmittedTriggerRoundTripsThroughTheRealLoaderWithTheDefaultPollInterval(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "trigger-outlook-message-received.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	def := loadSingleEmittedDefinition(t, result)
	if len(def.Triggers) != 1 {
		t.Fatalf("len(Triggers) = %d, want 1", len(def.Triggers))
	}
	trigger := def.Triggers[0]
	if trigger.Ingestion != "poll" {
		t.Errorf("Ingestion = %q, want %q", trigger.Ingestion, "poll")
	}
	if trigger.PollIntervalSeconds != 60 {
		t.Errorf("PollIntervalSeconds = %d, want the loader's default of 60", trigger.PollIntervalSeconds)
	}
}

// TestConvert_SkipsATriggerRecordWithNoDataRecordCreatedTriggerNodeAndRunContinues
// verifies a trigger record too incomplete to build (no
// data-record-created-trigger node anywhere in its graph, so there is no
// outputSchema to build a payloadSchema from) becomes a SkippedItem naming
// the reason, rather than a run failure or a silently invalid emission.
func TestConvert_SkipsATriggerRecordWithNoDataRecordCreatedTriggerNodeAndRunContinues(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		triggerRecordMissingDataRecordCreatedNode(),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}

	item, ok := findSkippedBySource(result.Skipped, "trigger-missing-data-record-node.yaml")
	if !ok {
		t.Fatalf("Skipped = %+v, want an entry naming %q", result.Skipped, "trigger-missing-data-record-node.yaml")
	}
	if !strings.Contains(item.Reason, dataRecordCreatedTriggerNodeType) {
		t.Errorf("Reason = %q, want it to name %q", item.Reason, dataRecordCreatedTriggerNodeType)
	}

	if len(result.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1 — the integration record alone still emits a provider file", len(result.Providers))
	}
}
