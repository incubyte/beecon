package membraneimport

import "strings"

// casePathOutcome classifies a Membrane action's `config.request.path` when
// it is a `$case` node, per the spec's rule-based $case handling: a
// single-fallback shape (one guarded branch + one plain default, as in the
// get-message `/users/{userId}/...` vs `/me/...`) has a clean Beecon
// equivalent (emit the default, report the dropped guard); a $case with two
// or more substantive value branches (genuine branching) has none.
type casePathOutcome int

const (
	casePathNotACase casePathOutcome = iota
	casePathSingleFallback
	casePathGenuineBranching
)

// genuineBranchingReason is the exact skip reason the spec names for a
// $case with two or more substantive value branches.
const genuineBranchingReason = "conditional mapping has no Beecon equivalent"

// classifyCasePath inspects path and, when it is a Membrane `$case` node,
// decides whether it is the single-fallback shape (returning the default
// branch's raw value and a human-readable description of the dropped guard)
// or genuine branching (no Beecon equivalent). A path that is not a `$case`
// node at all yields casePathNotACase, leaving path translation to the
// caller.
func classifyCasePath(path any) (outcome casePathOutcome, defaultValue any, guardDescription string) {
	asMap, ok := path.(map[string]any)
	if !ok || len(asMap) != 1 {
		return casePathNotACase, nil, ""
	}
	caseNode, ok := asMap["$case"].(map[string]any)
	if !ok {
		return casePathNotACase, nil, ""
	}

	cases, ok := caseNode["cases"].([]any)
	if !ok || len(cases) != 2 {
		return casePathGenuineBranching, nil, ""
	}
	guarded, guardedOK := cases[0].(map[string]any)
	fallback, fallbackOK := cases[1].(map[string]any)
	if !guardedOK || !fallbackOK {
		return casePathGenuineBranching, nil, ""
	}

	filter, guardedHasFilter := guarded["filter"]
	_, fallbackHasFilter := fallback["filter"]
	if !guardedHasFilter || fallbackHasFilter {
		return casePathGenuineBranching, nil, ""
	}

	return casePathSingleFallback, fallback["value"], describeCaseGuard(filter)
}

// describeCaseGuard renders a human-readable name for the dropped branch's
// guard condition, for the report — naming the input(s) it tests when the
// guard follows the `$and`/`$eval`/`$var` shape seen in the samples, and a
// generic description otherwise. Never renders the raw `$`-expression.
func describeCaseGuard(filter any) string {
	names := collectEvalInputNames(filter)
	if len(names) == 0 {
		return "a guarded condition"
	}
	return "a condition on " + strings.Join(names, ", ")
}

// collectEvalInputNames walks a Membrane filter expression looking for every
// `$eval: {$var: $.input.NAME}` shape, returning each distinct "input.NAME"
// it finds in first-seen order.
func collectEvalInputNames(node any) []string {
	var names []string
	seen := map[string]bool{}
	walkEvalInputNames(node, &names, seen)
	return names
}

func walkEvalInputNames(node any, names *[]string, seen map[string]bool) {
	switch value := node.(type) {
	case map[string]any:
		if evalExpr, ok := value["$eval"]; ok {
			if name, ok := simpleVarInputName(evalExpr); ok {
				full := "input." + name
				if !seen[full] {
					seen[full] = true
					*names = append(*names, full)
				}
			}
		}
		for _, nested := range value {
			walkEvalInputNames(nested, names, seen)
		}
	case []any:
		for _, item := range value {
			walkEvalInputNames(item, names, seen)
		}
	}
}
