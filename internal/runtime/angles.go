// Package runtime provides the Loop Harness runtime store, change record
// management, evidence recording, and (with REQ-003) the angles registry
// file layer.
//
// REQ-003 FR-005/FR-007: angles registry is a module-level append-mostly
// file at docs/design/angles/{module}.yaml. It is NOT part of Runtime state
// and does not flow through Store.Update CAS; it is a parallel file layer
// whose writers are serialized by registryMu. The runtime API here is the
// single entry point for all registry mutations.
package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Angle status values. Retract is permanent (status=retracted); revive is
// allowed exactly once per retracted angle (FR-007).
const (
	AngleStatusActive    = "active"
	AngleStatusStale     = "stale"
	AngleStatusRetracted = "retracted"
)

// AngleDispositionKind matches disposition.kind in angle-declaration.schema.json.
const (
	DispositionConfirm = "confirm"
	DispositionExtend  = "extend"
	DispositionRetract = "retract"
	DispositionRevive  = "revive"
)

// targetBlacklist is the list of generic category words that may NOT appear
// as an angle target. They are categories, not specific targets. The angle
// self-audit framework's whole point (REQ-003 Q-003) is to refuse generic
// taxonomy — these words are the floor of that refusal.
var targetBlacklist = map[string]bool{
	"security":        true,
	"performance":     true,
	"correctness":     true,
	"reliability":     true,
	"safety":          true,
	"usability":       true,
	"maintainability": true,
	"testability":     true,
}

// angleIDPattern is ANG-{MODULE-SUFFIX}-{NNN}.
var angleIDPattern = regexp.MustCompile(`^ANG-[A-Z0-9]+-[0-9]{3,}$`)

// AngleRegistryFileName maps a module path to its registry file name.
// REQ-003 FR-005: module name = path with leading/trailing slashes trimmed
// and internal slashes replaced by hyphens.
//
//	internal/change/      -> internal-change.yaml
//	internal/runtime/sub/ -> internal-runtime-sub.yaml
func AngleRegistryFileName(modulePath string) string {
	trimmed := strings.Trim(modulePath, "/")
	if trimmed == "" {
		return "root.yaml"
	}
	return strings.ReplaceAll(trimmed, "/", "-") + ".yaml"
}

// AngleRegistryFilePath returns the absolute path of a module's registry.
func AngleRegistryFilePath(root, modulePath string) string {
	return filepath.Join(root, "docs", "design", "angles", AngleRegistryFileName(modulePath))
}

// ModuleFromRegistryFileName is the inverse of AngleRegistryFileName, used
// by tests and audit tooling to recover the module path from a file name.
func ModuleFromRegistryFileName(name string) string {
	if name == "root.yaml" {
		return ""
	}
	if !strings.HasSuffix(name, ".yaml") {
		return name
	}
	base := strings.TrimSuffix(name, ".yaml")
	return strings.ReplaceAll(base, "-", "/")
}

// Angle is a single angle entry in a module registry.
type Angle struct {
	ID            string    `json:"id" yaml:"id"`
	Statement     string    `json:"statement" yaml:"statement"`
	Target        string    `json:"target" yaml:"target"`
	DeclaredIn    string    `json:"declared_in" yaml:"declared_in"`
	LastAppliedIn string    `json:"last_applied_in" yaml:"last_applied_in"`
	Status        string    `json:"status" yaml:"status"`
	RetractReason string    `json:"retract_reason,omitempty" yaml:"retract_reason,omitempty"`
	DeclaredAt    time.Time `json:"declared_at" yaml:"declared_at"`
}

// RefactorHistoryEntry records a retract event. Retract history is
// append-only and never deleted (FR-007 append-mostly invariant).
type RefactorHistoryEntry struct {
	AngleID     string    `json:"angle_id" yaml:"angle_id"`
	REQ         string    `json:"req" yaml:"req"`
	Reason      string    `json:"reason" yaml:"reason"`
	RetractedAt time.Time `json:"retracted_at" yaml:"retracted_at"`
}

