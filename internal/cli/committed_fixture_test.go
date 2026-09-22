package cli_test

import (
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
	"testing"
)

func commitStageFixture(t *testing.T, root string) string { return fixtures.CommitFixture(t, root) }

func hookContextValue(payload map[string]any) any {
	if specific, ok := payload["hookSpecificOutput"].(map[string]any); ok {
		return specific["additionalContext"]
	}
	return payload["systemMessage"]
}
