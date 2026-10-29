package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func TestEmbeddedCompiledSchemaCacheSharesImmutableObject(t *testing.T) {
	first, err := compileEmbeddedSchema("loop-event.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	second, err := compileEmbeddedSchema("assets/loop-event.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("equivalent embedded basenames did not reuse the compiled schema")
	}
}

func TestEmbeddedCompiledSchemaCacheConcurrentColdCompileAndValidate(t *testing.T) {
	const eventSchema = "loop-event.schema.json"
	const stateSchema = "loop-state.schema.json"
	embeddedCompiledSchemas.Delete(eventSchema)
	embeddedCompiledSchemas.Delete("loop-state-v1.1.schema.json")
	embeddedCompiledSchemas.Delete("loop-state-v1.2.schema.json")

	eventData, err := ReadAsset("loop-event.example.json")
	if err != nil {
		t.Fatal(err)
	}
	stateData, err := ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	stateV12, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 24
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			name, valid := eventSchema, eventData
			switch i % 3 {
			case 1:
				name, valid = stateSchema, stateData
			case 2:
				name, valid = stateSchema, stateV12
			}
			validator := NewEmbeddedValidator()
			for attempt := 0; attempt < 3; attempt++ {
				if err := validator.ValidateBytes(name, valid); err != nil {
					errs <- fmt.Errorf("worker %d valid attempt %d: %w", i, attempt, err)
					return
				}
				if err := validator.ValidateBytes(name, []byte(`{}`)); err == nil {
					errs <- fmt.Errorf("worker %d invalid attempt %d unexpectedly validated", i, attempt)
					return
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	for _, name := range []string{eventSchema, "loop-state-v1.1.schema.json", "loop-state-v1.2.schema.json"} {
		if _, ok := embeddedCompiledSchemas.Load(name); !ok {
			t.Errorf("successful compile of %s was not retained", name)
		}
	}
}

func TestEmbeddedCompiledSchemaCacheRejectsNonEmbeddedKeys(t *testing.T) {
	const schemaName = "temporary-cache-probe.schema.json"
	if AssetExists(schemaName) {
		t.Fatalf("test schema name unexpectedly collides with embedded asset %q", schemaName)
	}
	if _, err := compileEmbeddedSchema(schemaName); err == nil {
		t.Fatal("non-embedded schema was compiled through the process cache")
	}
	if _, ok := embeddedCompiledSchemas.Load(schemaName); ok {
		t.Fatal("non-embedded schema entered the process cache")
	}
}

func BenchmarkEmbeddedSchemaValidationCompileCache(b *testing.B) {
	const schemaName = "loop-event.schema.json"
	data, err := ReadAsset("loop-event.example.json")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := compileEmbeddedSchema(schemaName); err != nil {
		b.Fatal(err)
	}

	b.Run("process_cache_new_validator_each_call", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("fresh_compiler_each_call", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := validateBytesWithFreshEmbeddedCompiler(schemaName, data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func validateBytesWithFreshEmbeddedCompiler(name string, data []byte) error {
	compiled, err := newEmbeddedCompiler().Compile(name)
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
