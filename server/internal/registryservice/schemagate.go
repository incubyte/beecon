package registryservice

import (
	"beecon/internal/registrybundle"
	"beecon/internal/schema"
)

// validateOutputSchemasAgainstSamples is PD63's differentiator gate: every
// tool in a published bundle must declare an output schema AND carry a
// recorded sample response, and that schema must actually validate the
// sample — reusing internal/schema (the shared JSON-Schema leaf package
// BOUNDARIES.md lists as importable by any module, domain or not). Returns
// a specific error naming the first offending tool and what's wrong with
// it.
func validateOutputSchemasAgainstSamples(tools []registrybundle.Tool) error {
	for _, tool := range tools {
		if err := validateToolOutputSchemaAgainstSample(tool); err != nil {
			return err
		}
	}
	return nil
}

func validateToolOutputSchemaAgainstSample(tool registrybundle.Tool) error {
	if len(tool.OutputSchema) == 0 {
		return ErrMissingOutputSchema(tool.Slug)
	}
	if len(tool.Sample) == 0 {
		return ErrMissingSample(tool.Slug)
	}
	if err := schema.Validate(tool.OutputSchema, tool.Sample); err != nil {
		return ErrOutputSchemaSampleMismatch(tool.Slug, err.Error())
	}
	return nil
}
