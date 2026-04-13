package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// SchemaCache caches compiled JSON schemas to avoid recompilation on each validation.
type SchemaCache struct {
	mu    sync.RWMutex
	cache map[string]*jsonschema.Schema
}

// NewSchemaCache creates an empty schema cache.
func NewSchemaCache() *SchemaCache {
	return &SchemaCache{cache: make(map[string]*jsonschema.Schema)}
}

// Compile returns a compiled schema for the given tool name and raw schema bytes.
// Results are cached so subsequent calls with the same name skip compilation.
func (c *SchemaCache) Compile(name string, schema []byte) (*jsonschema.Schema, error) {
	c.mu.RLock()
	if compiled, ok := c.cache[name]; ok {
		c.mu.RUnlock()
		return compiled, nil
	}
	c.mu.RUnlock()

	compiler := jsonschema.NewCompiler()
	resource := name + ".json"
	if err := compiler.AddResource(resource, bytes.NewReader(schema)); err != nil {
		return nil, fmt.Errorf("loading schema: %w", err)
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		return nil, fmt.Errorf("compiling schema: %w", err)
	}

	c.mu.Lock()
	c.cache[name] = compiled
	c.mu.Unlock()

	return compiled, nil
}

// ValidateInput validates tool input against the tool's JSON Schema.
// Returns nil if valid, or an error describing the validation failure.
func ValidateInput(schema []byte, input map[string]any) error {
	return ValidateInputCached(nil, "", schema, input)
}

// ValidateInputCached validates tool input using a cached compiled schema.
// If cache is nil, the schema is compiled fresh each time.
func ValidateInputCached(cache *SchemaCache, toolName string, schema []byte, input map[string]any) error {
	if len(schema) == 0 {
		return nil
	}

	var compiled *jsonschema.Schema
	var err error

	if cache != nil && toolName != "" {
		compiled, err = cache.Compile(toolName, schema)
	} else {
		compiler := jsonschema.NewCompiler()
		if addErr := compiler.AddResource("schema.json", bytes.NewReader(schema)); addErr != nil {
			return fmt.Errorf("loading schema: %w", addErr)
		}
		compiled, err = compiler.Compile("schema.json")
	}
	if err != nil {
		return fmt.Errorf("compiling schema: %w", err)
	}

	// Convert input to a JSON-round-tripped interface{} for the validator.
	data, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshaling input: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("unmarshaling input: %w", err)
	}

	if err := compiled.Validate(decoded); err != nil {
		return fmt.Errorf("input validation failed: %w", err)
	}
	return nil
}
