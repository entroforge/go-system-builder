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
			_, err := checkexec.ReadProfileFile(b.MainRoot, ref.Path)
			if err != nil {
				return e, fmt.Errorf("check profile source unavailable: %s", ref.Path)
			}
		}
	}
	e.CheckProfiles = append([]checkexec.Profile(nil), profiles...)
	for i := range e.CheckProfiles {
		e.CheckProfiles[i].Dependencies = append(make([]checkexec.FileRef, 0, len(e.CheckProfiles[i].Dependencies)), e.CheckProfiles[i].Dependencies...)
		e.CheckProfiles[i].Fixtures = append(make([]checkexec.FileRef, 0, len(e.CheckProfiles[i].Fixtures)), e.CheckProfiles[i].Fixtures...)
	}
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
