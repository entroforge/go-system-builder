package reviewmeta

import (
	"encoding/base64"
	"strconv"
)

const (
	LegacySchemaVersion   = "1.0.0"
	IdentitySchemaVersion = "1.1.0"
)

// Identity locates an existing professional responsibility; an absent PlanID is unknown.
type Identity struct{ PlanID string }

func NewPlanID(runtimeID, eventID string) string {
	if runtimeID == "" || eventID == "" {
		return ""
	}
	return "review-plan-" + strconv.Itoa(len(runtimeID)) + "-" + base64.RawURLEncoding.EncodeToString([]byte(runtimeID)) + "-" + base64.RawURLEncoding.EncodeToString([]byte(eventID))
}

func FromRoundEntry(review map[string]any) Identity {
	entry, _ := review["round_entry"].(map[string]any)
	return Identity{PlanID: textField(entry["plan_id"])}
}

func FromReview(review map[string]any) Identity {
	if entry, ok := review["round_entry"].(map[string]any); ok {
		return Identity{PlanID: textField(entry["plan_id"])}
	}
	if plan, ok := review["plan"].(map[string]any); ok {
		return Identity{PlanID: textField(plan["plan_id"])}
	}
	return Identity{}
}

type Match uint8

const (
	Unknown Match = iota
	Same
	Different
)

func CompareIdentity(a, b Identity) Match {
	if a.PlanID == "" || b.PlanID == "" {
		return Unknown
	}
	if a.PlanID == b.PlanID {
		return Same
	}
	return Different
}
func SameIdentity(a, b Identity) bool { return CompareIdentity(a, b) == Same }

// ArtifactIdentity selects the actual Plan identity.
func ArtifactIdentity(planID string) Identity { return Identity{PlanID: planID} }
