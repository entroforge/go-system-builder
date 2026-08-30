// errors.go improves schema-validation failure readability. The underlying
// library (santhosh-tekuri/jsonschema) renders a failed top-level oneOf by
// enumerating every branch's full error tree; for the envelope schemas
// (agent-message, review-evidence) that buries the one real mistake under
// pages of irrelevant branch noise (L3-S7 §13 known-friction). When the
// instance identifies its target branch — discriminator fields
// kind/type/event/message_type rule out every other branch — only the
// closest branch's real errors are reported. When no branch is identifiable
// the output degrades to a truncated summary (branch names + first error per
// branch). The full per-branch error is never discarded: set
// LOOP_HARNESS_SCHEMA_VERBOSE=1 to bypass pruning.
//
// The library wraps many of its root-level validations in a top-level
// *kind.Schema marker that holds the schema itself as its single cause.
// That wrapping makes "root-level oneOf" an unreliable signal: in some
// envelopes the discriminator oneOf sits one or two levels below the
// marker, and pruning only at the top skips it. This implementation walks
// the entire ValidationError subtree, locates the shallowest discriminator
// failure (no-match oneOf, OR zero-match anyOf), and prunes at that node.
// anyOf failures where at least one branch DID match are left alone —
// they usually represent legitimate partial-validity errors and pruning
// would hide real problems.
package schema

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// SchemaVerboseEnv disables oneOf/anyOf pruning when set to a non-empty value.
const SchemaVerboseEnv = "LOOP_HARNESS_SCHEMA_VERBOSE"

// discriminatorFields are the object properties that identify which branch
// an instance intends to be (envelope kind markers).
var discriminatorFields = map[string]bool{
	"kind":         true,
	"type":         true,
	"event":        true,
	"message_type": true,
}

// branchNamePattern extracts the $defs name from a branch schema URL, e.g.
// "…/agent-message.schema.json#/$defs/completionReport" → "completionReport".
var branchNamePattern = regexp.MustCompile(`#/\$defs/([^/]+)`)

// verboseFooter is appended to every pruning output so users can always
// discover the escape hatch without reading source code.
const verboseFooter = "set %s=1 for the full per-branch error"

