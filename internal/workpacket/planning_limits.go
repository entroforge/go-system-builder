package workpacket

import "encoding/json"

// Planning answer limits cover the already-issued machine questionnaire as
// well as the author's bounded response. The system's source inventory must
// not consume the space reserved for professional answers.
func ContractAnswerReadLimit(p ContractPacket) int64 {
	return planningJSONReadLimit(p.Draft, 256<<10)
}

func TaskPlanningAnswerReadLimit(p TaskPlanningPacket) int64 {
	return planningJSONReadLimit(p.Draft, 256<<10)
}

// ContractEvidenceReadLimit accounts for the frozen upstream subjects carried
// into the evidence. The existing allowance for authored artifacts and answer
// metadata remains independent of the size of the machine source inventory.
func ContractEvidenceReadLimit(p ContractPacket) int64 {
	return planningJSONReadLimit(p.Inputs, 1<<20)
}

// S4 carries the accepted S3 subjects forward; that inherited inventory is
// likewise independent of the size allowance for its own professional result.
func TaskPlanningEvidenceReadLimit(p TaskPlanningPacket) int64 {
	return planningJSONReadLimit(p.Inputs, 1<<20)
}

func planningJSONReadLimit(value any, allowance int64) int64 {
	// Issuance uses this same indentation. The allowance also leaves room for
	// normal author formatting without permitting unbounded answer payloads.
	data, _ := json.MarshalIndent(value, "", "  ")
	return int64(len(data)) + allowance
}
