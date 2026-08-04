package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// Validator validates JSON instances against embedded schemas.
//
// Schemas (the *.schema.json files) are always loaded from the embedded assets
// compiled into the binary — they are never read from disk. Instance data (the
// files being validated) is read from disk by default, or from embedded assets
// when ValidateEmbedded is used.
type Validator struct {
	root     string
	compiler *jsonschema.Compiler
}

// NewValidator returns a Validator that resolves instance-data paths relative
// to root. Schemas are always embedded; root only affects disk-based instance
// reads.
func NewValidator(root string) *Validator {
	return &Validator{
		root:     root,
		compiler: newEmbeddedCompiler(),
	}
}

// NewEmbeddedValidator returns a Validator with no root (instance data must be
// supplied as bytes via ValidateBytes, not via ValidateFile).
func NewEmbeddedValidator() *Validator {
	return &Validator{compiler: newEmbeddedCompiler()}
}

// newEmbeddedCompiler builds a jsonschema.Compiler that resolves schema files
// from the embedded assets by basename. All $ref must be internal (#/$defs/),
// which is already the case for every schema in assets/.
func newEmbeddedCompiler() *jsonschema.Compiler {
	compiler := jsonschema.NewCompiler()
	// Register a loader that serves embedded schema bytes by basename.
	compiler.UseLoader(embeddedLoader{})
	return compiler
}

// embeddedLoader implements jsonschema.Loader, returning embedded schema bytes
// for any URL/basename that matches an embedded asset.
type embeddedLoader struct{}

func (embeddedLoader) Load(url string) (any, error) {
	name := stripDir(urlScheme(url))
	if AssetExists(name) {
		data, err := ReadAsset(name)
		if err != nil {
			return nil, err
		}
		var schema any
		if err := json.Unmarshal(data, &schema); err != nil {
			return nil, err
		}
		return schema, nil
	}
	return nil, nil // nil means "not handled, fall back to other loaders"
}

func urlScheme(s string) string {
	// santhosh-tekuri passes "file:///path" or bare paths; strip the scheme.
	if i := strings.Index(s, "://"); i >= 0 {
		return s[i+3:]
	}
	return s
}

// ValidateFile validates a disk-based instance file against an embedded schema.
// schemaName is a basename like "loop-state.schema.json".
func (v *Validator) ValidateFile(schemaName, dataPath string) error {
	data, err := os.ReadFile(v.resolve(dataPath))
	if err != nil {
		return fmt.Errorf("read data %s: %w", dataPath, err)
	}
	return v.ValidateBytes(schemaName, data)
}

// ValidateBytes validates in-memory instance bytes against an embedded schema.
// schemaName is a basename like "agent-message.schema.json". If schemaName
// matches an embedded schema, that schema is used; otherwise it is treated as
// a disk path for backward compatibility.
func (v *Validator) ValidateBytes(schemaName string, data []byte) error {
	compiled, err := v.compileSchema(schemaName)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	if err := compiled.Validate(value); err != nil {
		return fmt.Errorf("validate data: %w", err)
	}
	return nil
}

// ValidateEmbedded validates an embedded instance (by basename) against an
// embedded schema (by basename). Neither file needs to exist on disk.
func (v *Validator) ValidateEmbedded(schemaName, instanceName string) error {
	data, err := ReadAsset(instanceName)
	if err != nil {
		return err
	}
	return v.ValidateBytes(schemaName, data)
}

// compileSchema resolves a schema by basename from embed, falling back to disk
// for backward compatibility with any caller still passing a full path.
func (v *Validator) compileSchema(schemaName string) (*jsonschema.Schema, error) {
	name := stripDir(schemaName)
	if AssetExists(name) {
		return v.compiler.Compile(name)
	}
	// Backward-compat: treat as a disk path.
	return v.compiler.Compile(v.resolve(schemaName))
}

func (v *Validator) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(v.root, path)
}
