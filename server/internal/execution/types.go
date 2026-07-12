// Package execution owns the ToolExecution operation (Slice 5): running one
// tool call against a Connection's provider and returning the
// {successful, error, data} envelope PD6 specifies.
package execution

// ExecutionError is the tool-level error carried inside a non-successful
// Result (PD6): a machine-readable code and a human message. It is never an
// HTTP error — PD6 keeps every tool-level failure inside a successful HTTP
// 200 response.
type ExecutionError struct {
	Code    string
	Message string
}

// Result is the {successful, error, data} envelope every tool execution
// returns (PD6, AC1). A tool-level failure — invalid arguments, a
// non-ACTIVE connection, an upstream provider error — carries a nil Data and
// a non-nil Error; success carries a nil Error and the provider's response
// as Data.
type Result struct {
	Successful bool
	Error      *ExecutionError
	Data       any
}

// SuccessResult builds the {successful: true, error: null, data} envelope
// (AC1).
func SuccessResult(data any) Result {
	return Result{Successful: true, Data: data}
}

// FailureResult builds the {successful: false, error, data: null} envelope
// (PD6) for a tool-level failure.
func FailureResult(code, message string) Result {
	return Result{Successful: false, Error: &ExecutionError{Code: code, Message: message}}
}
