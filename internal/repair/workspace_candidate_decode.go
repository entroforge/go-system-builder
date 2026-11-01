package repair

import "encoding/json"

// DecodeWorkspaceCandidate reads the current S9 candidate representation.
func DecodeWorkspaceCandidate(data []byte) (WorkspaceCandidate, error) {
	var candidate WorkspaceCandidate
	if err := json.Unmarshal(data, &candidate); err != nil {
		return WorkspaceCandidate{}, err
	}
	return candidate, nil
}
