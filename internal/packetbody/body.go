// Package packetbody stores immutable original packet JSON separately from
// Runtime's responsibility index. Bodies never prove issuance by themselves.
package packetbody

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const (
	StorageFormat       = "packet-body-ref-v1"
	BodyFormat          = "packet-json-v1"
	Directory           = ".claude/packet-bodies/"
	Limit         int64 = 20 << 20
)

type Reference struct {
	Format string `json:"format"`
	Path   string `json:"path"`
}

// IsBodyPath recognizes the current storage root and Writer-selected
// alternative roots, without treating any path as proof of issuance.
func IsBodyPath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || path.Clean(value) != value {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) < 3 || parts[0] != ".claude" {
		return false
	}
	if parts[1] == "packet-bodies" {
		return true
	}
	const prefix = "packet-bodies-alt-"
	if !strings.HasPrefix(parts[1], prefix) {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(parts[1], prefix))
	return err == nil && n > 0 && strconv.Itoa(n) == strings.TrimPrefix(parts[1], prefix)
}

type rootKey struct{}
type resolverKey struct{}
type Resolver func(context.Context, Reference) ([]byte, error)

func WithRoot(ctx context.Context, root string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, rootKey{}, root)
}
func Root(ctx context.Context) string { root, _ := ctx.Value(rootKey{}).(string); return root }

// WithResolver is reserved for independently validated recovery sources.
func WithResolver(ctx context.Context, resolver Resolver) context.Context {
	return context.WithValue(ctx, resolverKey{}, resolver)
}

func ReferenceFromIndex(row map[string]any) (Reference, bool, error) {
	format, present := row["storage_format"]
	_, hasRef := row["body_ref"]
	if !present && !hasRef {
		return Reference{}, false, nil
	}
	if format != StorageFormat {
		return Reference{}, false, fmt.Errorf("unsupported packet storage format %q", format)
	}
	data, err := json.Marshal(row["body_ref"])
	if err != nil {
		return Reference{}, false, err
	}
	var ref Reference
	if err := json.Unmarshal(data, &ref); err != nil {
		return ref, false, fmt.Errorf("invalid packet body reference: %w", err)
	}
	if !IsBodyPath(ref.Path) || ref.Format != BodyFormat {
		return ref, false, errors.New("invalid packet body reference path/format/size")
	}
	return ref, true, nil
}

// Immutable removes only S7's mutable reception projection.
func Immutable(row map[string]any) map[string]any {
	if row["template_id"] != "s7-review-result-v1" || row["review_result_reception"] == nil {
		return row
	}
	body := make(map[string]any, len(row)-1)
	for key, value := range row {
		if key != "review_result_reception" {
			body[key] = value
		}
	}
	return body
}

