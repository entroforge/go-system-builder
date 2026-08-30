// Package recovery contains the read-only model and planning primitives for
// rebuilding a damaged Loop Runtime.
package recovery

import (
	"errors"
	"fmt"
)

const (
	// SchemaVersion is the RR-A model schema version.
	SchemaVersion = "1.0.0"

	// InputKindREQ identifies the explicitly selected locked requirement.
	InputKindREQ = "req"
	// InputKindRuntimeState identifies the active Runtime projection.
	InputKindRuntimeState = "runtime_state"
	// InputKindRuntimeJournal identifies the append-only Runtime journal.
	InputKindRuntimeJournal = "runtime_journal"
	// InputKindRuntimePending identifies a Runtime pending-operation marker.
	InputKindRuntimePending = "runtime_pending"
	// InputKindArtifact identifies a document or evidence artifact.
	InputKindArtifact = "artifact"

	// PlanBaseConservativeSeed is the RR-A fallback base mode.
	PlanBaseConservativeSeed = "conservative_seed"
	// PlanBaseArtifactReconstruction identifies a seed enriched from durable
	// artifacts and advanced only through the production Controller.
	PlanBaseArtifactReconstruction = "artifact_reconstruction"
	// PlanConfidenceConservative indicates that no cursor replay was performed.
	PlanConfidenceConservative = "conservative"
	// PlanConfidenceFormalReplay indicates that the target cursor was reached
	// by executing production gates and transitions against a staging Runtime.
	PlanConfidenceFormalReplay = "formal_replay"
	// PlanSeedCursor is the first cursor from which later replay can begin.
	PlanSeedCursor = "planning.design"
)

var (
	// ErrInvalidRoot means the inspection root cannot be used as a repository.
	ErrInvalidRoot = errors.New("invalid recovery root")
	// ErrInvalidREQ means the selected REQ cannot be safely bound.
	ErrInvalidREQ = errors.New("invalid recovery req")
	// ErrPathOutsideRepository means a requested path escapes the repository.
	ErrPathOutsideRepository = errors.New("recovery path is outside repository")
	// ErrREQFilename means the selected file name does not identify a REQ.
	ErrREQFilename = errors.New("recovery req filename is invalid")
	// ErrREQStatusMissing means the selected REQ has no readable status field.
	ErrREQStatusMissing = errors.New("recovery req status is missing")
	// ErrREQNotLocked means the selected REQ is not locked.
	ErrREQNotLocked = errors.New("recovery req is not locked")
	// ErrREQVersionMissing means the selected REQ has no readable version.
	ErrREQVersionMissing = errors.New("recovery req version is missing")
	// ErrInvalidInventory means an inventory cannot be converted into a plan.
	ErrInvalidInventory = errors.New("invalid recovery inventory")
)

// ValidationError carries a stable sentinel and field-level context for an
// invalid recovery input. Callers can use errors.Is and errors.As to classify
// the error without parsing its message.
type ValidationError struct {
	Code   error
	Field  string
	Path   string
	Reason string
	Cause  error
}

// Error implements error.
func (e *ValidationError) Error() string {
	if e == nil {
		return "recovery validation failed"
	}
	reason := e.Reason
	if reason == "" && e.Code != nil {
		reason = e.Code.Error()
	}
	if reason == "" {
		reason = "invalid recovery input"
	}
	if e.Field == "" {
		return fmt.Sprintf("recovery validation failed: %s", reason)
	}
	return fmt.Sprintf("recovery validation failed for %s: %s", e.Field, reason)
}

// Unwrap exposes both the stable classification and the underlying cause.
func (e *ValidationError) Unwrap() []error {
	if e == nil {
		return nil
	}
	errs := make([]error, 0, 2)
	if e.Code != nil {
		errs = append(errs, e.Code)
	}
	if e.Cause != nil {
		errs = append(errs, e.Cause)
	}
	return errs
}

// REQBinding is the verified identity of the explicitly selected locked REQ.
type REQBinding struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// InventoryInput is a content-addressed repository-relative recovery input.
type InventoryInput struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Inventory is the read-only result of inspecting a repository for recovery.
// Root is intentionally excluded from serialized plans because it is a host
// location, not a recovery fact.
type Inventory struct {
	SchemaVersion string           `json:"schema_version"`
	REQ           REQBinding       `json:"req"`
	Inputs        []InventoryInput `json:"inputs"`
	Root          string           `json:"-"`
}

// Plan is the deterministic, conservative RR-A recovery proposal. It does
// not mutate files and does not claim that any cursor replay has occurred.
type Plan struct {
	SchemaVersion string           `json:"schema_version"`
	REQ           REQBinding       `json:"req"`
	Inputs        []InventoryInput `json:"inputs"`
	BaseMode      string           `json:"base_mode"`
	TargetCursor  string           `json:"target_cursor"`
	Confidence    string           `json:"confidence"`
	PlanSHA256    string           `json:"plan_sha256"`
}

// Hash returns the content hash assigned to the plan.
func (p Plan) Hash() string {
	return p.PlanSHA256
}
