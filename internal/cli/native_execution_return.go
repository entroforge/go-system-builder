package cli

import "context"

// This observation belongs only to the actual tool response. It is neither a
// Runtime work state nor a receipt and is never inferred from returned prose.
type nativeExecutionReturn string

const (
	nativeExecutionUnknown   nativeExecutionReturn = "unknown"
	nativeExecutionLaunched  nativeExecutionReturn = "async_launched"
	nativeExecutionCompleted nativeExecutionReturn = "completed"
)

type nativeExecutionReturnKey struct{}

func nativeExecutionFromResponse(response map[string]any) nativeExecutionReturn {
	status, _ := response["status"].(string)
	asynchronous, present := response["isAsync"]
	async, valid := asynchronous.(bool)
	if present && !valid {
		return nativeExecutionUnknown
	}
	switch status {
	case "async_launched":
		if present && !async {
			return nativeExecutionUnknown
		}
		return nativeExecutionLaunched
	case "completed":
		// isAsync can describe the execution mode; it does not invalidate a
		// separately explicit completed status or itself prove completion.
		return nativeExecutionCompleted
	case "":
		if present && async {
			return nativeExecutionLaunched
		}
	}
	return nativeExecutionUnknown
}
func nativeExecutionInContext(ctx context.Context) nativeExecutionReturn {
	value, _ := ctx.Value(nativeExecutionReturnKey{}).(nativeExecutionReturn)
	return value
}
func nativeExecutionHasReturn(ctx context.Context) bool {
	value := nativeExecutionInContext(ctx)
	// Original receipt-only callers do not claim a new platform observation.
	return value == "" || value == nativeExecutionCompleted
}
func nativeExecutionPendingAction(value nativeExecutionReturn) string {
	if value == nativeExecutionLaunched {
		return "The platform reports asynchronous execution was launched. This is a launch acknowledgement, not a Worker return; it requests no reception repair. Main continues independent work and follows the actual completion notification and original reception facts for the handoff."
	}
	return "This response establishes no supported execution completion state. Platform execution is unknown here; preserve original responsibility and independently recorded receipts. Do not infer a running or returned Worker, a failed Stop, or a need to re-dispatch from this observation."
}