func Read(ctx context.Context, row map[string]any) (map[string]any, error) {
	ref, external, err := ReferenceFromIndex(row)
	if err != nil || !external {
		return Immutable(row), err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var data []byte
	if resolver, ok := ctx.Value(resolverKey{}).(Resolver); ok {
		data, err = resolver(ctx, ref)
	} else if root := Root(ctx); root != "" {
		data, err = workpacket.ReadFile(root, ref.Path, Limit)
	} else {
		return nil, errors.New("packet body reference requires the original repository root")
	}
	if err != nil {
		return nil, fmt.Errorf("read original packet body %s: %w", ref.Path, err)
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	if err := verifyRegisteredResponsibility(row, body); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}

// verifyRegisteredResponsibility checks only coordinates used to select the
// issued responsibility. Professional inputs and unrelated index attributes
// are not evidence and do not become an extra acceptance condition.
func verifyRegisteredResponsibility(index, body map[string]any) error {
	check := func(left, right map[string]any, keys ...string) error {
		for _, key := range keys {
			l, lok := left[key]
			r, rok := right[key]
			if !lok && !rok {
				continue
			}
			lb, le := json.Marshal(l)
			rb, re := json.Marshal(r)
			if !lok || !rok || le != nil || re != nil || !bytes.Equal(lb, rb) {
				return fmt.Errorf("packet body responsibility %s differs from Runtime index", key)
			}
		}
		return nil
	}
	if err := check(index, body, "packet_id", "template_id", "answer_path", "view_path"); err != nil {
		return err
	}
	indexBinding, _ := index["binding"].(map[string]any)
	bodyBinding, _ := body["binding"].(map[string]any)
	if indexSources, ok := indexBinding["sources"].(map[string]any); ok {
		indexBinding = indexSources
	}
	if bodySources, ok := bodyBinding["sources"].(map[string]any); ok {
		bodyBinding = bodySources
	}
	if err := check(indexBinding, bodyBinding, "runtime_id", "baseline_generation", "owner_agent_id", "owner", "assignment_id", "repair_assignment_id", "manifest_assignment_id"); err != nil {
		return err
	}
	if locator, ok := index["execution_locator"].(map[string]any); ok {
		inputs, _ := body["inputs"].(map[string]any)
		execution, _ := inputs["execution"].(map[string]any)
		if err := check(locator, execution, "assignment_id", "execution_generation", "worktree_path"); err != nil {
			return err
		}
	}
	return nil
}

func Index(body map[string]any, ref Reference) map[string]any {
	index := make(map[string]any, len(body))
	// The index is the system's current responsibility and locators, never a
	// copy of arbitrary body properties. The body remains unchanged on disk.
	for _, key := range []string{"packet_id", "template_id", "report_id", "result_id", "issuance_event_id", "brief_path", "view_path", "answer_path", "review_result_reception"} {
		if value, ok := body[key]; ok {
			index[key] = value
		}
	}
	if value, ok := body["binding"]; ok {
		index["binding"] = currentBinding(body["template_id"], value)
	}
	// S9 result progress needs its frozen execution coordinates without
	// opening the professional body. Read checks its recorded coordinates
	// against the original execution when opening that body.
	if body["template_id"] == "s9-repair-result-v1" {
		inputs, _ := body["inputs"].(map[string]any)
		execution, _ := inputs["execution"].(map[string]any)
		index["execution_locator"] = map[string]any{"assignment_id": execution["assignment_id"], "execution_generation": execution["execution_generation"], "worktree_path": execution["worktree_path"]}
	}
	index["storage_format"] = StorageFormat
	// JSON-shaped values keep the original Runtime map contract.
	index["body_ref"] = map[string]any{"format": ref.Format, "path": ref.Path}
	return index
}

// currentBinding projects the already supplied responsibility through its
// current public type. Unknown fields remain in the original body only.
func currentBinding(template, value any) any {
	var target any
	switch template {
	case "s7-review-result-v1":
		target = &workpacket.ReviewResultBinding{}
	case "s9-plan-report-v1":
		target = &workpacket.RepairPlanReportBinding{}
	case "s9-repair-result-v1":
		target = &workpacket.RepairResultBinding{}
	default:
		// Unsupported subtypes are rejected by the owning Runtime selector.
		// Preserve only generic current identity for the standalone writer.
		binding, _ := value.(map[string]any)
		projected := map[string]any{}
		for _, key := range []string{"runtime_id", "baseline_generation", "owner", "owner_agent_id", "assignment_id"} {
			if item, ok := binding[key]; ok {
				projected[key] = item
			}
		}
		return projected
	}
	data, err := json.Marshal(value)
	if err != nil || json.Unmarshal(data, target) != nil {
		return nil
	}
	data, err = json.Marshal(target)
	if err != nil {
		return nil
	}
	var projected map[string]any
	if json.Unmarshal(data, &projected) != nil {
		return nil
	}
	return projected
}

// Publish preserves the complete original packet under its issued packet ID.
// No garbage collection removes orphan, pending, archive or checkpoint bodies.
func Publish(ctx context.Context, root string, body map[string]any) (map[string]any, error) {
	var reception any
	if body["template_id"] == "s7-review-result-v1" {
		reception = body["review_result_reception"]
	}
	body = Immutable(body)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > Limit {
		return nil, errors.New("original packet body exceeds storage limit")
	}
	packetID, _ := body["packet_id"].(string)
	if packetID == "" {
		return nil, errors.New("original packet has no packet_id")
	}
	ref, err := PublishOriginal(ctx, root, packetID, data)
	if err != nil {
		return nil, err
	}
	index := Index(body, ref)
	if reception != nil {
		index["review_result_reception"] = reception
	}
	return index, nil
}

// PublishOriginal allocates a new body location without claiming an occupied
// path. The caller records the returned reference in its existing transaction.
func PublishOriginal(ctx context.Context, root, packetID string, data []byte) (Reference, error) {
	if packetID == "" {
		return Reference{}, errors.New("original packet has no packet_id")
	}
	if int64(len(data)) > Limit {
		return Reference{}, errors.New("original packet body exceeds storage limit")
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return Reference{}, err
	}
	parent, _, err := workpacket.OpenParent(root, ".claude/packet-body-root-probe", true)
	if err != nil {
		return Reference{}, err
	}
	if err := parent.Close(); err != nil {
		return Reference{}, err
	}
	ref := Reference{Format: BodyFormat}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(packetID))
	// Split the reversible identity into bounded path components; packet IDs
	// do not have a schema length bound and must not become a filename gate.
	parts := make([]string, 0, 1+len(encoded)/120)
	for len(encoded) > 120 {
		parts = append(parts, encoded[:120])
		encoded = encoded[120:]
	}
	parts = append(parts, encoded+".json")
	// An unregistered path, including one with identical bytes, is not proof
	// that it belongs to this issuance. Only the Runtime Writer can publish
	// the selected ref as the original responsibility. Leave any occupied
	// path intact and allocate another one for this candidate.
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return Reference{}, err
		}
		storage := Directory
		if attempt > 0 {
			storage = fmt.Sprintf(".claude/packet-bodies-alt-%d/", attempt)
		}
		ref.Path = storage + "p/" + strings.Join(parts, "/")
		created, err := workpacket.PublishNewBounded(root, ref.Path, data, Limit)
		if err != nil {
			return Reference{}, fmt.Errorf("publish original packet body: %w", err)
		}
		if created {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return Reference{}, err
	}
	return ref, nil
}
