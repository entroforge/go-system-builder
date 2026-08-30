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
		return fmt.Errorf("validate data: %w", pruneValidationError(err))
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

// WarnMissingExtensionFields returns warnings for the extension fields an
// Agent Message omitted. The envelope schema layers its requirements: the 12
// base envelope fields stay hard-required (missing ones are rejected by
// ValidateBytes), while each message type's extension fields are recorded on
// the branch's "x-warn-required" annotation. Callers use this after a
// successful hard validation to surface the missing extensions as
// recoverable warnings instead of rejections.
func WarnMissingExtensionFields(data []byte) []string {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var message map[string]any
	if err := decoder.Decode(&message); err != nil {
		return nil
	}
	return warnMissingExtensions(message)
}

// warnMissingExtensions resolves the message-type branch in the embedded
// agent-message schema and diffs its x-warn-required list against the keys
// actually present in the instance. Unknown message types return nil.
func warnMissingExtensions(message map[string]any) []string {
	rawSchema, err := ReadAsset("agent-message.schema.json")
	if err != nil {
		return nil
	}
	var parsed struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(rawSchema, &parsed); err != nil {
		return nil
	}
	rawBase, ok := parsed.Defs["base"]
	if !ok {
		return nil
	}
	var base struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(rawBase, &base); err != nil {
		return nil
	}
	if len(base.Properties) == 0 {
		return nil
	}
	baseFields := make(map[string]struct{}, len(base.Properties))
	for field := range base.Properties {
		baseFields[field] = struct{}{}
	}
	branch := messageBranchFor(message)
	if branch == "" {
		return nil
	}
	rawWarn, ok := parsed.Defs[branch]
	if !ok {
		return nil
	}
	var branchDef struct {
		AllOf []json.RawMessage `json:"allOf"`
	}
	if err := json.Unmarshal(rawWarn, &branchDef); err != nil {
		return nil
	}
	for _, layer := range branchDef.AllOf {
		var tier struct {
			WarnRequired []string `json:"x-warn-required"`
		}
		if err := json.Unmarshal(layer, &tier); err != nil || len(tier.WarnRequired) == 0 {
			continue
		}
		var warnings []string
		for _, field := range tier.WarnRequired {
			if _, present := message[field]; present {
				continue
			}
			if _, isBase := baseFields[field]; isBase {
				// Base fields are hard-required elsewhere; they never belong
				// on the warn tier.
				continue
			}
			warnings = append(warnings, field)
		}
		return warnings
	}
	return nil
}

// messageBranchFor maps a message_type value to the $defs branch that carries
// its warn tier, following the discriminator pruning convention in errors.go
// (one lifecycle branch for all lifecycle event types).
func messageBranchFor(message map[string]any) string {
	messageType, _ := message["message_type"].(string)
	switch messageType {
	case "readback_request":
		return "readbackRequest"
	case "readback_response":
		return "readbackResponse"
	case "plan_report":
		return "planReport"
	case "activation":
		return "activation"
	case "completion_report":
		return "completionReport"
	case "work_start", "completion_ack", "blocker_report", "blocker_resolution", "shutdown_approval":
		return "lifecycleEvent"
	default:
		return ""
	}
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
