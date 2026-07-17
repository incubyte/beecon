package membraneimport

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// recordKind classifies one decoded Membrane export file well enough to know
// which group-key field it carries and how to convert it.
type recordKind int

const (
	recordKindUnknown recordKind = iota
	recordKindIntegration
	recordKindAction
	recordKindTrigger
)

// membraneActionRequestType is the Membrane action `type` this importer
// recognizes as convertible (Slice 1/2). Any other type is unrecognized.
const membraneActionRequestType = "api-request-to-external-app"

// sourceRecord is one decoded Membrane file kept alongside its original name
// for error messages and the report.
type sourceRecord struct {
	Name   string
	Fields map[string]any
}

// recordGroup is every record sharing one integrationUuid: at most one
// Integration record (the provider identity source) plus zero or more
// Actions (Slice 1/2) and Triggers (Slice 5).
type recordGroup struct {
	Key         string
	Integration *sourceRecord
	Actions     []sourceRecord
	Triggers    []sourceRecord
}

// firstSourceName names any file in the group, for a skip message when the
// group has no Integration record to name instead.
func (g *recordGroup) firstSourceName() string {
	if len(g.Actions) > 0 {
		return g.Actions[0].Name
	}
	if len(g.Triggers) > 0 {
		return g.Triggers[0].Name
	}
	return g.Key
}

// recordGroups is every group discovered so far, plus the order groups were
// first seen in, so Convert's output is deterministic across runs.
type recordGroups struct {
	byKey map[string]*recordGroup
	order []string
}

// groupRecords decodes and classifies every input file, groups the
// classifiable ones by shared integrationUuid (an integration record's own
// "uuid" field, or an action/trigger record's "integrationUuid" field), and
// returns every file that could not be classified or grouped as a
// SkippedItem naming the file and the reason.
func groupRecords(files []SourceFile) (recordGroups, []SkippedItem) {
	groups := recordGroups{byKey: map[string]*recordGroup{}}
	var skipped []SkippedItem

	for _, file := range files {
		fields, err := decodeYAMLFields(file.Content)
		if err != nil {
			skipped = append(skipped, SkippedItem{Source: file.Name, Reason: fmt.Sprintf("invalid YAML: %v", err)})
			continue
		}

		kind := classifyRecord(fields)
		if kind == recordKindUnknown {
			skipped = append(skipped, SkippedItem{Source: file.Name, Reason: "unrecognized Membrane export shape"})
			continue
		}

		key, ok := groupKey(kind, fields)
		if !ok {
			skipped = append(skipped, SkippedItem{
				Source: file.Name,
				Reason: fmt.Sprintf("missing field %q — cannot be grouped", groupKeyField(kind)),
			})
			continue
		}

		groups.add(key, kind, sourceRecord{Name: file.Name, Fields: fields})
	}

	return groups, skipped
}

func (g *recordGroups) add(key string, kind recordKind, record sourceRecord) {
	group, exists := g.byKey[key]
	if !exists {
		group = &recordGroup{Key: key}
		g.byKey[key] = group
		g.order = append(g.order, key)
	}
	switch kind {
	case recordKindIntegration:
		group.Integration = &record
	case recordKindAction:
		group.Actions = append(group.Actions, record)
	case recordKindTrigger:
		group.Triggers = append(group.Triggers, record)
	}
}

// classifyRecord distinguishes the three Membrane export shapes this
// importer knows about: a trigger export's node graph (`nodes:`), an action
// export (`type: api-request-to-external-app`), and an integration export
// (identified by carrying a `logoUri`, since it has no `type` field of its
// own). Anything else is recordKindUnknown and is skipped, never guessed at.
func classifyRecord(fields map[string]any) recordKind {
	if _, hasNodes := fields["nodes"]; hasNodes {
		return recordKindTrigger
	}
	if stringAt(fields, "type") == membraneActionRequestType {
		return recordKindAction
	}
	if _, hasLogo := fields["logoUri"]; hasLogo {
		return recordKindIntegration
	}
	return recordKindUnknown
}

// groupKeyField names the field a record of this kind carries its grouping
// identity in: an integration record is identified by its own "uuid"; an
// action or trigger record references its owning integration via
// "integrationUuid".
func groupKeyField(kind recordKind) string {
	if kind == recordKindIntegration {
		return "uuid"
	}
	return "integrationUuid"
}

func groupKey(kind recordKind, fields map[string]any) (string, bool) {
	value := stringAt(fields, groupKeyField(kind))
	if value == "" {
		return "", false
	}
	return value, true
}

func decodeYAMLFields(content []byte) (map[string]any, error) {
	var fields map[string]any
	if err := yaml.Unmarshal(content, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}
