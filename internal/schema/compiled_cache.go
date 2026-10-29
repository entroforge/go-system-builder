package schema

import (
	"fmt"
	"sync"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// embeddedCompiledSchemaEntry coordinates concurrent cold compilation for a
// single immutable embedded asset. Completed entries live only in the
// compiled-schema cache; failed compiles are shared with current waiters but
// are not cached.
type embeddedCompiledSchemaEntry struct {
	ready  chan struct{}
	schema *jsonschema.Schema
	err    error
}

var (
	// Only immutable compiled Schema objects are retained; no instances or
	// validation results enter the process cache.
	embeddedCompiledSchemas sync.Map // basename -> *jsonschema.Schema
	embeddedSchemaFlights   sync.Map // basename -> *embeddedCompiledSchemaEntry
)

// compileEmbeddedSchema returns the immutable compiled form of one embedded
// schema. Each cache miss gets a private Compiler because Compiler is mutable
// and is not safe for concurrent Compile calls. The per-key flight prevents
// duplicate cold compilation without sharing that mutable Compiler.
func compileEmbeddedSchema(name string) (*jsonschema.Schema, error) {
	name = stripDir(name)
	if !AssetExists(name) {
		return nil, fmt.Errorf("embedded schema %q is unavailable", name)
	}
	if cached, ok := embeddedCompiledSchemas.Load(name); ok {
		return cached.(*jsonschema.Schema), nil
	}

	flight := &embeddedCompiledSchemaEntry{ready: make(chan struct{})}
	actual, loaded := embeddedSchemaFlights.LoadOrStore(name, flight)
	if loaded {
		completed := actual.(*embeddedCompiledSchemaEntry)
		<-completed.ready
		return completed.schema, completed.err
	}

	compiled, err := newEmbeddedCompiler().Compile(name)
	if err == nil {
		embeddedCompiledSchemas.Store(name, compiled)
	}
	flight.schema = compiled
	flight.err = err
	close(flight.ready)
	embeddedSchemaFlights.Delete(name)
	return compiled, err
}