// ReviveHistoryEntry records a revive event. Revive is allowed once per
// retracted angle; the history records the single revive (FR-007).
type ReviveHistoryEntry struct {
	AngleID   string    `json:"angle_id" yaml:"angle_id"`
	REQ       string    `json:"req" yaml:"req"`
	RevivedAt time.Time `json:"revived_at" yaml:"revived_at"`
}

// ModuleRegistry is the JSON-decoded body of docs/design/angles/{module}.yaml.
// Files are JSON-formatted (YAML 1.2 JSON subset) to keep the harness free
// of third-party YAML dependencies; the .yaml extension is preserved per
// REQ-003 §10 for human-reader familiarity.
type ModuleRegistry struct {
	Module          string                 `json:"module" yaml:"module"`
	Version         string                 `json:"version" yaml:"version"`
	Angles          []Angle                `json:"angles" yaml:"angles"`
	RefactorHistory []RefactorHistoryEntry `json:"refactor_history" yaml:"refactor_history"`
	ReviveHistory   []ReviveHistoryEntry   `json:"revive_history" yaml:"revive_history"`
}

// NewModuleRegistry returns an empty registry for a module.
func NewModuleRegistry(modulePath string) *ModuleRegistry {
	return &ModuleRegistry{
		Module:          modulePath,
		Version:         "v0.0.0",
		Angles:          []Angle{},
		RefactorHistory: []RefactorHistoryEntry{},
		ReviveHistory:   []ReviveHistoryEntry{},
	}
}

// registryMu serializes writes per process. Cross-process safety relies on
// the review_commit step being the sole writer (REQ-003 FR-007 risk note).
var registryMu sync.Mutex

// LoadRegistry loads a module's registry. A non-existent file returns an
// empty registry, not an error — initial state of any module is empty.
func LoadRegistry(root, modulePath string) (*ModuleRegistry, error) {
	path := AngleRegistryFilePath(root, modulePath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewModuleRegistry(modulePath), nil
		}
		return nil, fmt.Errorf("read registry %s: %w", path, err)
	}
	var reg ModuleRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("decode registry %s: %w", path, err)
	}
	if reg.Angles == nil {
		reg.Angles = []Angle{}
	}
	if reg.RefactorHistory == nil {
		reg.RefactorHistory = []RefactorHistoryEntry{}
	}
	if reg.ReviveHistory == nil {
		reg.ReviveHistory = []ReviveHistoryEntry{}
	}
	return &reg, nil
}

