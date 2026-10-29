package submission

import "testing"

func TestS5RevisionAuthorizationRetainsExplicitAndLegacyActivation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    any
		state   string
		wantErr bool
	}{
		{name: "ordinary plan checkpoint while reading", mode: "plan_checkpoint", state: "reading"},
		{name: "one shot while reading", mode: "one_shot", state: "reading"},
		{name: "explicit approval before activation", mode: "plan_approval_required", state: "reading", wantErr: true},
		{name: "legacy before activation", state: "reading", wantErr: true},
		{name: "explicit approval without its original chain", mode: "plan_approval_required", state: "working", wantErr: true},
		{name: "legacy without its original chain", state: "working", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			agent := map[string]any{"id": "reviewer", "state": tc.state}
			if tc.mode != nil {
				agent["dispatch_mode"] = tc.mode
			}
			state := map[string]any{"entities": map[string]any{"agents": []any{agent}}}
			err := validateS5ApprovalMode(root, state, "reviewer")
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateS5ApprovalMode() error = %v, want error=%t", err, tc.wantErr)
			}
		})
	}
}
