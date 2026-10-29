// Package projectconventions projects explicit engineering choices from their
// original sources. It owns no approvals, execution or lifecycle state.
package projectconventions

const Version = "1.0.0"
const RegistryPath = "docs/rules/project-conventions.json"
const IndexPath = "docs/rules/README.md"

type Reference struct {
	Kind    string `json:"kind,omitempty"`
	ID      string `json:"id,omitempty"`
	Path    string `json:"path"`
	Anchor  string `json:"anchor,omitempty"`
	Version string `json:"version,omitempty"`
	SHA256  string `json:"sha256"`
}
type Entry struct {
	ID          string      `json:"id"`
	Topic       string      `json:"topic"`
	Scope       []string    `json:"scope"`
	Default     string      `json:"default"`
	SourceRefs  []Reference `json:"source_refs"`
	ExampleRefs []Reference `json:"example_refs"`
	Exceptions  []string    `json:"exceptions"`
	Status      string      `json:"status"`
}
type Registry struct {
	SchemaVersion string  `json:"schema_version"`
	Entries       []Entry `json:"entries"`
}
type Source struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256,omitempty"`
	SourceMode string `json:"source_mode"`
	Status     string `json:"status"`
}
type Relevant struct {
	Entry     Entry    `json:"entry"`
	Status    string   `json:"status"`
	Questions []string `json:"questions"`
}
type Projection struct {
	ResolverVersion string     `json:"resolver_version"`
	TaskID          string     `json:"task_id"`
	TaskRef         string     `json:"task_ref"`
	RegistryStatus  string     `json:"registry_status"`
	Sources         []Source   `json:"sources"`
	Entries         []Relevant `json:"entries"`
	Questions       []string   `json:"questions"`
}
type Request struct {
	TaskID, TaskRef string
	Scope           []string
	Reading         []Reference
	// ApprovedRefs comes from the original registered document snapshot, never
	// from status claims in a convention registry or a project-map template.
	ApprovedRefs []Reference
}
