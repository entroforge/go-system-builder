package checkexec

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/schema"
	"os"
	"path/filepath"
	"strings"
)

type Observation struct {
	Permissions       *PermissionObservation `json:"permissions,omitempty"`
	ProfileID         string                 `json:"profile_id"`
	ProfileVersion    string                 `json:"profile_version"`
	Environment       []EnvFact              `json:"environment"`
	VerificationScope string                 `json:"verification_scope"`
	ReuseEligible     bool                   `json:"reuse_eligible"`
}

func ObserveProfile(ctx context.Context, root string, p Profile) (Observation, error) {
	out := Observation{ProfileID: p.ID, ProfileVersion: p.Version, VerificationScope: "actual invocation and declared environment; input equivalence is not established"}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	facts, err := p.ActualEnvironment()
	if err != nil {
		return out, err
	}
	out.Environment = facts
	out.ReuseEligible = p.ReuseMode == "enabled" && p.Location == "worker" && p.ReusePolicy == "same_input_same_responsibility" && p.EnvironmentPolicy == "declared_only" && p.SideEffects == "none" && p.InputClosure == "whole_tree" && ClosedStaticCheck(p.Command)
	data, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("check-observation.schema.json", data); err != nil {
		return out, err
	}
	return out, nil
}

func entranceOrEmpty(command string) string {
	tool, ok := LiteralEntrance(command)
	if !ok || builtins[tool] {
		return ""
	}
	return tool
}
func ReadProfileFile(root, path string) ([]byte, error) {
	if !safeProfilePath(path) {
		return nil, fmt.Errorf("unsafe profile input")
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	actual, err := filepath.EvalSymlinks(filepath.Join(base, path))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, actual)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("profile input escapes source root")
	}
	info, err := os.Stat(actual)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 512<<20 {
		return nil, fmt.Errorf("profile input is not a bounded regular file")
	}
	return os.ReadFile(actual)
}
