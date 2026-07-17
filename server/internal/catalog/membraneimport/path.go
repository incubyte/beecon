package membraneimport

import "fmt"

// resolveToolPath converts one Membrane action's config.request.path into
// the emitted tool's mapping.path:
//   - a plain string has its pathParameters inlined (Slice 1);
//   - a single-fallback $case (spec's rule-based handling) emits the
//     default branch with pathParameters inlined, and reports the dropped
//     guard as a caveat (a partial conversion);
//   - a $case with genuine branching returns skipReason so the whole tool
//     is reported needs-human, never guessed at;
//   - any other non-string, non-$case shape is not translated: the TODO
//     placeholder is emitted, with a caveat naming the unsupported
//     construct when the path is present but unrecognized.
func resolveToolPath(request map[string]any) (path string, caveats []string, defaults []schemaDefault, skipReason string) {
	rawPath := valueAt(request, "path")
	pathParameters := mapAt(request, "pathParameters")

	outcome, defaultBranch, guard := classifyCasePath(rawPath)
	switch outcome {
	case casePathGenuineBranching:
		return "", nil, nil, genuineBranchingReason

	case casePathSingleFallback:
		branchPath, ok := defaultBranch.(string)
		if !ok {
			return "", nil, nil, genuineBranchingReason
		}
		inlined, inlineCaveats, inlineDefaults := inlinePathParameters(branchPath, pathParameters)
		caveat := fmt.Sprintf("path: dropped $case branch guarded by %s; emitted default branch %q", guard, inlined)
		return inlined, append([]string{caveat}, inlineCaveats...), inlineDefaults, ""

	default: // casePathNotACase
		if rawPath == nil {
			return untranslatedPathPlaceholder, nil, nil, ""
		}
		literal, ok := rawPath.(string)
		if !ok {
			caveat := fmt.Sprintf("path: dropped unsupported construct %s — needs human translation", describeUnsupportedConstruct(rawPath))
			return untranslatedPathPlaceholder, []string{caveat}, nil, ""
		}
		inlined, inlineCaveats, inlineDefaults := inlinePathParameters(literal, pathParameters)
		return inlined, inlineCaveats, inlineDefaults, ""
	}
}
