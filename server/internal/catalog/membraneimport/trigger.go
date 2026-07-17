package membraneimport

import "fmt"

// dataRecordCreatedTriggerNodeType is the Membrane flow-graph node type that
// marks a trigger's own data-source node (Slice 5, AC1) — distinguishing it
// from the rest of the chain a trigger flow carries
// (find-data-record-by-id, transform-data, api-request-to-your-app).
//
// triggerIngestionPoll is the only ingestion mode the loader accepts
// (catalog's own triggerIngestionPoll constant is unexported, so this
// package names it again rather than importing an internal detail).
const (
	dataRecordCreatedTriggerNodeType = "data-record-created-trigger"
	triggerIngestionPoll             = "poll"
)

// missingTriggerPollFields names, in the order the spec's AC6 lists them,
// every poll-mapping field a Membrane trigger flow's abstract collectionKey
// cannot supply a concrete value for: the report names these exactly so a
// human knows what to fill in against the provider's real polling endpoint.
var missingTriggerPollFields = []string{
	"method", "path", "recordsPath", "recordIdPath", "recordTimestampPath", "payload",
}

// buildTrigger converts one Membrane trigger flow record's fields into a
// Beecon trigger definition. Membrane models a trigger's data source as an
// abstract collectionKey inside a multi-node flow graph, not a concrete REST
// poll, so the emitted poll mapping can never be more than a TODO
// placeholder for every field missingTriggerPollFields names. What DOES map
// cleanly: the flow's own parametersSchema becomes configSchema (preserving
// any property default, e.g. folderId's default Inbox), and the flow's
// data-record-created-trigger node's outputSchema becomes payloadSchema.
//
// The returned caveats always name every missing poll field — a Membrane
// trigger flow never supplies enough to complete the poll block, so
// buildProviderDefinition reports every successfully built trigger as
// Partial (needs-human) on that basis, never as cleanly Converted. An error
// means the trigger record itself is too incomplete to emit at all (missing
// key, parametersSchema, or a data-record-created-trigger node) — not seen
// in the sample set, but handled the same way an unconvertible action is:
// a SkippedItem, never a run failure.
func buildTrigger(fields map[string]any) (outputTriggerV1, []string, error) {
	slug := stringAt(fields, "key")
	if slug == "" {
		return outputTriggerV1{}, nil, fmt.Errorf("trigger record missing %q", "key")
	}

	configSchema := mapAt(fields, "parametersSchema")
	if len(configSchema) == 0 {
		return outputTriggerV1{}, nil, fmt.Errorf("trigger %q missing parametersSchema", slug)
	}

	payloadSchema := triggerNodeOutputSchema(fields)
	if len(payloadSchema) == 0 {
		return outputTriggerV1{}, nil, fmt.Errorf(
			"trigger %q has no %s node with an outputSchema", slug, dataRecordCreatedTriggerNodeType,
		)
	}

	return outputTriggerV1{
		Slug:          slug,
		Name:          stringAt(fields, "name"),
		Description:   stringAt(fields, "description"),
		ConfigSchema:  configSchema,
		PayloadSchema: payloadSchema,
		Ingestion:     triggerIngestionPoll,
		Poll:          todoTriggerPollMapping(),
	}, missingPollFieldCaveats(), nil
}

// triggerNodeOutputSchema finds the flow's data-record-created-trigger node
// among the `nodes` map (keyed by node name, each a node fields map) and
// returns its outputSchema — the record shape Slice 5's payloadSchema AC
// asks for. Returns nil if no such node exists or it carries no
// outputSchema.
func triggerNodeOutputSchema(fields map[string]any) map[string]any {
	nodes := mapAt(fields, "nodes")
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if stringAt(node, "type") == dataRecordCreatedTriggerNodeType {
			return mapAt(node, "outputSchema")
		}
	}
	return nil
}

// todoTriggerPollMapping is the TODO-placeholder poll block emitted for
// every trigger (Slice 5, AC4): every field is non-empty, so the definition
// still parses under the loader's required-poll-field checks, but every
// value is a placeholder, never a guessed real one.
func todoTriggerPollMapping() outputTriggerPollMappingV1 {
	return outputTriggerPollMappingV1{
		Method:              todoTriggerMethod,
		Path:                todoTriggerPath,
		RecordsPath:         todoTriggerRecordsPath,
		RecordIDPath:        todoTriggerRecordIDPath,
		RecordTimestampPath: todoTriggerRecordTimestampPath,
		Payload:             map[string]string{todoTriggerPayloadKey: todoTriggerPayloadValue},
	}
}

// missingPollFieldCaveats renders one report caveat per missingTriggerPollFields
// entry, each naming the specific field and pointing at the provider's real
// polling endpoint as the source of truth (Slice 5, AC6).
func missingPollFieldCaveats() []string {
	caveats := make([]string, 0, len(missingTriggerPollFields))
	for _, field := range missingTriggerPollFields {
		caveats = append(caveats, fmt.Sprintf(
			"poll.%s could not be derived from Membrane's abstract collectionKey flow graph — set it from the provider's real polling endpoint before use",
			field,
		))
	}
	return caveats
}
