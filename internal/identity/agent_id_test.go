package identity

import "testing"

func TestValidateAgentIDRejectsAuthoringPlaceholders(t *testing.T) {
	tests := []string{
		"TODO(planner):agent-id-for-qa-logic",
		"<agent-id>",
		"${AGENT_ID}",
		"agent with spaces",
		"agent-\n1",
	}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			if err := ValidateAgentID(value); err == nil {
				t.Fatalf("ValidateAgentID(%q) accepted an invalid identity", value)
			}
		})
	}
}

func TestValidateAgentIDAcceptsPlatformIdentityShapes(t *testing.T) {
	for _, value := range []string{"agent-1", "builder-7", "qa_2", "a1", "manifest-owner"} {
		if err := ValidateAgentID(value); err != nil {
			t.Errorf("ValidateAgentID(%q) rejected a valid platform identity: %v", value, err)
		}
	}
}
