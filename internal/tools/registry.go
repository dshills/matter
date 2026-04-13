// Package tools provides tool registration, validation, and execution.
package tools

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/dshills/matter/pkg/matter"
)

// Registry manages tool registration and lookup.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]matter.Tool
	order  []matter.Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]matter.Tool),
	}
}

// Register adds a tool to the registry. Returns an error if a tool with the
// same name is already registered.
func (r *Registry) Register(tool matter.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byName[tool.Name]; exists {
		return fmt.Errorf("tool %q is already registered", tool.Name)
	}
	r.byName[tool.Name] = tool
	r.order = append(r.order, tool)
	return nil
}

// Get returns a tool by name. Returns false if not found.
func (r *Registry) Get(name string) (matter.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byName[name]
	return t, ok
}

// List returns all tools in registration order.
func (r *Registry) List() []matter.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]matter.Tool, len(r.order))
	copy(out, r.order)
	return out
}

// toolSchema is the JSON structure exported for planner prompts.
type toolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Schemas returns a JSON-encoded array of tool schemas suitable for planner prompts.
func (r *Registry) Schemas() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemas := make([]toolSchema, len(r.order))
	for i, t := range r.order {
		schemas[i] = toolSchema{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return json.Marshal(schemas)
}
