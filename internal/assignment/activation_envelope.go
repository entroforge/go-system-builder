// activation_envelope.go — plan_checkpoint activation envelope pre-staging
// (L4 §3.3 continuous-execution auto-activation).
//
// For dispatch_mode=plan_checkpoint, register-workgroup pre-stages an
// activation envelope file at .claude/evidence/<req>/<task>/activation-<agent>.json
// so the Hook transport has a stable, schema-shaped source for allowed_tools /
// allowed_write_paths / allowed_command_classes the moment plan_checkpoint
// captures the PLAN_REPORT. The envelope's hash-chain fields are stamped at
// activation time by the PostToolUse(SendMessage) auto-chain
// (autoAdvanceToWorking) or the runtime agent-begin fallback verb — both
// rewrite the same file with the actual plan_report bytes before submitting
// activation_sent, preserving verifyActivationReadbackChain's fail-closed
// semantics.
//
// plan_approval_required and one_shot assignments are NOT pre-staged: the
// activation envelope for those modes is produced manually at activation_sent
// (the human Gate signs the readback_response before activation). Pre-staging
// them would silently widen the activation surface.
package assignment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// ActivationEnvelope is the on-disk representation read by
// internal/hookctx/loadActivation. Field names mirror the activationFile struct
// in hookctx/loader.go (must stay byte-compatible).
type ActivationEnvelope struct {
	AgentID               string   `json:"agent_id"`
	AllowedTools          []string `json:"allowed_tools"`
	AllowedWritePaths     []string `json:"allowed_write_paths"`
	AllowedCommandClasses []string `json:"allowed_command_classes"`
}

// ActivationSourceEntry is the row of the workgroup manifest that drives an
// activation envelope. Only the fields the envelope needs are listed — the
// caller is responsible for sourcing the rest.
type ActivationSourceEntry struct {
	AgentID            string
	AgentDefinitionRef string
	SkillRefs          []string
	WritePaths         []string
	OutputPaths        []string
}

// PreStageActivationEnvelope writes a plan_checkpoint activation envelope
// file for the given agent and returns the relative path (repo-root anchored)
// to stamp on the agent's activation_ref. Returns "" when the envelope does
// not apply (non plan_checkpoint mode or insufficient manifest data).
//
// The envelope's allowed_tools default covers the dispatch surface every
// Worker needs (Read / Glob / Grep / Edit / Write / Bash / Skill); callers
// needing a narrower tool set can override via the ActivationSourceEntry.
// allowed_write_paths is sourced from manifest.write_paths with output_paths
// appended (L3-S7 §3 dual-binding). allowed_command_classes is sourced from
// the registered SkillRefs map when present, otherwise defaults to {test,
// lint, build}.
func PreStageActivationEnvelope(root, workgroupID, taskID, agentID, dispatchMode string, source ActivationSourceEntry) (string, error) {
	if dispatchMode != "plan_checkpoint" {
		return "", nil
	}
	if agentID == "" {
		return "", fmt.Errorf("activation envelope: agent_id is required")
	}
	if err := identity.ValidateAgentID(agentID); err != nil {
		return "", fmt.Errorf("activation envelope: %w", err)
	}
	envelope := buildActivationEnvelope(agentID, source)
	rel := activationEnvelopePath(root, workgroupID, taskID, agentID)
	if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return "", fmt.Errorf("activation envelope: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return "", fmt.Errorf("activation envelope: encode: %w", err)
	}
	if err := os.WriteFile(rel, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("activation envelope: write: %w", err)
	}
	return repositoryPath(root, rel), nil
}

// buildActivationEnvelope fills the four hookctx-loader fields deterministically.
func buildActivationEnvelope(agentID string, source ActivationSourceEntry) ActivationEnvelope {
	tools := defaultActivationTools()
	writePaths := mergeUniqueStringSlices(source.WritePaths, source.OutputPaths)
	commandClasses := commandClassesForSkills(source.SkillRefs)
	return ActivationEnvelope{
		AgentID:               agentID,
		AllowedTools:          tools,
		AllowedWritePaths:     writePaths,
		AllowedCommandClasses: commandClasses,
	}
}

// defaultActivationTools is the dispatch surface every Worker needs. It mirrors
// the frontmatter `tools:` list in agents/*.md (the platform contract) so
// Hook + permission decisions match what the Worker was loaded with.
func defaultActivationTools() []string {
	return []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Skill"}
}

