package ai

// ContextOverflowError reports a provider rejection caused by the request
// exceeding the backend's context window (e.g. vLLM's "maximum context
// length is N tokens" HTTP 400). It is a distinct type so the agent loop
// can recognise the condition, compact the conversation harder, and retry
// instead of aborting the run. Message carries the full human-readable
// provider error, including the backend response body and remediation
// hint, so error output is unchanged for callers that don't special-case
// the type.
type ContextOverflowError struct {
	StatusCode int
	Message    string
}

func (e *ContextOverflowError) Error() string { return e.Message }
