package hookctx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const planningAnswerTestREQSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func planningAnswerTestState(stage string, packetMode bool) map[string]any {
	phase := map[string]string{"S2": "design", "S3": "contracts", "S4": "tasks"}[stage]
	state := map[string]any{
		"schema_version": "1.2.0",
		"runtime_id":     "loop-main-planning",
		"configuration":  map[string]any{"optimization": map[string]any{"packet": packetMode}},
		"baseline":       map[string]any{"generation": 1},
		"lifecycle":      map[string]any{"state": "planning", "phase": phase, "phase_revision": 7},
		"bound_req": map[string]any{
			"id": "REQ-001", "path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": planningAnswerTestREQSHA, "status": "locked",
		},
		"documents": []any{},
		"entities":  map[string]any{"work_packets": []any{}, "submissions": []any{}},
	}
	if stage == "S3" {
		state["documents"] = []any{map[string]any{
			"id": "ARCHITECTURE-001", "path": "docs/architecture/ARCHITECTURE-001.md", "version": "v1.0.0", "sha256": strings.Repeat("b", 64), "status": "locked", "kind": "design", "generation": 1,
		}}
	}
	return state
}

func planningAnswerTestPacket(t *testing.T, state map[string]any, stage string) any {
	t.Helper()
	switch stage {
	case "S2":
		packet, err := runtime.CurrentDesignPacket(state)
		if err != nil {
			t.Fatal(err)
		}
		return packet
	case "S3":
		binding, designs, err := runtime.CurrentContractBinding(state)
		if err != nil {
			t.Fatal(err)
		}
		packet, err := workpacket.BuildContracts(binding, workpacket.ContractInputs{Designs: designs, Sources: []workpacket.Subject{}, Directories: map[string][]string{}})
		if err != nil {
			t.Fatal(err)
		}
		return packet
	case "S4":
		binding, err := runtime.CurrentTaskPlanningBinding(state)
		if err != nil {
			t.Fatal(err)
		}
		inputs := workpacket.TaskPlanningInputs{
			Contracts: []workpacket.Reference{
				{ID: "CONTRACTS-REQ-001", Path: "docs/dev/contracts/CONTRACTS-REQ-001.md", Version: "v1.0.0", SHA256: strings.Repeat("c", 64)},
				{ID: "BE-REQ-001", Path: "docs/dev/contracts/BE-REQ-001.md", Version: "v1.0.0", SHA256: strings.Repeat("d", 64)},
			},
			Sources: []workpacket.Subject{}, Directories: map[string][]string{},
			ClauseUniverse: []workpacket.TaskClause{{ContractID: "BE-REQ-001", Section: 1}},
		}
		packet, err := workpacket.BuildTaskPlanning(binding, inputs)
		if err != nil {
			t.Fatal(err)
		}
		return packet
	default:
		t.Fatalf("unsupported stage %q", stage)
		return nil
	}
}

func addPlanningAnswerTestPacket(t *testing.T, state map[string]any, packet any) {
	t.Helper()
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	entities := state["entities"].(map[string]any)
	entities["work_packets"] = append(entities["work_packets"].([]any), row)
}

func TestCurrentMainPlanningAnswerPathsProjectsOnlyCurrentUnsubmittedTypedPacket(t *testing.T) {
	for _, stage := range []string{"S2", "S3", "S4"} {
		t.Run(stage, func(t *testing.T) {
			state := planningAnswerTestState(stage, true)
			packet := planningAnswerTestPacket(t, state, stage)
			var want string
			switch p := packet.(type) {
			case workpacket.DesignPacket:
				want = p.AnswerPath
			case workpacket.ContractPacket:
				want = p.AnswerPath
			case workpacket.TaskPlanningPacket:
				want = p.AnswerPath
			}
			addPlanningAnswerTestPacket(t, state, packet)
			got := currentMainPlanningAnswerPaths(state, "", nil)
			if len(got) != 1 || got[0] != want {
				t.Fatalf("current registered %s Main packet path = %#v, want %q", stage, got, want)
			}

			entities := state["entities"].(map[string]any)
			entities["submissions"] = []any{map[string]any{"packet_id": packetID(packet), "owner": "Main"}}
			if got := currentMainPlanningAnswerPaths(state, "", nil); len(got) != 0 {
				t.Fatalf("submitted %s packet must not retain an authoring path: %#v", stage, got)
			}
		})
	}
}