// commandClassesForSkills maps the registered SkillRefs to the
// allowed_command_classes the Safety Policy checks. Unknown skills map to an
// empty class so the envelope stays minimal rather than fabricating a
// permissive default.
func commandClassesForSkills(skillRefs []string) []string {
	if len(skillRefs) == 0 {
		return []string{"test", "lint", "build"}
	}
	classes := map[string]bool{}
	for _, ref := range skillRefs {
		switch strings.TrimSpace(ref) {
		case "backend-engineering", "frontend-engineering", "api-contracts",
			"database-change", "state-machine-design", "dag-design",
			"http-api-design", "gin", "gorm", "openapi-swagger":
			classes["test"] = true
			classes["lint"] = true
			classes["build"] = true
		case "testing-strategy", "code-quality", "playwright-e2e",
			"e2e-browser-testing", "user-flow-design", "scenario-model-design":
			classes["test"] = true
			classes["lint"] = true
		case "security-review", "reliability-review", "performance-review",
			"integration-verification", "ui-prototyping", "vue-router", "pinia":
			classes["test"] = true
			classes["lint"] = true
		case "agent-dispatch", "loop-orchestration", "team-planning",
			"bug-resolution":
			// Orchestration skills — no product-adjacent command classes.
		}
	}
	if len(classes) == 0 {
		return []string{"test", "lint", "build"}
	}
	out := make([]string, 0, len(classes))
	for c := range classes {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// mergeUniqueStringSlices merges two string slices, deduplicating and sorting.
// Manifests commonly declare the same path in write_paths and output_paths;
// the envelope must not double-list.
func mergeUniqueStringSlices(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// activationEnvelopePath returns the absolute path for the activation envelope
// file. The layout mirrors the workgroup manifest convention so the Hook
// loader's relative-path resolution (filepath.Join(root, ref)) finds it.
func activationEnvelopePath(root, workgroupID, taskID, agentID string) string {
	safeAgentID := strings.ReplaceAll(agentID, "/", "_")
	safeAgentID = strings.ReplaceAll(safeAgentID, "..", "_")
	safeTaskID := strings.ReplaceAll(taskID, "/", "_")
	safeWorkgroupID := strings.ReplaceAll(workgroupID, "/", "_")
	return filepath.Join(root, ".claude", "evidence", safeWorkgroupID, safeTaskID, "activation-"+safeAgentID+".json")
}

// BuildPlanCheckpointActivationMessage synthesizes an activation message
// bound to the plan_report file bytes. Used by the PostToolUse auto-chain and
// the runtime agent-begin fallback verb. It clones the bundled
// agent-message.examples.json activation example and patches the agent_id /
// task_id / runtime_id / hash chain fields so the
// auto-generated envelope stays schema-valid without re-deriving the
// document / allowed_*_paths / checkpoints / stop_conditions payloads from
// scratch. Documents array is patched to >=3 entries (schema minItems=3).
//
// activationRevision is an optional explicit Runtime revision assertion. A
// negative value is the normal path and is omitted from the Agent message.
func BuildPlanCheckpointActivationMessage(
	root, runtimeID, agentID, agentDefinitionRef, taskID, teamID string,
	activationRevision int,
	planPath string,
	source ActivationSourceEntry,
	occurredAtRFC3339 string,
) (map[string]any, error) {
	planAbs := planPath
	if !filepath.IsAbs(planAbs) {
		planAbs = filepath.Join(root, planAbs)
	}
	planBytes, err := os.ReadFile(planAbs)
	if err != nil {
		return nil, fmt.Errorf("activation message: read plan: %w", err)
	}
	sum := sha256.Sum256(planBytes)
	planSHA := hex.EncodeToString(sum[:])
	var plan map[string]any
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return nil, fmt.Errorf("activation message: decode plan: %w", err)
	}
	messageID, _ := plan["message_id"].(string)
	if messageID == "" {
		messageID = "msg-plan-" + planSHA[:12]
	}
	correlationID, _ := plan["correlation_id"].(string)
	if correlationID == "" {
		correlationID = "corr-auto-" + planSHA[:12]
	}
	activationID := "act-auto-" + planSHA[:12]

	// Clone the bundled activation example to keep schema validity (documents,
	// allowed_read_paths, checkpoints, stop_conditions, expires_on) for free.
	cloned, err := cloneBundledActivationExample(root)
	if err != nil {
		return nil, err
	}
	cloned["agent_id"] = agentID
	cloned["agent_definition_ref"] = agentDefinitionRef
	cloned["task_id"] = taskID
	cloned["team_id"] = teamID
	cloned["runtime_id"] = runtimeID
	if activationRevision >= 0 {
		cloned["expected_runtime_revision"] = activationRevision
	} else {
		delete(cloned, "expected_runtime_revision")
	}
	cloned["occurred_at"] = occurredAtRFC3339
	cloned["activation_id"] = activationID
	cloned["approved_readback_message_id"] = messageID
	cloned["approved_readback_sha256"] = planSHA
	cloned["message_id"] = "msg-" + activationID
	cloned["correlation_id"] = correlationID
	cloned["approval_evidence_id"] = "auto-chain:plan_checkpoint"

	// Surface the manifest's declared write surface so the Hook transport
	// honors the workgroup's scope (L3-S7 §3 dual-binding). Read paths are
	// kept broad (docs/, .claude/, internal/) to match the bundled example.
	envelope := buildActivationEnvelope(agentID, source)
	cloned["allowed_write_paths"] = envelope.AllowedWritePaths
	cloned["allowed_tools"] = envelope.AllowedTools
	cloned["allowed_command_classes"] = envelope.AllowedCommandClasses
	if len(envelope.AllowedWritePaths) > 0 {
		cloned["output_paths"] = envelope.AllowedWritePaths
	}
	return cloned, nil
}

// cloneBundledActivationExample loads the bundled agent-message.examples.json
// activation example and returns a deep-cloned map. The clone is a fresh
// allocation so the caller can mutate freely. The examples ship embedded in
// the binary (internal/schema), so the chain works in any target repository,
// not only in the harness source tree.
func cloneBundledActivationExample(root string) (map[string]any, error) {
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		return nil, fmt.Errorf("activation message: read embedded examples: %w", err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("activation message: decode examples: %w", err)
	}
	for _, message := range messages {
		if t, _ := message["message_type"].(string); t == "activation" {
			// Deep clone via JSON roundtrip — small enough not to worry about cost.
			buf, err := json.Marshal(message)
			if err != nil {
				return nil, fmt.Errorf("activation message: clone: %w", err)
			}
			var cloned map[string]any
			if err := json.Unmarshal(buf, &cloned); err != nil {
				return nil, fmt.Errorf("activation message: clone decode: %w", err)
			}
			return cloned, nil
		}
	}
	return nil, fmt.Errorf("activation message: bundled examples have no activation entry")
}

// ValidateActivationMessageBytes is a thin convenience wrapper for callers
// that want to schema-validate a synthesized activation message before
// passing it to AdvanceAgent.
func ValidateActivationMessageBytes(root string, data []byte) error {
	return schema.NewValidator(root).ValidateBytes("agent-message.schema.json", data)
}

// WriteActivationMessageFile writes a synthesized activation message to disk
// and returns its absolute path. The auto-chain and the agent-begin fallback
// verb pass the returned path to AdvanceAgent as the activation_sent message.
func WriteActivationMessageFile(root, workgroupID, taskID, agentID string, message map[string]any) (string, error) {
	rel := activationMessagePath(root, workgroupID, taskID, agentID)
	if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return "", fmt.Errorf("activation message: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(message, "", "  ")
	if err != nil {
		return "", fmt.Errorf("activation message: encode: %w", err)
	}
	if err := os.WriteFile(rel, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("activation message: write: %w", err)
	}
	return rel, nil
}

// activationMessagePath returns the absolute path for the synthesized
// activation message. Distinct from the activation envelope so the
// lifecycle.go verifyActivationReadbackChain logic (which reads the
// activation envelope) does not collide with the activation message.
func activationMessagePath(root, workgroupID, taskID, agentID string) string {
	return activationEnvelopePath(root, workgroupID, taskID, agentID+"-message")
}

// WriteWorkStartMessageFile writes a minimal work_start message file and
// returns its path. Used by the auto-chain to advance activated -> working.
// The activation_id is the same id stamped on the activation message so the
// journal/dedup layer sees one continuous chain.
func WriteWorkStartMessageFile(
	root, workgroupID, taskID, agentID, agentDefinitionRef, teamID, runtimeID, activationID, correlationID string,
	expectedRevision int,
	occurredAtRFC3339 string,
) (string, error) {
	safeAgentID := strings.ReplaceAll(agentID, "/", "_")
	safeTaskID := strings.ReplaceAll(taskID, "/", "_")
	safeWorkgroupID := strings.ReplaceAll(workgroupID, "/", "_")
	rel := filepath.Join(root, ".claude", "evidence", safeWorkgroupID, safeTaskID, "work-start-"+safeAgentID+".json")
	if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return "", fmt.Errorf("work_start message: mkdir: %w", err)
	}
	message := map[string]any{
		"schema_version":       "1.0.0",
		"message_type":         "work_start",
		"message_id":           "msg-work-start-" + activationID,
		"correlation_id":       correlationID,
		"runtime_id":           runtimeID,
		"agent_id":             agentID,
		"agent_definition_ref": agentDefinitionRef,
		"task_id":              taskID,
		"bug_id":               nil,
		"team_id":              teamID,
		"occurred_at":          occurredAtRFC3339,
		"activation_id":        activationID,
		"body":                 "auto-chain: plan_checkpoint continuous execution",
	}
	if expectedRevision >= 0 {
		message["expected_runtime_revision"] = expectedRevision
	}
	data, err := json.MarshalIndent(message, "", "  ")
	if err != nil {
		return "", fmt.Errorf("work_start message: encode: %w", err)
	}
	if err := os.WriteFile(rel, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("work_start message: write: %w", err)
	}
	return rel, nil
}

// ResolveAgentActivationEnvelope reads the activation envelope that the
// pre-stage step wrote for the given agent and returns its
// ActivationSourceEntry (for use by the auto-chain / agent-begin verb to
// keep envelope and message in lockstep). Returns an empty entry when the
// agent has no pre-staged envelope (e.g. plan_approval_required assignments).
func ResolveAgentActivationEnvelope(root, activationRef string) (ActivationSourceEntry, error) {
	if activationRef == "" {
		return ActivationSourceEntry{}, nil
	}
	path := activationRef
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ActivationSourceEntry{}, nil
	}
	var envelope ActivationEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ActivationSourceEntry{}, err
	}
	return ActivationSourceEntry{
		AgentID:     envelope.AgentID,
		WritePaths:  envelope.AllowedWritePaths,
		OutputPaths: envelope.AllowedWritePaths,
	}, nil
}
