package assignment

import "testing"

func TestS5NativeActivationSkipRetainsOtherStagesAndApproval(t *testing.T) {
	// These are the packet initialization fields read by PacketEnabled.
	state := map[string]any{"schema_version": "1.2.0", "configuration": map[string]any{"optimization": map[string]any{"packet": true}}, "lifecycle": map[string]any{"state": "document_verification"}}
	m := manifest{WorkgroupKind: "document_verifier"}
	a := assignment{RoleFamily: "document-verifier", ResponsibilityID: "DV-SPEC-CONSISTENCY"}
	for _, mode := range []string{"", "plan_checkpoint", "one_shot"} {
		a.DispatchMode = mode
		if !s5NativeReviewAssignment(state, m, a) {
			t.Fatalf("original S5 mode %q still needs generic activation", mode)
		}
	}
	a.DispatchMode = "explicit_approval"
	if s5NativeReviewAssignment(state, m, a) {
		t.Fatal("real approval was skipped")
	}
	a.DispatchMode = "plan_checkpoint"
	for _, stage := range []string{"verification", "bug_resolution", "building"} {
		state["lifecycle"].(map[string]any)["state"] = stage
		if s5NativeReviewAssignment(state, m, a) {
			t.Fatalf("%s activation was skipped", stage)
		}
	}
	state["lifecycle"].(map[string]any)["state"] = "document_verification"
	a.ResponsibilityID = "OTHER"
	if s5NativeReviewAssignment(state, m, a) {
		t.Fatal("unrelated responsibility skipped activation")
	}
}
