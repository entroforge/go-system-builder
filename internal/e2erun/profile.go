package e2erun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// DecodeProfile validates configuration shape, not its approval. The Runtime
// producer must read this document from the formal source view and verify its
// protected authority binding before invoking Run.
func DecodeProfile(data []byte) (Profile, error) {
	var profile Profile
	if err := schema.NewEmbeddedValidator().ValidateBytes("e2e-runner-profile.schema.json", data); err != nil {
		return profile, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return profile, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return profile, fmt.Errorf("E2E profile must contain one JSON object")
	}
	return profile, nil
}

// InspectTool fingerprints a supplied, already-provisioned local tool by
// copying it under the same normalization used during execution. It executes
// nothing, installs nothing, and grants no authority to the resulting digest.
func InspectTool(ctx context.Context, path string) (Tool, error) {
	if !filepath.IsAbs(path) {
		return Tool{}, fmt.Errorf("provisioned tool requires an absolute path")
	}
	temporary, err := os.MkdirTemp("", "loop-e2e-inspect-")
	if err != nil {
		return Tool{}, err
	}
	defer os.RemoveAll(temporary)
	hash, err := copyTool(ctx, Tool{Path: path}, filepath.Join(temporary, "tool"))
	if err != nil {
		return Tool{}, err
	}
	return Tool{Path: path, SHA256: hash}, nil
}

// InspectPlatform records the actual supported host boundary. It is a fact
// for a draft profile, not approval to execute project code on that host.
func InspectPlatform() (Platform, error) { return currentPlatform() }
