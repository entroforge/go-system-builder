package workspace

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func (b *ExecutionRegistry) FreezeCheckProfiles(e Execution, profiles []checkexec.Profile) (Execution, error) {
	location := e.CheckLocation
	if location == "" {
		location = "worker"
	}
	if err := checkexec.ValidateProfiles(profiles, e.Checks, location); err != nil {
		return e, err
	}
	for _, p := range profiles {
		if _, err := p.ActualEnvironment(); err != nil {
			return e, err
		}
		for _, ref := range append(append([]checkexec.FileRef{}, p.Dependencies...), p.Fixtures...) {
			data, err := checkexec.ReadProfileFile(b.MainRoot, ref.Path)
			if err != nil || checkexec.Hash(data) != ref.SHA256 {
				return e, fmt.Errorf("check profile source changed: %s", ref.Path)
			}
			if old, ok := e.Inputs[ref.Path]; ok && old != ref.SHA256 {
				return e, fmt.Errorf("profile input differs from frozen source")
			}
			e.Inputs[ref.Path] = ref.SHA256
		}
	}
	e.CheckProfiles = append([]checkexec.Profile(nil), profiles...)
	return e, nil
}
func ProfileAt(e Execution, index int) (checkexec.Profile, bool) {
	for _, p := range e.CheckProfiles {
		if p.CheckIndex == index {
			return p, true
		}
	}
	return checkexec.Profile{}, false
}