func TestCurrentMainPlanningAnswerPathsRejectsWrongIdentityAndBinding(t *testing.T) {
	for _, stage := range []string{"S2", "S3", "S4"} {
		t.Run(stage, func(t *testing.T) {
			state := planningAnswerTestState(stage, true)
			packet := planningAnswerTestPacket(t, state, stage)
			addPlanningAnswerTestPacket(t, state, mutatePlanningAnswerTestPacket(t, packet, "wrong_req"))
			if got := currentMainPlanningAnswerPaths(state, "", nil); len(got) != 0 {
				t.Fatalf("wrong REQ binding must not project Main path: %#v", got)
			}

			state = planningAnswerTestState(stage, true)
			packet = planningAnswerTestPacket(t, state, stage)
			addPlanningAnswerTestPacket(t, state, mutatePlanningAnswerTestPacket(t, packet, "wrong_owner"))
			if got := currentMainPlanningAnswerPaths(state, "", nil); len(got) != 0 {
				t.Fatalf("wrong owner binding must not project Main path: %#v", got)
			}

			state = planningAnswerTestState(stage, true)
			oldState := planningAnswerTestState(stage, true)
			oldState["lifecycle"].(map[string]any)["phase_revision"] = 6
			addPlanningAnswerTestPacket(t, state, planningAnswerTestPacket(t, oldState, stage))
			if got := currentMainPlanningAnswerPaths(state, "", nil); len(got) != 0 {
				t.Fatalf("old entry must not project Main path: %#v", got)
			}

			issuedState := planningAnswerTestState(stage, true)
			packet = planningAnswerTestPacket(t, issuedState, stage)
			state = planningAnswerTestState(stage, false)
			addPlanningAnswerTestPacket(t, state, packet)
			if got := currentMainPlanningAnswerPaths(state, "", nil); len(got) != 0 {
				t.Fatalf("packet-off state must not project Main path: %#v", got)
			}
		})
	}

	state := planningAnswerTestState("S2", true)
	packet := planningAnswerTestPacket(t, state, "S2")
	addPlanningAnswerTestPacket(t, state, packet)
	if got := currentMainPlanningAnswerPaths(state, "worker-1", nil); len(got) != 0 {
		t.Fatalf("Worker identity must not fall back to Main path: %#v", got)
	}
	if got := currentMainPlanningAnswerPaths(state, "", &policy.AgentContext{ID: "worker-1"}); len(got) != 0 {
		t.Fatalf("loaded Worker context must not fall back to Main path: %#v", got)
	}
}

func packetID(packet any) string {
	switch p := packet.(type) {
	case workpacket.DesignPacket:
		return p.ID
	case workpacket.ContractPacket:
		return p.ID
	case workpacket.TaskPlanningPacket:
		return p.ID
	default:
		return ""
	}
}

func mutatePlanningAnswerTestPacket(t *testing.T, packet any, mutation string) any {
	t.Helper()
	mutate := func(binding *workpacket.PlanningBinding) {
		switch mutation {
		case "wrong_req":
			binding.Requirement.ID = "REQ-002"
			binding.Requirement.Path = "docs/requirements/REQ-002.md"
		case "wrong_owner":
			binding.Owner = "worker-1"
		}
	}
	switch p := packet.(type) {
	case workpacket.DesignPacket:
		mutate(&p.Binding)
		return p
	case workpacket.ContractPacket:
		mutate(&p.Binding)
		return p
	case workpacket.TaskPlanningPacket:
		mutate(&p.Binding)
		return p
	default:
		t.Fatalf("unsupported packet type %T", packet)
		return nil
	}
}
