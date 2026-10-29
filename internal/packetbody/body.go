// Package packetbody stores immutable original packet JSON separately from
// Runtime's responsibility index. Bodies never prove issuance by themselves.
package packetbody

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const (
	StorageFormat       = "packet-body-ref-v1"
	BodyFormat          = "packet-json-v1"
	Directory           = ".claude/packet-bodies/"
	Limit         int64 = 20 << 20
)

type Reference struct {
	Format    string `json:"format"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
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
func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ref); err != nil {
		return ref, false, fmt.Errorf("invalid packet body reference: %w", err)
	}
	decodedSHA, err := hex.DecodeString(ref.SHA256)
	if err != nil || len(decodedSHA) != sha256.Size || ref.Path != Directory+ref.SHA256+".json" || ref.Format != BodyFormat || ref.SizeBytes < 1 || ref.SizeBytes > Limit {
		return ref, false, errors.New("invalid packet body reference identity/format/size")
	}
	return ref, true, nil
}

func Verify(ref Reference, data []byte) error {
	if int64(len(data)) != ref.SizeBytes || Digest(data) != ref.SHA256 {
		return errors.New("packet body bytes differ from original reference")
	}
	return nil
}

func Read(ctx context.Context, row map[string]any) (map[string]any, error) {
	ref, external, err := ReferenceFromIndex(row)
	if err != nil || !external {
		return row, err
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
	if err := Verify(ref, data); err != nil {
		return nil, fmt.Errorf("packet body %s: %w", ref.Path, err)
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	index := Index(body, ref)
	expected, err := json.Marshal(index)
	if err != nil {
		return nil, err
	}
	actual, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(expected, actual) {
		return nil, errors.New("packet body identity differs from Runtime index")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}

func Index(body map[string]any, ref Reference) map[string]any {
	index := make(map[string]any, len(body))
	for key, value := range body {
		if key != "draft" && key != "inputs" {
			index[key] = value
		}
	}
	// S9 result progress needs its frozen execution coordinates without
	// opening the professional body. Read binds this derived locator by comparing
	// the complete projected index to the same original body.
	if body["template_id"] == "s9-repair-result-v1" {
		inputs, _ := body["inputs"].(map[string]any)
		execution, _ := inputs["execution"].(map[string]any)
		index["execution_locator"] = map[string]any{"assignment_id": execution["assignment_id"], "execution_generation": execution["execution_generation"], "worktree_path": execution["worktree_path"]}
	}
	index["storage_format"] = StorageFormat
	// JSON-shaped values keep the original Runtime map contract.
	index["body_ref"] = map[string]any{"format": ref.Format, "path": ref.Path, "sha256": ref.SHA256, "size_bytes": ref.SizeBytes}
	return index
}

// Publish preserves the complete original packet. Content addressing and the
// original binding/issuance ID inside the bytes identify orphan publication;
// no garbage collection removes orphan, pending, archive or checkpoint bodies.
func Publish(ctx context.Context, root string, body map[string]any) (map[string]any, error) {
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
	ref := Reference{Format: BodyFormat, SHA256: Digest(data), SizeBytes: int64(len(data))}
	ref.Path = Directory + ref.SHA256 + ".json"
	existing, err := workpacket.PublishBounded(root, ref.Path, data, Limit)
	if err != nil {
		return nil, fmt.Errorf("publish original packet body: %w", err)
	}
	if !bytes.Equal(existing, data) {
		return nil, errors.New("immutable original packet body conflict")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Index(body, ref), nil
}
