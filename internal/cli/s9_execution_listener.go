package cli

import (
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// These are the existing S9 lifecycle phases. Unknown and legacy BUG phases
// retain their original Controller path.
func s9ProfessionalPhase(phase string) bool {
	switch phase {
	case "repair_readback", "planning", "reproducing", "fixing", "targeted_reverification", "ready_for_full_review":
		return true
	}
	return false
}

// Professional questions and actual source documents precede optional
// recovery material. This projection neither prepares work nor guesses a
// native Worker owner from its role.
func s9QuestionnaireReadOrder(root string, state map[string]any, request policy.Input) []string {
	owner := request.EffectiveAgentID()
	main := mainQuestionnaireViewer(request) && (owner != "" || request.AgentType == "" && request.AgentTranscriptPath == "")
	if owner == "" && (!main || request.Event == "SubagentStart" || request.Event == "SubagentStop" || request.Event == "TeammateIdle") {
		return nil
	}
	var paths []string
	appendPath := func(path string) {
		if path != "" {
			paths = appendUnique(paths, path)
		}
	}
	if main {
		owner = ""
		for _, path := range repairDispatchPaths(root, state) {
			appendPath(path)
		}
		for _, path := range repairVerifierDispatchPaths(root, state) {
			appendPath(path)
		}
	}
	phase := stringValue(mapFieldCLI(state, "lifecycle")["phase"])
	status := stringValue(repairPointerCLI(state)["status"])
	result := phase == "fixing"
	packetPhase := phase == "planning" || phase == "reproducing" || phase == "fixing" && (status == "repairing" || status == "blocked")
	if packets, err := s9CurrentQuestionnaires(root, state, owner, result, contextForHook(request)); err == nil && packetPhase {
		nativePackets := s9ListenerNativePlans(root, state)
		for _, packet := range packets {
			fact, factErr := s9ListenerAcceptanceContext(contextForHook(request), root, state, packet)
			if fact.unknown {
				// These are already indexed navigation references, not an
				// assertion that this request read or accepted their content.
				if fact.registeredRef != "" {
					appendPath(resolveRootPath(root, fact.registeredRef))
				}
				if native, ok := s9ListenerNativePacket(state, nativePackets, packet); ok {
					brief, _, _ := nativeS9Paths(native)
					appendPath(resolveRootPath(root, brief))
				} else {
					source, _, _ := s9ListenerResultSource(root, state, packet)
					if source != "" {
						appendPath(resolveRootPath(source, packet.view))
						appendPath(resolveRootPath(source, packet.answer))
					}
				}
				continue
			}
			if main || fact.kind != "" {
				// Main reads accepted professional artifacts, not the Worker's
				// private authoring cache. An accepted Worker also has no further
				// questionnaire duty; its original artifact remains the read source.
				if fact.ref != "" {
					appendPath(resolveRootPath(root, fact.ref))
				}
				if main && fact.kind == "blocker" && packet.result != nil {
					if path, err := workpacket.RepairResultResolutionAuthoringPath(workpacket.RepairResultPacket{AnswerPath: packet.answer, Binding: *packet.result}); err == nil {
						appendPath(resolveRootPath(root, path))
					}
				}
				if main && factErr == nil && fact.kind == "" {
					if native, ok := s9ListenerNativePacket(state, nativePackets, packet); ok {
						brief, _, _ := nativeS9Paths(native)
						appendPath(resolveRootPath(root, brief))
					} else if contextForHook(request).Err() != nil {
						source, _, _ := s9ListenerResultSource(root, state, packet)
						if source != "" {
							appendPath(resolveRootPath(source, packet.view))
							appendPath(resolveRootPath(source, packet.answer))
						}
					}
				}
				continue
			}
			if native, ok := s9ListenerNativePacket(state, nativePackets, packet); ok {
				brief, _, _ := nativeS9Paths(native)
				appendPath(resolveRootPath(root, brief))
				appendPath(resolveRootPath(root, native.ViewPath))
				continue
			}
			source, _, _ := s9ListenerResultSource(root, state, packet)
			if source != "" {
				appendPath(resolveRootPath(source, packet.answer))
				appendPath(resolveRootPath(source, packet.view))
			}
		}
	}
	pointer := repairPointerCLI(state)
	if main {
		for _, ref := range repairArtifactRefs(pointer, "result_refs", "result_ref", "result_sha256") {
			appendPath(resolveRootPath(root, ref.Path))
		}
		for _, key := range []string{"impact_ref", "handoff_ref"} {
			if ref := stringValue(pointer[key]); ref != "" {
				appendPath(resolveRootPath(root, ref))
			}
		}
	}
	for _, key := range []string{"contract_ref", "path", "plan_ref"} {
		if ref := stringValue(pointer[key]); ref != "" {
			appendPath(resolveRootPath(root, ref))
		}
	}
	if ref := stringValue(investigationPointer(state)["path"]); ref != "" {
		appendPath(resolveRootPath(root, ref))
	}
	return paths
}