// SaveRegistry writes a module's registry atomically (temp + rename).
func SaveRegistry(root, modulePath string, reg *ModuleRegistry) error {
	if reg == nil {
		return fmt.Errorf("nil registry")
	}
	if reg.Module != modulePath {
		return fmt.Errorf("module mismatch: registry=%q path=%q", reg.Module, modulePath)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	path := AngleRegistryFilePath(root, modulePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

// ValidateAngleTarget rejects empty and generic-category targets.
// REQ-003 FR-003: target must be a concrete file path, runtime invariant,
// or failure mode — not a one-word category like "security".
func ValidateAngleTarget(target string) error {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return fmt.Errorf("angle target is empty")
	}
	if targetBlacklist[strings.ToLower(trimmed)] {
		return fmt.Errorf("angle target %q is a generic category (blacklisted); must be a concrete file/invariant/failure-mode", trimmed)
	}
	return nil
}

// ValidateAngleID rejects malformed IDs.
func ValidateAngleID(id string) error {
	if !angleIDPattern.MatchString(id) {
		return fmt.Errorf("angle id %q does not match ANG-{MODULE}-{NNN} pattern", id)
	}
	return nil
}

// moduleSuffixForID extracts the MODULE portion of an ANG-{MODULE}-{NNN} id.
// internal/change -> CHANGE; internal/runtime -> RUNTIME; etc. The mapping
// is deterministic per module: uppercase + last path segment.
func moduleSuffixForID(modulePath string) string {
	trimmed := strings.Trim(modulePath, "/")
	if trimmed == "" {
		return "ROOT"
	}
	parts := strings.Split(trimmed, "/")
	last := parts[len(parts)-1]
	return strings.ToUpper(strings.ReplaceAll(last, "-", ""))
}

// nextAngleID returns the next monotonic id for the module.
func nextAngleID(reg *ModuleRegistry, modulePath string) string {
	prefix := "ANG-" + moduleSuffixForID(modulePath) + "-"
	maxNum := 0
	for _, a := range reg.Angles {
		if !strings.HasPrefix(a.ID, prefix) {
			continue
		}
		numStr := strings.TrimPrefix(a.ID, prefix)
		var n int
		if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
			continue
		}
		if n > maxNum {
			maxNum = n
		}
	}
	return fmt.Sprintf("%s%03d", prefix, maxNum+1)
}

// findAngleByID returns a pointer into reg.Angles, or nil if not found.
func findAngleByID(reg *ModuleRegistry, id string) *Angle {
	for i := range reg.Angles {
		if reg.Angles[i].ID == id {
			return &reg.Angles[i]
		}
	}
	return nil
}

// CreateAngle adds a new active angle to the module registry. ID is
// auto-assigned monotonically per module; callers must not pass an ID.
// declaredIn must be a REQ id (e.g. "REQ-002"); declaredAt defaults to now.
type CreateAngleRequest struct {
	ModulePath string
	Statement  string
	Target     string
	DeclaredIn string
	DeclaredAt time.Time
}

// CreateAngle validates and appends a new active angle.
func CreateAngle(root string, req CreateAngleRequest) (*ModuleRegistry, *Angle, error) {
	if strings.TrimSpace(req.Statement) == "" {
		return nil, nil, fmt.Errorf("angle statement is empty")
	}
	if err := ValidateAngleTarget(req.Target); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(req.DeclaredIn) == "" {
		return nil, nil, fmt.Errorf("declared_in is empty (must be a REQ id)")
	}
	reg, err := LoadRegistry(root, req.ModulePath)
	if err != nil {
		return nil, nil, err
	}
	at := req.DeclaredAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	id := nextAngleID(reg, req.ModulePath)
	angle := Angle{
		ID:            id,
		Statement:     req.Statement,
		Target:        req.Target,
		DeclaredIn:    req.DeclaredIn,
		LastAppliedIn: req.DeclaredIn,
		Status:        AngleStatusActive,
		DeclaredAt:    at,
	}
	reg.Angles = append(reg.Angles, angle)
	reg.Version = bumpVersion(reg.Version)
	if err := SaveRegistry(root, req.ModulePath, reg); err != nil {
		return nil, nil, err
	}
	return reg, &angle, nil
}

// ConfirmAngle marks an active angle as still applicable in this REQ round
// by updating LastAppliedIn. Active-only precondition; rejected otherwise.
func ConfirmAngle(root, modulePath, angleID, req string) (*ModuleRegistry, error) {
	if strings.TrimSpace(req) == "" {
		return nil, fmt.Errorf("req is empty")
	}
	reg, err := LoadRegistry(root, modulePath)
	if err != nil {
		return nil, err
	}
	a := findAngleByID(reg, angleID)
	if a == nil {
		return nil, fmt.Errorf("angle %q not found in module %q", angleID, modulePath)
	}
	if a.Status != AngleStatusActive {
		return nil, fmt.Errorf("angle %q is %s; only active angles can be confirmed", angleID, a.Status)
	}
	a.LastAppliedIn = req
	reg.Version = bumpVersion(reg.Version)
	if err := SaveRegistry(root, modulePath, reg); err != nil {
		return nil, err
	}
	return reg, nil
}

// RetractAngle marks an active angle as retracted. Reason is mandatory.
// The angle is NOT removed; status flips and a RefactorHistoryEntry is
// appended (FR-007 append-mostly invariant).
func RetractAngle(root, modulePath, angleID, req, reason string) (*ModuleRegistry, error) {
	if strings.TrimSpace(req) == "" {
		return nil, fmt.Errorf("req is empty")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("retract reason is empty (FR-007)")
	}
	reg, err := LoadRegistry(root, modulePath)
	if err != nil {
		return nil, err
	}
	a := findAngleByID(reg, angleID)
	if a == nil {
		return nil, fmt.Errorf("angle %q not found in module %q", angleID, modulePath)
	}
	if a.Status != AngleStatusActive {
		return nil, fmt.Errorf("angle %q is %s; only active angles can be retracted", angleID, a.Status)
	}
	a.Status = AngleStatusRetracted
	a.RetractReason = reason
	reg.RefactorHistory = append(reg.RefactorHistory, RefactorHistoryEntry{
		AngleID:     angleID,
		REQ:         req,
		Reason:      reason,
		RetractedAt: time.Now().UTC(),
	})
	reg.Version = bumpVersion(reg.Version)
	if err := SaveRegistry(root, modulePath, reg); err != nil {
		return nil, err
	}
	return reg, nil
}

// reviveOnce returns false if the angle has ever been revived before.
// FR-007: revive is allowed at most once per retracted angle.
func (reg *ModuleRegistry) reviveOnce(angleID string) bool {
	for _, e := range reg.ReviveHistory {
		if e.AngleID == angleID {
			return false
		}
	}
	return true
}

// ReviveAngle flips a retracted angle back to active. Allowed exactly once
// per angle; second revive is rejected (FR-007).
func ReviveAngle(root, modulePath, angleID, req string) (*ModuleRegistry, error) {
	if strings.TrimSpace(req) == "" {
		return nil, fmt.Errorf("req is empty")
	}
	reg, err := LoadRegistry(root, modulePath)
	if err != nil {
		return nil, err
	}
	a := findAngleByID(reg, angleID)
	if a == nil {
		return nil, fmt.Errorf("angle %q not found in module %q", angleID, modulePath)
	}
	if a.Status != AngleStatusRetracted {
		return nil, fmt.Errorf("angle %q is %s; only retracted angles can be revived", angleID, a.Status)
	}
	if !reg.reviveOnce(angleID) {
		return nil, fmt.Errorf("angle %q has already been revived once (FR-007 single-revive invariant)", angleID)
	}
	a.Status = AngleStatusActive
	a.RetractReason = ""
	a.LastAppliedIn = req
	reg.ReviveHistory = append(reg.ReviveHistory, ReviveHistoryEntry{
		AngleID:   angleID,
		REQ:       req,
		RevivedAt: time.Now().UTC(),
	})
	reg.Version = bumpVersion(reg.Version)
	if err := SaveRegistry(root, modulePath, reg); err != nil {
		return nil, err
	}
	return reg, nil
}

// BaselineAngle is a view of an active angle suitable for serializing into
// a team manifest's inherited_angles array (REQ-003 FR-013).
type BaselineAngle struct {
	ID            string `json:"id" yaml:"id"`
	Module        string `json:"module" yaml:"module"`
	Statement     string `json:"statement" yaml:"statement"`
	Target        string `json:"target" yaml:"target"`
	LastAppliedIn string `json:"last_applied_in" yaml:"last_applied_in"`
}

// ListBaselineFor aggregates all status=active angles across the given
// module paths. This is the inherited-baseline input for review team
// manifests (REQ-003 FR-006).
//
// Modules with no registry file contribute zero angles. The output slice
// is sorted by (module, id) for deterministic output.
func ListBaselineFor(root string, modulePaths []string) ([]BaselineAngle, error) {
	out := []BaselineAngle{}
	for _, mp := range modulePaths {
		reg, err := LoadRegistry(root, mp)
		if err != nil {
			return nil, err
		}
		for _, a := range reg.Angles {
			if a.Status != AngleStatusActive {
				continue
			}
			out = append(out, BaselineAngle{
				ID:            a.ID,
				Module:        mp,
				Statement:     a.Statement,
				Target:        a.Target,
				LastAppliedIn: a.LastAppliedIn,
			})
		}
	}
	return out, nil
}

// bumpVersion increments a "vN.N.N" registry version's patch component.
// The registry version is monotonic per module and records any mutation
// (create, confirm, retract, revive).
func bumpVersion(v string) string {
	if !strings.HasPrefix(v, "v") {
		return "v0.0.1"
	}
	rest := strings.TrimPrefix(v, "v")
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return "v0.0.1"
	}
	var maj, min, pat int
	if _, err := fmt.Sscanf(parts[0]+"."+parts[1]+"."+parts[2], "%d.%d.%d", &maj, &min, &pat); err != nil {
		return "v0.0.1"
	}
	pat++
	return fmt.Sprintf("v%d.%d.%d", maj, min, pat)
}
