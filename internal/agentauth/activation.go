package agentauth

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Message struct {
	MessageType             string `json:"message_type"`
	RuntimeID               string `json:"runtime_id"`
	ExpectedRuntimeRevision int    `json:"expected_runtime_revision"`
	AgentID                 string `json:"agent_id"`
	TaskID                  string `json:"task_id"`
	// Activation chain fields (verified at activation_sent — L3-S6
	// complexity pass: previously schema-required but never checked, pure
	// ceremony; now fail-closed against the registered readback file).
	ApprovedReadbackMessageID string `json:"approved_readback_message_id,omitempty"`
	ApprovedReadbackSHA256    string `json:"approved_readback_sha256,omitempty"`
	Body                      string `json:"body,omitempty"`
}

func VerifyActivationReadbackChain(root string, agent map[string]any, message Message) error {
	ref, _ := agent["readback_ref"].(string)
	if ref == "" {
		return fmt.Errorf("activation chain: agent has no registered readback_ref — submit readback_submitted first")
	}
	path := ref
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("activation chain: registered readback %s unreadable: %w", ref, err)
	}
	sum := sha256.Sum256(data)
	actual := fmt.Sprintf("%x", sum[:])
	if message.ApprovedReadbackSHA256 == "" {
		return fmt.Errorf("activation chain: approved_readback_sha256 is empty — set it to the sha256 of the readback message file (%s)", ref)
	}
	if message.ApprovedReadbackSHA256 != actual {
		return fmt.Errorf(
			"activation chain: approved_readback_sha256 %s… does not match registered readback %s (actual %s…) — recompute the hash of the readback message file and resubmit",
			message.ApprovedReadbackSHA256[:12], ref, actual[:12])
	}
	var readback struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(data, &readback); err != nil {
		return fmt.Errorf("activation chain: registered readback %s is not valid JSON: %w", ref, err)
	}
	if message.ApprovedReadbackMessageID == "" || message.ApprovedReadbackMessageID != readback.MessageID {
		return fmt.Errorf(
			"activation chain: approved_readback_message_id %q does not match registered readback message_id %q",
			message.ApprovedReadbackMessageID, readback.MessageID)
	}
	return nil
}