// pruneValidationError rewrites a validation failure for readability. It
// walks the entire ValidationError subtree looking for the shallowest
// no-match oneOf or zero-match anyOf with at least two causes (a real
// discriminator split, not an internal fan-out) and prunes that node.
// Every other error passes through unchanged. The returned error unwraps
// to the original, so errors.As on *jsonschema.ValidationError still works.
func pruneValidationError(err error) error {
	if os.Getenv(SchemaVerboseEnv) != "" {
		return err
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	target, ok := findDiscriminatorFailure(ve)
	if !ok {
		return err
	}
	switch target.ErrorKind.(type) {
	case *kind.OneOf:
		return renderOneOfPruning(target, err)
	case *kind.AnyOf:
		return renderAnyOfPruning(target, err)
	default:
		return err
	}
}

// findDiscriminatorFailure walks the subtree depth-first and returns the
// shallowest node that looks like a discriminator-style failure: a oneOf
// with no matched branch, or an anyOf whose branches all failed. The
// returned boolean is false when no such node exists.
//
// "Shallowest" means lowest depth in the tree — pruning a top-level
// discriminator is almost always what the operator wants. We require at
// least two causes so we don't accidentally rewrite single-branch
// oneOfs/anyOfs that exist for other reasons.
func findDiscriminatorFailure(root *jsonschema.ValidationError) (*jsonschema.ValidationError, bool) {
	var best *jsonschema.ValidationError
	var bestDepth int
	walkAtDepth(root, 0, func(node *jsonschema.ValidationError, depth int) {
		if len(node.Causes) < 2 {
			return
		}
		switch k := node.ErrorKind.(type) {
		case *kind.OneOf:
			if len(k.Subschemas) > 0 {
				return
			}
		case *kind.AnyOf:
			// anyOf only fails when every branch failed; the v6 kind has no
			// Subschemas field, so any failure is by definition zero-match.
		default:
			return
		}
		if best == nil || depth < bestDepth {
			best = node
			bestDepth = depth
		}
	})
	return best, best != nil
}

// walkAtDepth visits every node in the error subtree, passing its depth so
// the visitor can prefer shallow nodes.
func walkAtDepth(node *jsonschema.ValidationError, depth int, visit func(*jsonschema.ValidationError, int)) {
	visit(node, depth)
	for _, cause := range node.Causes {
		walkAtDepth(cause, depth+1, visit)
	}
}

// renderOneOfPruning produces the human-readable oneOf failure: closest
// branch's real errors plus a list of ruled-out branches, OR a per-branch
// summary when no branch is identifiable. The original error is preserved
// in the unwrap chain so callers can still type-assert *ValidationError.
func renderOneOfPruning(target *jsonschema.ValidationError, original error) error {
	branches := analyzeBranches(target.Causes)
	chosen := closestBranch(branches)

	var sb strings.Builder
	fmt.Fprintf(&sb, "'oneOf' failed, none matched at '%s'", jsonPointer(target.InstanceLocation))
	if chosen >= 0 {
		fmt.Fprintf(&sb, "; the instance is closest to branch %q:", branches[chosen].name)
		for _, leaf := range leafErrors(target.Causes[chosen]) {
			fmt.Fprintf(&sb, "\n  %s", leaf)
		}
		others := make([]string, 0, len(branches)-1)
		for i, branch := range branches {
			if i != chosen {
				others = append(others, branch.name)
			}
		}
		fmt.Fprintf(&sb, "\n(other branches ruled out: %s; %s)",
			strings.Join(others, ", "), fmt.Sprintf(verboseFooter, SchemaVerboseEnv))
	} else {
		names := make([]string, 0, len(branches))
		for _, branch := range branches {
			names = append(names, branch.name)
		}
		fmt.Fprintf(&sb, "; no target branch is identifiable (branches: %s). First error per branch:",
			strings.Join(names, ", "))
		for _, branch := range branches {
			fmt.Fprintf(&sb, "\n  %s: %s", branch.name, branch.firstError)
		}
		fmt.Fprintf(&sb, "\n(%s)", fmt.Sprintf(verboseFooter, SchemaVerboseEnv))
	}
	return &prunedError{rendered: sb.String(), cause: original}
}

// renderAnyOfPruning produces the same shape for anyOf failures: pick the
// branch closest to the instance using the same discriminator logic, or
// fall back to a per-branch summary when no branch stands out.
func renderAnyOfPruning(target *jsonschema.ValidationError, original error) error {
	branches := analyzeBranches(target.Causes)
	chosen := closestBranch(branches)

	var sb strings.Builder
	fmt.Fprintf(&sb, "'anyOf' failed, none matched at '%s'", jsonPointer(target.InstanceLocation))
	if chosen >= 0 {
		fmt.Fprintf(&sb, "; the instance is closest to branch %q:", branches[chosen].name)
		for _, leaf := range leafErrors(target.Causes[chosen]) {
			fmt.Fprintf(&sb, "\n  %s", leaf)
		}
		others := make([]string, 0, len(branches)-1)
		for i, branch := range branches {
			if i != chosen {
				others = append(others, branch.name)
			}
		}
		fmt.Fprintf(&sb, "\n(other branches ruled out: %s; %s)",
			strings.Join(others, ", "), fmt.Sprintf(verboseFooter, SchemaVerboseEnv))
	} else {
		names := make([]string, 0, len(branches))
		for _, branch := range branches {
			names = append(names, branch.name)
		}
		fmt.Fprintf(&sb, "; no branch matches well enough (branches: %s). First error per branch:",
			strings.Join(names, ", "))
		for _, branch := range branches {
			fmt.Fprintf(&sb, "\n  %s: %s", branch.name, branch.firstError)
		}
		fmt.Fprintf(&sb, "\n(%s)", fmt.Sprintf(verboseFooter, SchemaVerboseEnv))
	}
	return &prunedError{rendered: sb.String(), cause: original}
}

// prunedError carries the readable rendering while preserving the original
// validation error in the unwrap chain.
type prunedError struct {
	rendered string
	cause    error
}

func (e *prunedError) Error() string { return e.rendered }
func (e *prunedError) Unwrap() error { return e.cause }

// branchInfo is the per-branch analysis of a oneOf/anyOf alternative.
type branchInfo struct {
	name       string
	leafCount  int
	ruledOut   bool // fails on a discriminator field — the instance cannot intend this branch
	firstError string
}

// analyzeBranches profiles every branch of a failed oneOf/anyOf.
func analyzeBranches(causes []*jsonschema.ValidationError) []branchInfo {
	branches := make([]branchInfo, 0, len(causes))
	for i, cause := range causes {
		info := branchInfo{name: branchName(cause, i)}
		walkErrors(cause, func(node *jsonschema.ValidationError) {
			if len(node.Causes) == 0 {
				info.leafCount++
				if info.firstError == "" {
					info.firstError = node.Error()
				}
				if isDiscriminatorFailure(node) {
					info.ruledOut = true
				}
			}
		})
		if info.firstError == "" {
			info.firstError = cause.Error()
		}
		branches = append(branches, info)
	}
	return branches
}

// closestBranch picks the branch the instance most likely targets: the
// single branch not ruled out by a discriminator mismatch, else the unique
// branch with the fewest concrete errors. Returns -1 when undecidable.
func closestBranch(branches []branchInfo) int {
	var candidates []int
	for i, branch := range branches {
		if !branch.ruledOut {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	if len(candidates) == 0 {
		return -1
	}
	best, bestCount, tied := -1, 0, false
	for _, i := range candidates {
		if best < 0 || branches[i].leafCount < bestCount {
			best, bestCount, tied = i, branches[i].leafCount, false
		} else if branches[i].leafCount == bestCount {
			tied = true
		}
	}
	if tied {
		return -1
	}
	return best
}

// isDiscriminatorFailure reports whether the node is a const/enum mismatch
// on a discriminator field (kind/type/event/message_type) — proof that the
// instance does not intend this branch.
func isDiscriminatorFailure(node *jsonschema.ValidationError) bool {
	switch node.ErrorKind.(type) {
	case *kind.Const, *kind.Enum:
	default:
		return false
	}
	if len(node.InstanceLocation) == 0 {
		return false
	}
	return discriminatorFields[node.InstanceLocation[len(node.InstanceLocation)-1]]
}

// walkErrors visits every node in the error subtree.
func walkErrors(node *jsonschema.ValidationError, visit func(*jsonschema.ValidationError)) {
	visit(node)
	for _, cause := range node.Causes {
		walkErrors(cause, visit)
	}
}

// leafErrors renders every concrete (cause-free) failure in the subtree,
// preserving the library's real messages.
func leafErrors(node *jsonschema.ValidationError) []string {
	var leaves []string
	walkErrors(node, func(current *jsonschema.ValidationError) {
		if len(current.Causes) == 0 {
			leaves = append(leaves, current.Error())
		}
	})
	return leaves
}

// branchName derives a human branch label from the branch schema URL,
// falling back to the positional index.
func branchName(cause *jsonschema.ValidationError, index int) string {
	if match := branchNamePattern.FindStringSubmatch(cause.SchemaURL); match != nil {
		return match[1]
	}
	// The branch may be wrapped in a Reference node; look one level down.
	for _, child := range cause.Causes {
		if match := branchNamePattern.FindStringSubmatch(child.SchemaURL); match != nil {
			return match[1]
		}
	}
	return fmt.Sprintf("branch #%d", index)
}

// jsonPointer renders an instance location as a JSON pointer.
func jsonPointer(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	return "/" + strings.Join(tokens, "/")
}
