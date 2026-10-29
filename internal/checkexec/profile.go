package checkexec

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

type FileRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type EnvFact struct {
	Name  string  `json:"name"`
	Value *string `json:"value"`
}
type Profile struct {
	ReuseMode         string    `json:"reuse_mode,omitempty"`
	ID                string    `json:"id"`
	Version           string    `json:"version"`
	CheckIndex        int       `json:"check_index"`
	Command           string    `json:"command"`
	Location          string    `json:"location"`
	TimeoutSeconds    int       `json:"timeout_seconds"`
	EnvironmentPolicy string    `json:"environment_policy"`
	Environment       []EnvFact `json:"environment"`
	Dependencies      []FileRef `json:"dependencies"`
	Fixtures          []FileRef `json:"fixtures"`
	InputClosure      string    `json:"input_closure"`
	SideEffects       string    `json:"side_effects"`
	ReusePolicy       string    `json:"reuse_policy"`
}

var AllowedEnvironment = map[string]bool{"PATH": true, "LANG": true, "LC_ALL": true, "TZ": true, "CI": true, "HOME": true, "GOPROXY": true, "GOSUMDB": true, "GOTOOLCHAIN": true, "GOFLAGS": true, "GOOS": true, "GOARCH": true, "CGO_ENABLED": true, "GOCACHE": true, "GOMODCACHE": true, "GOPATH": true, "GOROOT": true, "NODE_ENV": true, "E2E_WORKERS": true, "E2E_RUNTIME_OWNER": true, "E2E_RUNTIME_SERVICES": true}

func Hash(data []byte) string    { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func (p Profile) Digest() string { data, _ := json.Marshal(p); return Hash(data) }
func ValidateProfiles(profiles []Profile, checks []string, location string) error {
	seen := map[int]bool{}
	ids := map[string]bool{}
	for _, p := range profiles {
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("check-profile.schema.json", data); err != nil {
			return err
		}
		if p.CheckIndex < 0 || p.CheckIndex >= len(checks) || checks[p.CheckIndex] != p.Command || strings.HasPrefix(strings.TrimSpace(p.Command), "locked:") {
			return fmt.Errorf("check profile must match its original executable required_checks index")
		}
		if seen[p.CheckIndex] || ids[p.ID] {
			return fmt.Errorf("check profile index/identity is duplicated")
		}
		seen[p.CheckIndex] = true
		ids[p.ID] = true
		if location != "" && p.Location != location {
			return fmt.Errorf("profile location differs from original execution runner")
		}
		names := map[string]bool{}
		for _, fact := range p.Environment {
			if !AllowedEnvironment[fact.Name] || names[fact.Name] {
				return fmt.Errorf("profile environment is not a unique approved whitelist")
			}
			names[fact.Name] = true
		}
		for _, ref := range append(append([]FileRef{}, p.Dependencies...), p.Fixtures...) {
			if !safeProfilePath(ref.Path) {
				return fmt.Errorf("profile source is not a repository dependency/fixture path")
			}
		}
		if p.ReusePolicy != "never" && (p.EnvironmentPolicy != "declared_only" || p.SideEffects != "none") {
			return fmt.Errorf("profile reuse needs declared-only environment and no side effects")
		}
		if p.ReuseMode == "enabled" && (p.Location != "worker" || p.ReusePolicy != "same_input_same_responsibility" || !ClosedStaticCheck(p.Command)) {
			return fmt.Errorf("enabled reuse requires an original Worker closed static profile")
		}
	}
	return nil
}
func safeProfilePath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\:*?[]{}\x00") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || part == ".git" || part == ".claude" || part == ".worktrees" || part == ".env" || strings.HasPrefix(part, ".env.") {
			return false
		}
	}
	return true
}
func (p Profile) ActualEnvironment() ([]EnvFact, error) {
	facts := []EnvFact{}
	for _, declared := range p.Environment {
		value, exists := os.LookupEnv(declared.Name)
		if declared.Value == nil {
			if exists {
				return nil, fmt.Errorf("profile expects environment %s unset", declared.Name)
			}
			facts = append(facts, declared)
		} else {
			if !exists || value != *declared.Value {
				return nil, fmt.Errorf("profile environment %s differs from approved fact", declared.Name)
			}
			v := value
			facts = append(facts, EnvFact{Name: declared.Name, Value: &v})
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].Name < facts[j].Name })
	return facts, nil
}
func (p Profile) CommandEnvironment() ([]string, error) {
	facts, err := p.ActualEnvironment()
	if err != nil {
		return nil, err
	}
	if p.EnvironmentPolicy == "inherited" {
		return os.Environ(), nil
	}
	env := []string{}
	for _, f := range facts {
		if f.Value != nil {
			env = append(env, f.Name+"="+*f.Value)
		}
	}
	return env, nil
}

type profileKey struct{}

func WithProfile(ctx context.Context, p Profile) context.Context {
	return context.WithValue(ctx, profileKey{}, p)
}
func CurrentProfile(ctx context.Context) (Profile, bool) {
	p, ok := ctx.Value(profileKey{}).(Profile)
	return p, ok
}
func CheckTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if p, ok := CurrentProfile(ctx); ok {
		return context.WithTimeout(ctx, time.Duration(p.TimeoutSeconds)*time.Second)
	}
	return context.WithTimeout(ctx, 5*time.Minute)
}

type Binding struct {
	RuntimeID           string `json:"runtime_id"`
	Generation          int    `json:"baseline_generation"`
	AssignmentID        string `json:"assignment_id"`
	Owner               string `json:"owner"`
	ExecutionGeneration int    `json:"execution_generation"`
	Responsibility      string `json:"responsibility"`
}
type bindingKey struct{}

func WithBinding(ctx context.Context, b Binding) context.Context {
	return context.WithValue(ctx, bindingKey{}, b)
}
func CurrentBinding(ctx context.Context) (Binding, bool) {
	b, ok := ctx.Value(bindingKey{}).(Binding)
	return b, ok
}
