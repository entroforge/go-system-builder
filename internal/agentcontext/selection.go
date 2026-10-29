// Package agentcontext projects knowledge sources; it grants no capability.
package agentcontext

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/entroforge/go-system-builder/internal/catalog"
)

//go:embed agent-context-profiles.json
var profilesJSON []byte

// ProfileBytes returns the actual profile embedded in the executing binary.
// Installed projects need no copy of the harness source tree to prove it.
func ProfileBytes() []byte { return append([]byte(nil), profilesJSON...) }

type profile struct {
	Core   []string `json:"core"`
	Legacy []string `json:"legacy"`
}
type profileSet struct {
	Version          string              `json:"version"`
	Mode             string              `json:"mode"`
	Roles            map[string]profile  `json:"roles"`
	Responsibilities map[string][]string `json:"responsibility_skills"`
}

var profiles = func() profileSet {
	var p profileSet
	if err := json.Unmarshal(profilesJSON, &p); err != nil {
		panic(err)
	}
	return p
}()

type Skill struct {
	Name    string   `json:"name"`
	Source  string   `json:"source"`
	SHA256  string   `json:"sha256"`
	Reasons []string `json:"reasons"`
}
type Selection struct {
	ProfileVersion         string   `json:"profile_version"`
	ProfileSHA256          string   `json:"profile_sha256"`
	Mode                   string   `json:"mode"`
	Role                   string   `json:"role"`
	Selected               []Skill  `json:"selected"`
	LegacySkills           []string `json:"legacy_skills"`
	AuthorizationSkillRefs []string `json:"authorization_skill_refs"`
}

func LegacySkills(role string) []string {
	p, ok := profiles.Roles[role]
	if !ok {
		return []string{"agent-dispatch"}
	}
	return append([]string{}, p.Legacy...)
}
func RequiredSkills(responsibility string) []string {
	return append([]string{}, profiles.Responsibilities[responsibility]...)
}

// Select consumes only registered facts. It does not parse a project template
// or infer stack/risk from prose, and never substitutes the authorization set.
func Select(root, role string, responsibilities, registered []string) (Selection, error) {
	p, known := profiles.Roles[role]
	mode := profiles.Mode
	if !known {
		mode = "legacy"
		p.Core = LegacySkills(role)
	}
	reasons := map[string][]string{}
	add := func(name, reason string) {
		for _, existing := range reasons[name] {
			if existing == reason {
				return
			}
		}
		reasons[name] = append(reasons[name], reason)
	}
	for _, name := range p.Core {
		add(name, "core:"+role)
	}
	for _, responsibility := range responsibilities {
		for _, name := range RequiredSkills(responsibility) {
			add(name, "responsibility:"+responsibility)
		}
	}
	for _, name := range registered {
		add(name, "assignment:skill_refs")
	}
	result := Selection{ProfileVersion: profiles.Version, ProfileSHA256: fmt.Sprintf("%x", sha256.Sum256(profilesJSON)), Mode: mode, Role: role, Selected: []Skill{}, LegacySkills: LegacySkills(role), AuthorizationSkillRefs: append([]string{}, registered...)}
	names := make([]string, 0, len(reasons))
	for name := range reasons {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data, source, err := catalog.ReadSkillAsset(root, name)
		if err != nil {
			return result, err
		}
		sort.Strings(reasons[name])
		result.Selected = append(result.Selected, Skill{Name: name, Source: source, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Reasons: reasons[name]})
	}
	return result, nil
}
